package auth

import (
	"net/http/httptest"
	"testing"

	"agentbox/internal/agentbox/config"
)

func TestParseAPIKeysCommaAndJSON(t *testing.T) {
	keys, err := ParseAPIKeys("primary:secret:Agent,secondary:other")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0].Author != "Agent" || keys[1].Author != "secondary" {
		t.Fatalf("unexpected keys: %#v", keys)
	}

	keys, err = ParseAPIKeys(`[{"name":"json","key":"secret","author":"Author"},{"name":"bad"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Name != "json" {
		t.Fatalf("unexpected JSON keys: %#v", keys)
	}
}

func TestAuthenticateRequestAndLocalFallback(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/threads?key=secret", nil)
	actor, err := AuthenticateRequest(req, config.Config{APIKeys: "primary:secret:Agent", Environment: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if actor == nil || actor.Name != "Agent" || actor.KeyName != "primary" {
		t.Fatalf("unexpected actor: %#v", actor)
	}

	missing := httptest.NewRequest("GET", "/api/threads", nil)
	actor, err = AuthenticateRequest(missing, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if actor == nil || actor.Name != "local-dev" {
		t.Fatalf("expected local dev actor, got %#v", actor)
	}

	actor, err = AuthenticateRequest(missing, config.Config{Environment: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if actor != nil {
		t.Fatalf("expected no production actor, got %#v", actor)
	}
}

func TestAdminKeysAndOrigin(t *testing.T) {
	if err := RequireAdminKey("admin-secret", config.Config{AdminKeys: "admin:admin-secret", Environment: "production"}); err != nil {
		t.Fatal(err)
	}
	if err := RequireAdminKey("wrong", config.Config{AdminKeys: "admin:admin-secret", Environment: "production"}); err == nil || err.Error() != "Unauthorized" {
		t.Fatalf("expected unauthorized, got %v", err)
	}

	req := httptest.NewRequest("GET", "/api/mcp", nil)
	req.Header.Set("origin", "https://bad.example")
	if ValidateOrigin(req, config.Config{AllowedOrigins: []string{"https://good.example"}}) {
		t.Fatal("expected origin to be rejected")
	}
}
