package config

import "testing"

func TestLoadFromEnvDefaultsAndVercelMultipartLimit(t *testing.T) {
	t.Setenv("VERCEL_ENV", "production")
	t.Setenv("AGENTBOX_MAX_FILE_SIZE_BYTES", "26214400")

	cfg := LoadFromEnv()
	if !cfg.IsProduction() {
		t.Fatal("expected Vercel production to be production")
	}
	if cfg.MaxFileSizeBytes != 26214400 {
		t.Fatalf("MaxFileSizeBytes = %d", cfg.MaxFileSizeBytes)
	}
	if cfg.MultipartLimitBytes != VercelMaxPayloadBytes {
		t.Fatalf("MultipartLimitBytes = %d", cfg.MultipartLimitBytes)
	}
	if !cfg.SecureCookies {
		t.Fatal("expected production cookies to be secure by default")
	}
	if cfg.SessionCookieName != DefaultSessionCookieName {
		t.Fatalf("SessionCookieName = %q", cfg.SessionCookieName)
	}
}

func TestLoadFromEnvNonVercelMultipartKeepsConfiguredLimit(t *testing.T) {
	t.Setenv("AGENTBOX_MAX_FILE_SIZE_BYTES", "12345")
	cfg := LoadFromEnv()
	if cfg.MultipartLimitBytes != 12345 {
		t.Fatalf("MultipartLimitBytes = %d", cfg.MultipartLimitBytes)
	}
	if cfg.DBPoolSize != DefaultDBPoolSize {
		t.Fatalf("DBPoolSize = %d", cfg.DBPoolSize)
	}
	if cfg.SecureCookies {
		t.Fatal("expected non-production cookies to default to insecure")
	}
}

func TestLoadFromEnvAuthSettings(t *testing.T) {
	t.Setenv("AGENTBOX_APP_PUBLIC_URL", "https://agentbox.example.com/")
	t.Setenv("AGENTBOX_SESSION_COOKIE_NAME", "custom_session")
	t.Setenv("AGENTBOX_SESSION_SECRET", "session-secret")
	t.Setenv("AGENTBOX_TOKEN_HASH_PEPPER", "pepper")
	t.Setenv("AGENTBOX_SECURE_COOKIES", "true")

	cfg := LoadFromEnv()
	if cfg.AppPublicURL != "https://agentbox.example.com" {
		t.Fatalf("AppPublicURL = %q", cfg.AppPublicURL)
	}
	if cfg.SessionCookieName != "custom_session" {
		t.Fatalf("SessionCookieName = %q", cfg.SessionCookieName)
	}
	if cfg.SessionSecret != "session-secret" {
		t.Fatalf("SessionSecret = %q", cfg.SessionSecret)
	}
	if cfg.TokenHashPepper != "pepper" {
		t.Fatalf("TokenHashPepper = %q", cfg.TokenHashPepper)
	}
	if !cfg.SecureCookies {
		t.Fatal("expected secure cookie override")
	}
}

func TestLoadFromEnvMaintenanceSettings(t *testing.T) {
	t.Setenv("AGENTBOX_MAINTENANCE_MODE", "true")
	t.Setenv("AGENTBOX_MAINTENANCE_BYPASS_KEY", "  maintenance-secret  ")

	cfg := LoadFromEnv()
	if !cfg.MaintenanceMode || cfg.MaintenanceBypassKey != "maintenance-secret" {
		t.Fatalf("maintenance config = %#v", cfg)
	}
}

func TestTrustedAppPublicURLNormalizesOnlyDashboardOrigins(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		want    string
		wantErr bool
	}{
		{name: "development fallback omitted", config: Config{}, want: ""},
		{name: "https origin", config: Config{AppPublicURL: "https://Dashboard.Example:8443/"}, want: "https://Dashboard.Example:8443"},
		{name: "development http", config: Config{AppPublicURL: "http://127.0.0.1:3000"}, want: "http://127.0.0.1:3000"},
		{name: "production missing", config: Config{Environment: "production"}, wantErr: true},
		{name: "production http", config: Config{Environment: "production", AppPublicURL: "http://dashboard.example"}, wantErr: true},
		{name: "path rejected", config: Config{AppPublicURL: "https://dashboard.example/share"}, wantErr: true},
		{name: "query rejected", config: Config{AppPublicURL: "https://dashboard.example?next=evil"}, wantErr: true},
		{name: "userinfo rejected", config: Config{AppPublicURL: "https://user@dashboard.example"}, wantErr: true},
		{name: "non-http rejected", config: Config{AppPublicURL: "javascript:alert(1)"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.config.TrustedAppPublicURL()
			if test.wantErr {
				if err == nil {
					t.Fatalf("TrustedAppPublicURL()=%q, want error", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("TrustedAppPublicURL()=%q err=%v want=%q", got, err, test.want)
			}
		})
	}
}
