package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

type credentialInventoryResponse struct {
	Credentials []struct {
		ID          string   `json:"id"`
		UserID      string   `json:"user_id"`
		Name        string   `json:"name"`
		Purpose     string   `json:"purpose"`
		Key         string   `json:"key"`
		KeyMasked   string   `json:"key_masked"`
		TokenPrefix string   `json:"token_prefix"`
		Scopes      []string `json:"scopes"`
		CreatedAt   string   `json:"created_at"`
		LastUsedAt  *string  `json:"last_used_at"`
		RevokedAt   *string  `json:"revoked_at"`
	} `json:"credentials"`
	Page types.PageInfo `json:"page"`
}

func TestHTTPCredentialInventoryAndIndependentRaycastInstallations(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	userA := types.User{ID: "usr_http_credentials_a", Email: "a@example.invalid", DisplayName: "A"}
	userB := types.User{ID: "usr_http_credentials_b", Email: "b@example.invalid", DisplayName: "B"}
	repo.Users = append(repo.Users, userA, userB)
	authA := types.AuthContext{UserID: userA.ID, UserDisplayName: userA.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard", SessionID: "sess_a"}
	authB := types.AuthContext{UserID: userB.ID, UserDisplayName: userB.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard", SessionID: "sess_b"}
	managerA, err := svc.CreateAPIKeyWithPurposeAndScopes(t.Context(), authA, "credential manager", "cli", []string{"keys:read", "keys:write"})
	if err != nil {
		t.Fatal(err)
	}
	managerB, err := svc.CreateAPIKeyWithPurposeAndScopes(t.Context(), authB, "credential manager", "cli", []string{"keys:read", "keys:write"})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{AppPublicURL: "https://dashboard.example"}, svc)

	do := func(method string, path string, secret string, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			request.Header.Set("content-type", "application/json")
		}
		request.Header.Set("authorization", "Bearer "+secret)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	create := func(label string) struct {
		Credential struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Purpose string   `json:"purpose"`
			Key     string   `json:"key"`
			Scopes  []string `json:"scopes"`
		} `json:"credential"`
		Setup types.RaycastSetupMaterial `json:"raycast_setup"`
	} {
		t.Helper()
		response := do(http.MethodPost, "/api/raycast-installations", managerA.Key, `{"label":`+quotedJSON(label)+`}`)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %q status=%d body=%s", label, response.Code, response.Body.String())
		}
		var payload struct {
			Credential struct {
				ID      string   `json:"id"`
				Name    string   `json:"name"`
				Purpose string   `json:"purpose"`
				Key     string   `json:"key"`
				Scopes  []string `json:"scopes"`
			} `json:"credential"`
			Setup types.RaycastSetupMaterial `json:"raycast_setup"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	macbook := create("MacBook Air")
	studio := create("Studio Mac")
	if macbook.Credential.ID == studio.Credential.ID || macbook.Credential.Key == studio.Credential.Key {
		t.Fatalf("installations collided: macbook=%#v studio=%#v", macbook, studio)
	}
	for _, payload := range []struct {
		Credential struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Purpose string   `json:"purpose"`
			Key     string   `json:"key"`
			Scopes  []string `json:"scopes"`
		} `json:"credential"`
		Setup types.RaycastSetupMaterial `json:"raycast_setup"`
	}{macbook, studio} {
		if payload.Credential.Purpose != "raycast" || strings.Join(payload.Credential.Scopes, ",") != "threads:read,threads:write,assets:read,assets:write" || payload.Setup.APIKey != payload.Credential.Key || payload.Setup.CredentialID != payload.Credential.ID {
			t.Fatalf("unsafe or incomplete Raycast contract: %#v", payload)
		}
	}

	duplicate := do(http.MethodPost, "/api/raycast-installations", managerA.Key, `{"label":"MacBook Air"}`)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), `"code":"CREDENTIAL_LABEL_CONFLICT"`) {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	list := do(http.MethodGet, "/api/keys?limit=2", managerA.Key, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), macbook.Credential.Key) || strings.Contains(list.Body.String(), studio.Credential.Key) || strings.Contains(list.Body.String(), "token_hash") {
		t.Fatalf("credential inventory leaked secret/hash: %s", list.Body.String())
	}
	var firstPage credentialInventoryResponse
	if err := json.Unmarshal(list.Body.Bytes(), &firstPage); err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Credentials) != 2 || !firstPage.Page.HasMore || firstPage.Page.NextCursor == nil {
		t.Fatalf("first page=%#v", firstPage)
	}
	continuation := do(http.MethodGet, "/api/keys?limit=2&cursor="+*firstPage.Page.NextCursor, managerA.Key, "")
	if continuation.Code != http.StatusOK {
		t.Fatalf("continuation status=%d body=%s", continuation.Code, continuation.Body.String())
	}

	oldMacbookSecret := macbook.Credential.Key
	rotate := do(http.MethodPatch, "/api/keys/"+macbook.Credential.ID, managerA.Key, "")
	if rotate.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rotate.Code, rotate.Body.String())
	}
	var rotated struct {
		Credential struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"credential"`
		Setup types.RaycastSetupMaterial `json:"raycast_setup"`
	}
	if err := json.Unmarshal(rotate.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.Credential.ID != macbook.Credential.ID || rotated.Credential.Key == "" || rotated.Credential.Key == oldMacbookSecret || rotated.Setup.APIKey != rotated.Credential.Key {
		t.Fatalf("rotate payload=%#v", rotated)
	}
	if response := do(http.MethodGet, "/api/auth/me", oldMacbookSecret, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("old secret status=%d body=%s", response.Code, response.Body.String())
	}
	if response := do(http.MethodGet, "/api/auth/me", studio.Credential.Key, ""); response.Code != http.StatusOK {
		t.Fatalf("other installation was rotated: status=%d body=%s", response.Code, response.Body.String())
	}

	setup := do(http.MethodGet, "/api/keys/"+macbook.Credential.ID+"/setup", managerA.Key, "")
	if setup.Code != http.StatusOK || strings.Contains(setup.Body.String(), rotated.Credential.Key) || strings.Contains(setup.Body.String(), `"api_key":`) || !strings.Contains(setup.Body.String(), `"credential_id":"`+macbook.Credential.ID+`"`) {
		t.Fatalf("reopened setup status=%d body=%s", setup.Code, setup.Body.String())
	}

	revoke := do(http.MethodDelete, "/api/keys/"+studio.Credential.ID, managerA.Key, "")
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revoke.Code, revoke.Body.String())
	}
	inventory := do(http.MethodGet, "/api/keys?limit=10", managerA.Key, "")
	var all credentialInventoryResponse
	if err := json.Unmarshal(inventory.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	var revokedSeen bool
	for _, credential := range all.Credentials {
		if credential.ID == studio.Credential.ID {
			revokedSeen = credential.RevokedAt != nil
		}
	}
	if !revokedSeen {
		t.Fatalf("revoked history missing: %s", inventory.Body.String())
	}

	for _, crossUser := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/keys/" + macbook.Credential.ID + "/setup"},
		{http.MethodPatch, "/api/keys/" + macbook.Credential.ID},
		{http.MethodDelete, "/api/keys/" + macbook.Credential.ID},
	} {
		response := do(crossUser.method, crossUser.path, managerB.Key, "")
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"CREDENTIAL_NOT_FOUND"`) {
			t.Fatalf("cross-user %s %s status=%d body=%s", crossUser.method, crossUser.path, response.Code, response.Body.String())
		}
	}
}

func quotedJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
