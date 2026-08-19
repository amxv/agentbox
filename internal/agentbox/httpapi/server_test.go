package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

func setThreadVisibilityForTest(ctx context.Context, repository interface {
	ManageThreadVisibility(context.Context, string, string, types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error)
}, userID string, threadID string, desiredTeamIDs []string) (types.ThreadVisibility, error) {
	current, err := repository.ManageThreadVisibility(ctx, userID, threadID, types.ManageThreadVisibilityInput{})
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	desired := map[string]bool{}
	for _, teamID := range desiredTeamIDs {
		desired[teamID] = true
	}
	currentIDs := map[string]bool{}
	input := types.ManageThreadVisibilityInput{}
	for _, team := range current.SharedTeams {
		currentIDs[team.ID] = true
		if !desired[team.ID] {
			input.RemoveTeams = append(input.RemoveTeams, team.ID)
		}
	}
	for _, teamID := range desiredTeamIDs {
		if !currentIDs[teamID] {
			input.AddTeams = append(input.AddTeams, teamID)
		}
	}
	state, err := repository.ManageThreadVisibility(ctx, userID, threadID, input)
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	return types.ThreadVisibility{ThreadID: state.ThreadID, OwnerUserID: state.OwnerUserID, SharedTeams: state.SharedTeams}, nil
}

func TestHealth(t *testing.T) {
	svc := service.New(&db.MemoryRepository{}, &assets.FakeStore{})
	server := NewServer(config.Config{}, svc)

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true || payload["service"] != "agentbox" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestMaintenanceModeBlocksProductRoutesButAllowsOperationalPathsAndExplicitBypass(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	server := NewServer(config.Config{
		AdminKey:             "admin-secret",
		MaintenanceMode:      true,
		MaintenanceBypassKey: "bypass-secret",
		SessionCookieName:    config.DefaultSessionCookieName,
	}, svc)

	blocked := httptest.NewRecorder()
	server.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/api/threads", nil))
	if blocked.Code != http.StatusServiceUnavailable || !strings.Contains(blocked.Body.String(), `"code":"MAINTENANCE_MODE"`) {
		t.Fatalf("blocked status=%d body=%s", blocked.Code, blocked.Body.String())
	}

	health := httptest.NewRecorder()
	server.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", health.Code, health.Body.String())
	}

	setupToken := httptest.NewRecorder()
	setupRequest := httptest.NewRequest(http.MethodPost, "/api/admin/owner/setup-token", strings.NewReader(`{}`))
	setupRequest.Header.Set("x-agentbox-admin-key", "admin-secret")
	server.ServeHTTP(setupToken, setupRequest)
	if setupToken.Code != http.StatusCreated {
		t.Fatalf("setup-token status=%d body=%s", setupToken.Code, setupToken.Body.String())
	}

	bypassed := httptest.NewRecorder()
	bypassRequest := httptest.NewRequest(http.MethodGet, "/api/threads", nil)
	bypassRequest.Header.Set("x-agentbox-maintenance-key", "bypass-secret")
	server.ServeHTTP(bypassed, bypassRequest)
	if bypassed.Code != http.StatusUnauthorized {
		t.Fatalf("bypass should reach normal auth boundary: status=%d body=%s", bypassed.Code, bypassed.Body.String())
	}

	owner := types.User{ID: "usr_maintenance_owner", Email: "owner@example.com", DisplayName: "Owner", IsOwner: true}
	repo.Users = append(repo.Users, owner)
	ownerSecret := "maintenance-owner-session"
	if _, err := repo.CreateUserSession(t.Context(), types.UserSession{
		ID:         "sess_maintenance_owner",
		UserID:     owner.ID,
		SecretHash: dbHashForTest(ownerSecret),
	}); err != nil {
		t.Fatal(err)
	}
	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/owner/users", nil)
	ownerRequest.AddCookie(&http.Cookie{Name: config.DefaultSessionCookieName, Value: ownerSecret})
	ownerResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerResponse, ownerRequest)
	if ownerResponse.Code != http.StatusOK {
		t.Fatalf("owner browser should pass maintenance gate: status=%d body=%s", ownerResponse.Code, ownerResponse.Body.String())
	}

	ownerKey, err := svc.CreateAPIKey(t.Context(), types.AuthContext{
		UserID:          owner.ID,
		UserDisplayName: owner.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		SessionID:       "sess_maintenance_owner",
		ActorName:       "Web dashboard",
		IsOwner:         true,
	}, "owner-api")
	if err != nil {
		t.Fatal(err)
	}
	ownerKeyRequest := httptest.NewRequest(http.MethodGet, "/api/owner/users?key="+url.QueryEscape(ownerKey.Key), nil)
	ownerKeyResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerKeyResponse, ownerKeyRequest)
	if ownerKeyResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("owner API key bypassed maintenance: status=%d body=%s", ownerKeyResponse.Code, ownerKeyResponse.Body.String())
	}
}

func TestMCPOriginValidation(t *testing.T) {
	svc := service.New(&db.MemoryRepository{}, &assets.FakeStore{})
	server := NewServer(config.Config{AllowedOrigins: []string{"https://allowed.test"}}, svc)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(`{}`))
	req.Header.Set("origin", "https://blocked.test")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRequestBaseURLUsesConfiguredOriginAndIgnoresForwardedSpoofing(t *testing.T) {
	configured := NewServer(config.Config{Environment: "production", AppPublicURL: "https://dashboard.example/"}, nil)
	request := httptest.NewRequest(http.MethodGet, "http://backend.internal/api/health", nil)
	request.Host = "backend.internal"
	request.Header.Set("Forwarded", `host=evil.example;proto=https`)
	request.Header.Set("X-Forwarded-Host", "evil.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	if got := configured.requestBaseURL(request); got != "https://dashboard.example" {
		t.Fatalf("configured request base URL=%q", got)
	}

	development := NewServer(config.Config{}, nil)
	if got := development.requestBaseURL(request); got != "http://backend.internal" {
		t.Fatalf("development request base URL trusted forwarded spoofing: %q", got)
	}
	request.Host = "evil.example/path"
	if got := development.requestBaseURL(request); got != "" {
		t.Fatalf("invalid development host produced origin %q", got)
	}

	invalidProduction := NewServer(config.Config{Environment: "production", AppPublicURL: "http://dashboard.example"}, nil)
	if got := invalidProduction.requestBaseURL(request); got != "" {
		t.Fatalf("invalid production origin produced %q", got)
	}
}

func authContext(_ string, actorName string) types.AuthContext {
	return types.AuthContext{
		UserID:      "usr_" + actorName,
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   actorName,
	}
}

func testUser(_ string, userID string, email string, displayName string, _ string, passwordHash string) types.User {
	now := "2026-07-07T00:00:00.000Z"
	return types.User{
		ID:           userID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func dbHashForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
