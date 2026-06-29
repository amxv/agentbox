package main

import (
	"strings"
	"testing"

	"agentbox/internal/agentbox/config"
)

func TestValidateRuntimeConfigRequiresPersistentDependencies(t *testing.T) {
	err := validateRuntimeConfig(config.Config{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, name := range []string{
		"DATABASE_URL",
		"R2_ACCOUNT_ID",
		"R2_ACCESS_KEY_ID",
		"R2_SECRET_ACCESS_KEY",
		"R2_BUCKET",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error %q did not mention %s", err.Error(), name)
		}
	}
}

func TestValidateRuntimeConfigAcceptsConfiguredBackend(t *testing.T) {
	err := validateRuntimeConfig(config.Config{
		DatabaseURL:       "postgres://example",
		R2AccountID:       "acct",
		R2AccessKeyID:     "key",
		R2SecretAccessKey: "secret",
		R2Bucket:          "bucket",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
