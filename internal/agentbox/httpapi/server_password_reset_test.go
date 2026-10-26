package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

func TestOwnerIssuedPasswordResetPreservesAgentCredentialsAndInvalidatesBrowserSessions(t *testing.T) {
	oldHash, err := auth.HashPassword("current-password")
	if err != nil {
		t.Fatal(err)
	}
	owner := types.User{ID: "usr_owner", Email: "owner@example.com", DisplayName: "Owner", PasswordHash: &oldHash, IsOwner: true}
	member := types.User{ID: "usr_member", Email: "member@example.com", DisplayName: "Member", PasswordHash: &oldHash}
	repo := &db.MemoryRepository{Users: []types.User{owner, member}}
	svc := service.New(repo, &assets.FakeStore{})
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, svc)

	call := func(method string, path string, body string, cookie *http.Cookie, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	ownerLogin := call(http.MethodPost, "/api/auth/login", `{"email":"owner@example.com","password":"current-password"}`, nil, "")
	if ownerLogin.Code != http.StatusOK {
		t.Fatal(ownerLogin.Body.String())
	}
	ownerCookie := ownerLogin.Result().Cookies()[0]
	memberLogin := call(http.MethodPost, "/api/auth/login", `{"email":"member@example.com","password":"current-password"}`, nil, "")
	if memberLogin.Code != http.StatusOK {
		t.Fatal(memberLogin.Body.String())
	}
	memberCookie := memberLogin.Result().Cookies()[0]

	agent, err := svc.CreateAPIKeyWithPurposeAndScopes(t.Context(), types.AuthContext{UserID: member.ID, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_fake_member", ActorName: "Member"}, "Connected agent", "chatgpt", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := svc.CreateAPIKeyWithPurposeAndScopes(t.Context(), types.AuthContext{UserID: owner.ID, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_fake_owner", IsOwner: true, ActorName: "Owner"}, "Owner agent", "chatgpt", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	issuePath := "/api/owner/users/usr_member/password-reset"
	for name, request := range map[string]*httptest.ResponseRecorder{
		"no auth":        call(http.MethodPost, issuePath, "", nil, ""),
		"member browser": call(http.MethodPost, issuePath, "", memberCookie, ""),
		"owner API key":  call(http.MethodPost, issuePath, "", nil, ownerKey.Key),
	} {
		want := http.StatusForbidden
		if name == "no auth" {
			want = http.StatusUnauthorized
		}
		if request.Code != want {
			t.Fatalf("%s code=%d body=%s", name, request.Code, request.Body.String())
		}
	}
	if result := call(http.MethodPost, "/api/owner/users/usr_owner/password-reset", "", ownerCookie, ""); result.Code != http.StatusBadRequest {
		t.Fatalf("owner recovery allowed: %d %s", result.Code, result.Body.String())
	}
	issue := func() string {
		t.Helper()
		response := call(http.MethodPost, issuePath, "", ownerCookie, "")
		if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("issue code=%d body=%s", response.Code, response.Body.String())
		}
		var result struct {
			ResetPath string `json:"reset_path"`
			ExpiresAt string `json:"expires_at"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		resetURL, err := url.Parse(result.ResetPath)
		if err != nil || resetURL.Path != "/reset-password" || result.ExpiresAt == "" {
			t.Fatalf("bad reset path: %#v error=%v", result, err)
		}
		token := strings.TrimPrefix(resetURL.Fragment, "token=")
		if !strings.HasPrefix(token, "agpr_") || len(token) != len("agpr_")+43 {
			t.Fatalf("unexpected token %q", token)
		}
		return token
	}
	oldToken := issue()
	token := issue()
	if token == oldToken {
		t.Fatal("reissued same token")
	}
	requestJSON := func(path string, token string, password string) *httptest.ResponseRecorder {
		t.Helper()
		value, err := json.Marshal(map[string]string{"token": token, "password": password})
		if err != nil {
			t.Fatal(err)
		}
		return call(http.MethodPost, path, string(value), nil, "")
	}
	inspectPath := "/api/auth/password-reset/inspect"
	completePath := "/api/auth/password-reset/complete"
	if response := requestJSON(inspectPath, oldToken, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("old token still valid: %s", response.Body.String())
	}
	if response := requestJSON(completePath, oldToken, "a-brand-new-long-password"); response.Code != http.StatusBadRequest {
		t.Fatalf("old token completed: %s", response.Body.String())
	}
	if response := requestJSON(inspectPath, token, ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"valid":true`) {
		t.Fatalf("valid link inspection: %d %s", response.Code, response.Body.String())
	}
	if response := requestJSON(completePath, token, "short"); response.Code != http.StatusBadRequest {
		t.Fatalf("weak password accepted: %s", response.Body.String())
	}
	if response := requestJSON(inspectPath, token, ""); response.Code != http.StatusOK {
		t.Fatalf("failed password submission consumed token: %s", response.Body.String())
	}
	if response := requestJSON(completePath, token, "a-brand-new-long-password"); response.Code != http.StatusOK {
		t.Fatalf("reset failed: %d %s", response.Code, response.Body.String())
	}
	if response := requestJSON(completePath, token, "a-different-long-password"); response.Code != http.StatusBadRequest {
		t.Fatalf("replay succeeded: %s", response.Body.String())
	}
	if response := requestJSON(inspectPath, token, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("used link still valid: %s", response.Body.String())
	}
	if response := call(http.MethodPost, "/api/auth/login", `{"email":"member@example.com","password":"current-password"}`, nil, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("old password worked: %s", response.Body.String())
	}
	if response := call(http.MethodGet, "/api/auth/me", "", memberCookie, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("old browser session worked: %s", response.Body.String())
	}
	if response := call(http.MethodPost, "/api/auth/login", `{"email":"member@example.com","password":"a-brand-new-long-password"}`, nil, ""); response.Code != http.StatusOK {
		t.Fatalf("new password rejected: %s", response.Body.String())
	}
	if response := call(http.MethodGet, "/api/auth/me", "", ownerCookie, ""); response.Code != http.StatusOK {
		t.Fatalf("owner session affected: %s", response.Body.String())
	}
	if response := call(http.MethodGet, "/api/auth/me", "", nil, agent.Key); response.Code != http.StatusOK {
		t.Fatalf("member agent key revoked: %s", response.Body.String())
	}
	if repo.Users[1].ID != member.ID || repo.Users[1].Email != member.Email {
		t.Fatalf("member identity mutated: %#v", repo.Users[1])
	}
}
