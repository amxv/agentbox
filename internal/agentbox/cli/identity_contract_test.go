package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"agentbox/internal/agentbox/profiles"
)

func TestSavedProfileContainsNoTenantIdentity(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	if _, err := profiles.SaveProfile(profiles.Profile{
		Name:     "local",
		BaseURL:  "https://agentbox.example.com",
		APIKey:   "agb_secret",
		UserID:   "usr_123",
		KeyName:  "local-macbook",
		AuthType: "api_key",
	}, true); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(profiles.DefaultConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte("tenant_id"), []byte("tenant_slug"), []byte("tenant_name")} {
		if bytes.Contains(payload, forbidden) {
			t.Fatalf("profile JSON contains legacy tenant identity: %s", payload)
		}
	}
}

func TestRemovedCompatibilityCommandsAreUnavailable(t *testing.T) {
	for _, command := range [][]string{{"provision", "tenant"}, {"init"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		runner := &Runner{Stdout: &stdout, Stderr: &stderr, Stdin: strings.NewReader("")}
		if code := runner.Run(command); code == 0 {
			t.Fatalf("removed command %v unexpectedly succeeded: stdout=%s", command, stdout.String())
		}
		if !strings.Contains(strings.ToLower(stderr.String()), "unknown command") {
			t.Fatalf("removed command %v error=%s", command, stderr.String())
		}
	}
}
