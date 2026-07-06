package profiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTypeScriptCreatedProfilesJSON(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	raw := `{
  "active_profile": "prod",
  "profiles": {
    "prod": {
      "base_url": "https://agentbox.example.com/",
      "api_key": "secret-prod"
    },
    "camel": {
      "baseUrl": "https://camel.example.com///",
      "apiKey": "secret-camel"
    }
  }
}`
	if err := os.WriteFile(DefaultConfigPath(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := ReadStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.ActiveProfileName != "prod" || len(store.Profiles) != 2 {
		t.Fatalf("store = %#v", store)
	}
	resolved, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name != "prod" || resolved.BaseURL != "https://agentbox.example.com" || resolved.APIKey != "secret-prod" || resolved.Source != "config" {
		t.Fatalf("resolved = %#v", resolved)
	}
	camel, err := Resolve("camel")
	if err != nil {
		t.Fatal(err)
	}
	if camel.BaseURL != "https://camel.example.com" || camel.APIKey != "secret-camel" {
		t.Fatalf("camel = %#v", camel)
	}
}

func TestGoWrittenProfileShapeIsTypeScriptCompatible(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTBOX_CONFIG_DIR", dir)
	if _, err := SaveProfile(Profile{Name: "default", BaseURL: "https://example.com/", APIKey: "secret"}, true); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ActiveProfile string `json:"active_profile"`
		Profiles      map[string]struct {
			BaseURL string `json:"base_url"`
			APIKey  string `json:"api_key"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ActiveProfile != "default" || payload.Profiles["default"].BaseURL != "https://example.com" || payload.Profiles["default"].APIKey != "secret" {
		t.Fatalf("payload = %#v", payload)
	}
	info, err := os.Stat(filepath.Join(dir, "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestProfileMetadataRoundTripsAndOldShapesStillParse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTBOX_CONFIG_DIR", dir)
	if _, err := SaveProfile(Profile{
		Name:       "tenant",
		BaseURL:    "https://example.com/",
		APIKey:     "secret",
		TenantID:   "ten_acme",
		TenantSlug: "acme",
		TenantName: "Acme",
		UserID:     "usr_123",
		KeyName:    "cli-workstation",
		AuthType:   "api_key",
	}, true); err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve("tenant")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TenantID != "ten_acme" || resolved.TenantSlug != "acme" || resolved.TenantName != "Acme" || resolved.UserID != "usr_123" || resolved.KeyName != "cli-workstation" || resolved.AuthType != "api_key" {
		t.Fatalf("resolved metadata = %#v", resolved.Profile)
	}

	parsed, err := ParseProfilesConfig(`[{"name":"old","base_url":"https://old.example.com","api_key":"old-key"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Name != "old" || parsed[0].TenantID != "" {
		t.Fatalf("parsed old shape = %#v", parsed)
	}
}

func TestEnvProfilePrecedenceAndLegacyFallback(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	if _, err := SaveProfile(Profile{Name: "stored", BaseURL: "https://stored.example.com", APIKey: "stored-key"}, true); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTBOX_PROFILES", `[{"name":"env","baseUrl":"https://env.example.com/","apiKey":"env-key"}]`)
	resolved, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name != "env" || resolved.Source != "env" || resolved.BaseURL != "https://env.example.com" {
		t.Fatalf("resolved = %#v", resolved)
	}

	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	t.Setenv("AGENTBOX_PROFILES", "")
	t.Setenv("AGENTBOX_BASE_URL", "https://legacy.example.com/")
	t.Setenv("AGENTBOX_API_KEY", "legacy-key")
	legacy, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Name != "default" || legacy.Source != "legacy-env" || legacy.BaseURL != "https://legacy.example.com" {
		t.Fatalf("legacy = %#v", legacy)
	}
}
