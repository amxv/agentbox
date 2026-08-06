package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/config"
)

func TestParseOptionsUsesSafeDefaults(t *testing.T) {
	clearBackupEnv(t)
	options, err := parseOptions([]string{"--output-dir", "/secure/backups", "--owner-email", "owner@example.com"}, config.Config{R2Bucket: "attachments"}, &bytes.Buffer{}, time.Date(2026, 8, 1, 12, 34, 56, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if options.RunID != "20260801T123456Z" {
		t.Fatalf("run ID = %q", options.RunID)
	}
	if options.SourceBucket != "attachments" || options.BackupBucket != "attachments" {
		t.Fatalf("unexpected buckets: %#v", options)
	}
	if options.BackupPrefix != "agentbox-recovery/20260801T123456Z" {
		t.Fatalf("backup prefix = %q", options.BackupPrefix)
	}
	if options.PGDumpBinary != "pg_dump" || options.Timeout != defaultPreflightTimeout {
		t.Fatalf("unexpected command defaults: %#v", options)
	}
}

func TestParseOptionsAllowsExplicitRecoveryDestination(t *testing.T) {
	clearBackupEnv(t)
	options, err := parseOptions([]string{
		"--output-dir", "/secure/backups",
		"--run-id", "cutover-1",
		"--source-bucket", "source",
		"--source-prefix", "/agentbox/",
		"--backup-bucket", "recovery",
		"--backup-prefix", "/snapshots/cutover-1/",
		"--pg-dump-binary", "/usr/local/bin/pg_dump",
		"--owner-email", "owner@example.com",
		"--timeout", "30m",
	}, config.Config{}, &bytes.Buffer{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if options.SourcePrefix != "agentbox/" || options.BackupPrefix != "snapshots/cutover-1" || options.BackupBucket != "recovery" {
		t.Fatalf("unexpected explicit options: %#v", options)
	}
	if options.Timeout != 30*time.Minute {
		t.Fatalf("timeout = %s", options.Timeout)
	}
}

func TestParseOptionsRequiresOutputDirectory(t *testing.T) {
	clearBackupEnv(t)
	_, err := parseOptions([]string{"--owner-email", "owner@example.com"}, config.Config{R2Bucket: "attachments"}, &bytes.Buffer{}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "output-dir") {
		t.Fatalf("error = %v", err)
	}
}

func clearBackupEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"AGENTBOX_BACKUP_OUTPUT_DIR",
		"AGENTBOX_BACKUP_SOURCE_PREFIX",
		"AGENTBOX_BACKUP_BUCKET",
		"AGENTBOX_BACKUP_PREFIX",
		"AGENTBOX_PG_DUMP_BINARY",
		"AGENTBOX_OWNER_EMAIL",
	} {
		t.Setenv(name, "")
	}
}

func TestValidateConfigNamesMissingValuesWithoutSecrets(t *testing.T) {
	err := validateConfig(config.Config{}, commandOptions{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, name := range []string{"DATABASE_URL", "R2_ACCOUNT_ID", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "R2_BUCKET"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error %q did not mention %s", err.Error(), name)
		}
	}
}
