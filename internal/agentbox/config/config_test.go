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
}
