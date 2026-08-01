package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/types"
	migrationfiles "agentbox/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateLegacySchemaIsIdempotentAndPreservesContent(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	migrations, err := migrationfiles.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:4] {
		if _, err := repository.pool.Exec(ctx, migration.SQL); err != nil {
			t.Fatalf("apply legacy migration %s: %v", migration.Name, err)
		}
	}

	legacyStatements := []string{
		`insert into threads (id, title, created_by) values ('thr_legacy', 'Legacy thread', 'Legacy owner')`,
		`insert into messages (id, thread_id, author, body, body_content_type) values ('msg_legacy', 'thr_legacy', 'Legacy agent', 'Preserve this body', 'text/markdown')`,
		`insert into assets (id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_by) values ('ast_legacy', 'msg_legacy', 'agentbox/legacy/key.bin', 'key.bin', 'application/octet-stream', 9, null, 'Legacy agent')`,
		`insert into pending_uploads (id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, expires_at, created_by) values ('upl_legacy', 'thr_legacy', 'agentbox/legacy/pending.bin', 'pending.bin', 'application/octet-stream', 11, null, now() + interval '1 hour', 'Legacy agent')`,
		`insert into api_keys (name, key_value) values ('legacy', 'legacy-secret')`,
	}
	for _, statement := range legacyStatements {
		if _, err := repository.pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	if _, err := repository.pool.Exec(ctx, migrations[4].SQL); err != nil {
		t.Fatalf("apply legacy auth migration %s: %v", migrations[4].Name, err)
	}
	legacyAuthStatements := []string{
		`insert into users (id, tenant_id, email, display_name, password_hash, role) values ('usr_legacy', 'ten_default', 'legacy@example.com', 'Legacy User', 'hash', 'admin')`,
		`insert into user_sessions (id, tenant_id, user_id, secret_hash, expires_at) values ('sess_legacy', 'ten_default', 'usr_legacy', 'session-hash', now() + interval '1 hour')`,
		`insert into cli_login_codes (id, tenant_id, user_id, code_hash, state_hash, redirect_uri, expires_at) values ('code_legacy', 'ten_default', 'usr_legacy', 'code-hash', 'state-hash', 'http://127.0.0.1:8080/callback', now() + interval '1 hour')`,
	}
	for _, statement := range legacyAuthStatements {
		if _, err := repository.pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed disposable legacy auth: %v", err)
		}
	}

	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var migrationCount int
	var firstLedgerSnapshot string
	if err := repository.pool.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != len(migrations) {
		t.Fatalf("schema_migrations count = %d, want %d", migrationCount, len(migrations))
	}
	if err := repository.pool.QueryRow(ctx, `
select string_agg(version || ':' || name || ':' || checksum || ':' || applied_at::text, ',' order by version)
from schema_migrations
`).Scan(&firstLedgerSnapshot); err != nil {
		t.Fatal(err)
	}

	var threadTitle string
	var threadTenant string
	if err := repository.pool.QueryRow(ctx, `select title, tenant_id from threads where id = 'thr_legacy'`).Scan(&threadTitle, &threadTenant); err != nil {
		t.Fatal(err)
	}
	if threadTitle != "Legacy thread" || threadTenant != "ten_default" {
		t.Fatalf("legacy thread changed: title=%q tenant=%q", threadTitle, threadTenant)
	}

	var messageBody string
	var storageKey string
	var pendingStorageKey string
	if err := repository.pool.QueryRow(ctx, `select body from messages where id = 'msg_legacy'`).Scan(&messageBody); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select storage_key from assets where id = 'ast_legacy'`).Scan(&storageKey); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select storage_key from pending_uploads where id = 'upl_legacy'`).Scan(&pendingStorageKey); err != nil {
		t.Fatal(err)
	}
	if messageBody != "Preserve this body" || storageKey != "agentbox/legacy/key.bin" || pendingStorageKey != "agentbox/legacy/pending.bin" {
		t.Fatalf("legacy content changed: body=%q asset=%q pending=%q", messageBody, storageKey, pendingStorageKey)
	}

	var usersCount int
	var sessionsCount int
	var codesCount int
	var activeKeyCount int
	if err := repository.pool.QueryRow(ctx, `
select
  (select count(*) from users),
  (select count(*) from user_sessions),
  (select count(*) from cli_login_codes),
  (select count(*) from api_keys)
`).Scan(&usersCount, &sessionsCount, &codesCount, &activeKeyCount); err != nil {
		t.Fatal(err)
	}
	if usersCount != 0 || sessionsCount != 0 || codesCount != 0 || activeKeyCount != 0 {
		t.Fatalf("legacy authentication data was retained: users=%d sessions=%d codes=%d api_keys=%d", usersCount, sessionsCount, codesCount, activeKeyCount)
	}

	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var secondLedgerSnapshot string
	if err := repository.pool.QueryRow(ctx, `
select string_agg(version || ':' || name || ':' || checksum || ':' || applied_at::text, ',' order by version)
from schema_migrations
`).Scan(&secondLedgerSnapshot); err != nil {
		t.Fatal(err)
	}
	if secondLedgerSnapshot != firstLedgerSnapshot {
		t.Fatalf("migration retry changed ledger\nfirst:  %s\nsecond: %s", firstLedgerSnapshot, secondLedgerSnapshot)
	}
}

func TestBootstrapOwnerIsUniqueIdempotentAndProtected(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	first, err := repository.BootstrapOwner(ctx, "owner@example.com", "Original Owner", "hash-one")
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsOwner || first.Role != "admin" || first.TenantID != types.DefaultTenantID {
		t.Fatalf("unexpected owner: %#v", first)
	}

	second, err := repository.BootstrapOwner(ctx, "OWNER@example.com", "Updated Owner", "hash-two")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.DisplayName != "Updated Owner" || second.PasswordHash == nil || *second.PasswordHash != "hash-two" {
		t.Fatalf("owner bootstrap was not idempotent: first=%#v second=%#v", first, second)
	}

	if _, err := repository.BootstrapOwner(ctx, "other@example.com", "Other", "hash-three"); !errors.Is(err, ErrOwnerAlreadyExists) {
		t.Fatalf("second owner error = %v, want ErrOwnerAlreadyExists", err)
	}

	var ownerCount int
	if err := repository.pool.QueryRow(ctx, `select count(*) from users where is_owner`).Scan(&ownerCount); err != nil {
		t.Fatal(err)
	}
	if ownerCount != 1 {
		t.Fatalf("owner count = %d, want 1", ownerCount)
	}

	for name, statement := range map[string]string{
		"demote":  `update users set is_owner = false where id = $1`,
		"disable": `update users set disabled_at = now() where id = $1`,
		"role":    `update users set role = 'member' where id = $1`,
		"delete":  `delete from users where id = $1`,
	} {
		if _, err := repository.pool.Exec(ctx, statement, first.ID); err == nil {
			t.Fatalf("%s owner mutation unexpectedly succeeded", name)
		}
	}

	if _, err := repository.UpsertTenant(ctx, types.Tenant{ID: "ten_other", Slug: "other", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertProvisionedUser(ctx, "ten_other", "owner@example.com", "Duplicate", nil, "member"); err == nil {
		t.Fatal("deployment-global email uniqueness was not enforced")
	}
}

func TestMigrateRejectsAppliedMigrationDrift(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `update schema_migrations set checksum = 'changed' where version = '0001'`); err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "drift detected") {
		t.Fatalf("Migrate error = %v, want drift detection", err)
	}
}

func TestListThreadsDoesNotRunMigrations(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `insert into threads (id, tenant_id, title, created_by) values ('thr_hotpath', 'ten_default', 'Hot path', 'test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `drop table schema_migrations`); err != nil {
		t.Fatal(err)
	}

	threads, err := repository.ListThreads(ctx, "ten_default", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != "thr_hotpath" {
		t.Fatalf("threads = %#v", threads)
	}

	var ledgerExists bool
	if err := repository.pool.QueryRow(ctx, `
select exists (
  select 1
  from information_schema.tables
  where table_schema = current_schema()
    and table_name = 'schema_migrations'
)
`).Scan(&ledgerExists); err != nil {
		t.Fatal(err)
	}
	if ledgerExists {
		t.Fatal("ListThreads recreated schema_migrations; request-time DDL is still active")
	}
}

func openPostgresTestRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; PostgreSQL integration test runs in CI")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}

	schemaName := "agentbox_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schemaName+",public")
	parsedURL.RawQuery = query.Encode()

	repository, err := Open(ctx, config.Config{
		DatabaseURL: parsedURL.String(),
		DBPoolSize:  4,
	})
	if err != nil {
		_, _ = adminPool.Exec(ctx, "drop schema "+quotedSchema+" cascade")
		adminPool.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		repository.Close()
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupContext, "drop schema "+quotedSchema+" cascade"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		adminPool.Close()
	})

	if err := repository.pool.Ping(ctx); err != nil {
		t.Fatal(fmt.Errorf("ping isolated PostgreSQL schema: %w", err))
	}
	return repository, ctx
}
