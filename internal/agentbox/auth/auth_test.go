package auth

import (
	"net/http/httptest"
	"testing"

	"agentbox/internal/agentbox/config"
)

func TestAdminKeysAndOrigin(t *testing.T) {
	if err := RequireAdminKey("admin-secret", config.Config{AdminKey: "admin-secret", Environment: "production"}); err != nil {
		t.Fatal(err)
	}
	if err := RequireAdminKey("wrong", config.Config{AdminKey: "admin-secret", Environment: "production"}); err == nil || err.Error() != "Unauthorized" {
		t.Fatalf("expected unauthorized, got %v", err)
	}

	req := httptest.NewRequest("GET", "/api/mcp", nil)
	req.Header.Set("origin", "https://bad.example")
	if ValidateOrigin(req, config.Config{AllowedOrigins: []string{"https://good.example"}}) {
		t.Fatal("expected origin to be rejected")
	}
}
