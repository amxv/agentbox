package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	authpkg "agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

func TestThreadDisplayPreferencesSyncBetweenSessionsButRemainUserScoped(t *testing.T) {
	repo := &db.MemoryRepository{}
	passwordHash, err := authpkg.HashPassword("browser-password")
	if err != nil {
		t.Fatal(err)
	}
	repo.Users = append(repo.Users,
		testUser("", "reader_a", "reader-a@example.com", "Reader A", "", passwordHash),
		testUser("", "reader_b", "reader-b@example.com", "Reader B", "", passwordHash),
	)
	svc := service.New(repo, &assets.FakeStore{})
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, svc)
	login := func(email string) *http.Cookie {
		t.Helper()
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"`+email+`","password":"browser-password"}`)))
		if response.Code != http.StatusOK {
			t.Fatalf("login %s: %d %s", email, response.Code, response.Body.String())
		}
		return response.Result().Cookies()[0]
	}
	aSession1 := login("reader-a@example.com")
	aSession2 := login("reader-a@example.com")
	bSession := login("reader-b@example.com")

	aAuth := types.AuthContext{UserID: "reader_a", SubjectType: types.AuthSubjectUserSession, ActorName: "Reader A"}
	thread, err := svc.CreateThread(t.Context(), aAuth, "Shared display preference test")
	if err != nil {
		t.Fatal(err)
	}
	repo.Teams = append(repo.Teams, types.Team{ID: "team_readers", Slug: "readers", Name: "Readers"})
	repo.TeamMemberships = append(repo.TeamMemberships, types.TeamMembership{TeamID: "team_readers", UserID: "reader_b"})
	repo.ThreadTeamShares = append(repo.ThreadTeamShares, types.ThreadTeamShare{TeamID: "team_readers", ThreadID: thread.ID})

	path := "/api/threads/" + thread.ID + "/display-preference"
	request := func(method string, url string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}
	decode := func(response *httptest.ResponseRecorder) types.ThreadDisplayPreference {
		t.Helper()
		if response.Code != http.StatusOK {
			t.Fatalf("status %d: %s", response.Code, response.Body.String())
		}
		var result struct {
			Preference types.ThreadDisplayPreference `json:"preference"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Preference
	}

	if got := request(http.MethodGet, path, nil, ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous preference read status=%d", got.Code)
	}
	if initial := decode(request(http.MethodGet, path, aSession1, "")); initial.AllPlainAsMarkdown || len(initial.MessagePreferences) != 0 {
		t.Fatalf("initial preference = %#v", initial)
	}
	saved := decode(request(http.MethodPatch, path, aSession1, `{"all_plain_as_markdown":true,"message_preferences":{"msg_one":false,"msg_two":true}}`))
	if !saved.AllPlainAsMarkdown || saved.MessagePreferences["msg_one"] || !saved.MessagePreferences["msg_two"] {
		t.Fatalf("saved preference = %#v", saved)
	}
	onOtherDevice := decode(request(http.MethodGet, path, aSession2, ""))
	if !onOtherDevice.AllPlainAsMarkdown || onOtherDevice.MessagePreferences["msg_one"] || !onOtherDevice.MessagePreferences["msg_two"] {
		t.Fatalf("second session lost settings: %#v", onOtherDevice)
	}
	if otherReader := decode(request(http.MethodGet, path, bSession, "")); otherReader.AllPlainAsMarkdown || len(otherReader.MessagePreferences) != 0 {
		t.Fatalf("another user inherited first user's preferences: %#v", otherReader)
	}
	decode(request(http.MethodPatch, path, bSession, `{"all_plain_as_markdown":false,"message_preferences":{"msg_two":false}}`))
	if aSetting := decode(request(http.MethodGet, path, aSession1, "")); !aSetting.AllPlainAsMarkdown || !aSetting.MessagePreferences["msg_two"] {
		t.Fatalf("other user overwrote account preference: %#v", aSetting)
	}

	for _, payload := range []string{
		`{"all_plain_as_markdown":true,"surprise":1}`,
		`{"message_preferences":{"msg_one":"yes"}}`,
		`{"message_preferences":{" ":true}}`,
	} {
		response := request(http.MethodPatch, path, aSession1, payload)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid payload accepted: %d, %s", response.Code, response.Body.String())
		}
	}
	if got := request(http.MethodPost, path, aSession1, ""); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("preference method status=%d", got.Code)
	}

	activityPath := "/api/threads/" + thread.ID + "/activity"
	activity := request(http.MethodGet, activityPath, aSession2, "")
	if activity.Code != http.StatusOK || !strings.Contains(activity.Body.String(), thread.UpdatedAt) || strings.Contains(activity.Body.String(), "message_preferences") {
		t.Fatalf("activity must be a lightweight thread timestamp: %d, %s", activity.Code, activity.Body.String())
	}
	if got := request(http.MethodGet, activityPath, nil, ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous activity status=%d", got.Code)
	}

	// Removing a teammate revokes preference and activity access, even when a
	// saved row remains. An owner session cannot bypass normal thread access.
	repo.TeamMemberships = nil
	for _, url := range []string{path, activityPath} {
		if got := request(http.MethodGet, url, bSession, ""); got.Code != http.StatusNotFound {
			t.Fatalf("revoked reader GET %s status=%d body=%s", url, got.Code, got.Body.String())
		}
	}
	if got := request(http.MethodPatch, path, bSession, `{"all_plain_as_markdown":true}`); got.Code != http.StatusNotFound {
		t.Fatalf("revoked reader PATCH status=%d body=%s", got.Code, got.Body.String())
	}
	if got := decode(request(http.MethodGet, path, aSession2, "")); !got.AllPlainAsMarkdown {
		t.Fatalf("owner lost preference after teammate removal: %#v", got)
	}
}
