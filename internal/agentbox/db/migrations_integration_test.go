package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/types"
	migrationfiles "agentbox/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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

func migrateTestThrough(t *testing.T, repository *Repository, ctx context.Context, version string) error {
	t.Helper()
	migrations, err := migrationfiles.Load()
	if err != nil {
		return err
	}
	for index, migration := range migrations {
		if migration.Version == version {
			return repository.applyMigrations(ctx, migrations[:index+1])
		}
	}
	return fmt.Errorf("unknown test migration version %q", version)
}

func TestMigrateLegacySchemaIsIdempotentAndPreservesContent(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	migrations, err := migrationfiles.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateTestThrough(t, repository, ctx, "0004"); err != nil {
		t.Fatal(err)
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
	if err := migrateTestThrough(t, repository, ctx, "0005"); err != nil {
		t.Fatal(err)
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

	if err := migrateTestThrough(t, repository, ctx, "0016"); err != nil {
		t.Fatal(err)
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

	var preOwnerID *string
	if err := repository.pool.QueryRow(ctx, `select owner_user_id from threads where id = 'thr_legacy'`).Scan(&preOwnerID); err != nil {
		t.Fatal(err)
	}
	if preOwnerID != nil {
		t.Fatalf("legacy thread was assigned before explicit owner setup: %q", *preOwnerID)
	}
	if err := repository.Migrate(ctx); err == nil {
		t.Fatal("irreversible tenant removal succeeded without a permanent owner")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23514" || !strings.Contains(err.Error(), "permanent owner setup is required") {
			t.Fatalf("pre-owner final migration error = %v", err)
		}
	}
	var preCutoverMigrationCount int
	if err := repository.pool.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&preCutoverMigrationCount); err != nil {
		t.Fatal(err)
	}
	if preCutoverMigrationCount != 16 {
		t.Fatalf("failed final migration changed ledger count=%d", preCutoverMigrationCount)
	}

	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Deployment Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	var legacyOwnerUserID string
	var legacyCreatedBy string
	if err := repository.pool.QueryRow(ctx, `
select owner_user_id, created_by
from threads
where id = 'thr_legacy'
`).Scan(&legacyOwnerUserID, &legacyCreatedBy); err != nil {
		t.Fatal(err)
	}
	if legacyOwnerUserID != owner.ID || legacyCreatedBy != "Legacy owner" {
		t.Fatalf("legacy ownership backfill changed attribution: owner=%q want=%q created_by=%q", legacyOwnerUserID, owner.ID, legacyCreatedBy)
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
	var messageBody string
	var messageAuthor string
	var storageKey string
	var pendingStorageKey string
	if err := repository.pool.QueryRow(ctx, `select title from threads where id = 'thr_legacy'`).Scan(&threadTitle); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select body, author from messages where id = 'msg_legacy' and thread_id = 'thr_legacy'`).Scan(&messageBody, &messageAuthor); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select storage_key from assets where id = 'ast_legacy' and message_id = 'msg_legacy'`).Scan(&storageKey); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select storage_key from pending_uploads where id = 'upl_legacy' and thread_id = 'thr_legacy'`).Scan(&pendingStorageKey); err != nil {
		t.Fatal(err)
	}
	if threadTitle != "Legacy thread" || messageBody != "Preserve this body" || messageAuthor != "Legacy agent" || storageKey != "agentbox/legacy/key.bin" || pendingStorageKey != "agentbox/legacy/pending.bin" {
		t.Fatalf("legacy content changed: title=%q body=%q author=%q asset=%q pending=%q", threadTitle, messageBody, messageAuthor, storageKey, pendingStorageKey)
	}

	var tenantTable *string
	if err := repository.pool.QueryRow(ctx, `select to_regclass('public.tenants')::text`).Scan(&tenantTable); err != nil {
		t.Fatal(err)
	}
	var legacyColumnCount int
	if err := repository.pool.QueryRow(ctx, `
select count(*)::int
from information_schema.columns
where table_schema = 'public'
  and (
    column_name = 'tenant_id'
    or (table_name = 'users' and column_name = 'role')
    or (table_name in ('assets', 'pending_uploads') and column_name = 'public_url')
  )
`).Scan(&legacyColumnCount); err != nil {
		t.Fatal(err)
	}
	if tenantTable != nil || legacyColumnCount != 0 {
		t.Fatalf("tenant-era schema remains: tenants=%v columns=%d", tenantTable, legacyColumnCount)
	}

	access, err := repository.ResolveThreadAccess(ctx, owner.ID, "thr_legacy")
	if err != nil || access == nil || !access.IsOwner || len(access.MatchedTeamIDs) != 0 {
		t.Fatalf("legacy thread was not private to permanent owner: access=%#v err=%v", access, err)
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

func TestContentOrdinalMigrationBackfillsTiesAndEnforcesConstraints(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := migrateTestThrough(t, repository, ctx, "0016"); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "ordinal-backfill@example.com", "Ordinal Backfill", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateTestThrough(t, repository, ctx, "0022"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `
insert into threads (id, owner_user_id, title, created_by)
values ('thr_ordinal_backfill', $1, 'Ordinal backfill', 'test')
`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `
insert into messages (id, thread_id, author, body, created_at)
values
  ('msg_ordinal_b', 'thr_ordinal_backfill', 'test', 'second by deterministic legacy key', '2026-08-04T00:00:00Z'),
  ('msg_ordinal_a', 'thr_ordinal_backfill', 'test', 'first by deterministic legacy key', '2026-08-04T00:00:00Z')
`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `
insert into assets (id, message_id, storage_key, file_name, size_bytes, created_by, created_at)
values
  ('ast_ordinal_b', 'msg_ordinal_a', 'ordinal/b', 'b.txt', 1, 'test', '2026-08-04T00:00:00Z'),
  ('ast_ordinal_a', 'msg_ordinal_a', 'ordinal/a', 'a.txt', 1, 'test', '2026-08-04T00:00:00Z')
`); err != nil {
		t.Fatal(err)
	}
	if err := migrateTestThrough(t, repository, ctx, "0023"); err != nil {
		t.Fatal(err)
	}

	var messageOrder string
	if err := repository.pool.QueryRow(ctx, `
select string_agg(id || ':' || position::text, ',' order by position)
from messages where thread_id = 'thr_ordinal_backfill'
`).Scan(&messageOrder); err != nil {
		t.Fatal(err)
	}
	if messageOrder != "msg_ordinal_a:1,msg_ordinal_b:2" {
		t.Fatalf("message backfill order=%q", messageOrder)
	}
	var assetOrder string
	if err := repository.pool.QueryRow(ctx, `
select string_agg(id || ':' || position::text, ',' order by position)
from assets where message_id = 'msg_ordinal_a'
`).Scan(&assetOrder); err != nil {
		t.Fatal(err)
	}
	if assetOrder != "ast_ordinal_a:1,ast_ordinal_b:2" {
		t.Fatalf("asset backfill order=%q", assetOrder)
	}
	if _, err := repository.pool.Exec(ctx, `
insert into messages (id, thread_id, position, author, body)
values ('msg_ordinal_duplicate', 'thr_ordinal_backfill', 1, 'test', 'duplicate')
`); err == nil {
		t.Fatal("duplicate message ordinal was accepted")
	}
	if _, err := repository.pool.Exec(ctx, `
insert into assets (id, message_id, position, storage_key, file_name, size_bytes, created_by)
values ('ast_ordinal_zero', 'msg_ordinal_a', 0, 'ordinal/zero', 'zero.txt', 1, 'test')
`); err == nil {
		t.Fatal("non-positive asset ordinal was accepted")
	}
}

func TestContentOrdinalsPreserveLiveOrderAcrossReadersAndTimestampTies(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "ordinal-live@example.com", "Ordinal Live", "hash")
	if err != nil {
		t.Fatal(err)
	}
	auth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard", SessionID: "sess_ordinal_live"}
	thread, first, err := repository.CreateThreadWithMessage(ctx, owner.ID, "Ordinal live", auth, "first", nil)
	if err != nil {
		t.Fatal(err)
	}
	withAssets, err := repository.PostMessage(ctx, owner.ID, thread.ID, auth, "with-assets", nil, []types.NewAsset{
		{StorageKey: "ordinal/first", FileName: "first.txt", SizeBytes: 1},
		{StorageKey: "ordinal/second", FileName: "second.txt", SizeBytes: 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		message types.Message
		err     error
	}
	results := make(chan result, 4)
	start := make(chan struct{})
	for index := 0; index < 4; index++ {
		index := index
		go func() {
			<-start
			message, err := repository.PostMessage(context.Background(), owner.ID, thread.ID, auth, fmt.Sprintf("concurrent-%d", index), nil, nil)
			results <- result{message: message, err: err}
		}()
	}
	close(start)
	expected := []types.Message{first, withAssets}
	for index := 0; index < 4; index++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		expected = append(expected, result.message)
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i].Position < expected[j].Position })
	for index, message := range expected {
		if message.Position != int64(index+1) {
			t.Fatalf("allocated message positions=%#v", expected)
		}
	}
	if _, err := repository.pool.Exec(ctx, `update messages set created_at = '2026-08-04T00:00:00Z' where thread_id = $1`, thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `update assets set created_at = '2026-08-04T00:00:00Z' where message_id = $1`, withAssets.ID); err != nil {
		t.Fatal(err)
	}

	assertOrder := func(name string, messages []types.Message) {
		t.Helper()
		if len(messages) != len(expected) {
			t.Fatalf("%s message count=%d want=%d", name, len(messages), len(expected))
		}
		for index := range expected {
			if messages[index].Body != expected[index].Body {
				t.Fatalf("%s order[%d]=%q want=%q", name, index, messages[index].Body, expected[index].Body)
			}
		}
		if len(messages[1].Assets) != 2 || messages[1].Assets[0].FileName != "first.txt" || messages[1].Assets[1].FileName != "second.txt" {
			t.Fatalf("%s asset order=%#v", name, messages[1].Assets)
		}
	}

	authenticated, err := repository.GetThread(ctx, owner.ID, thread.ID)
	if err != nil || authenticated == nil {
		t.Fatalf("authenticated detail=%#v err=%v", authenticated, err)
	}
	assertOrder("authenticated", authenticated.Messages)
	ownerDetail, err := repository.GetOwnerContentThread(ctx, owner.ID, thread.ID)
	if err != nil || ownerDetail == nil {
		t.Fatalf("owner detail=%#v err=%v", ownerDetail, err)
	}
	assertOrder("owner", ownerDetail.Messages)

	publish := true
	token := "agpub_ordinal_live"
	if _, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &publish, PublicToken: token, PublicTokenHash: hashSecret(token), PublicTokenPrefix: "agpub_ordinal"}); err != nil {
		t.Fatal(err)
	}
	lease, err := repository.AcquirePublicThreadLease(ctx, hashSecret(token))
	if err != nil || lease == nil {
		t.Fatalf("public lease=%#v err=%v", lease, err)
	}
	publicThread := lease.Thread()
	if err := lease.Close(ctx); err != nil {
		t.Fatal(err)
	}
	assertOrder("public", publicThread.Messages)
}

func assertNotCompleted(t *testing.T, completed <-chan error, message string) {
	t.Helper()
	select {
	case err := <-completed:
		t.Fatalf("%s: err=%v", message, err)
	case <-time.After(150 * time.Millisecond):
	}
}

func waitForError(t *testing.T, completed <-chan error) error {
	t.Helper()
	select {
	case err := <-completed:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for serialized database operation")
		return nil
	}
}

func TestCredentialMigrationRemovesTenantAndPlaintextSecretColumns(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	for table, forbiddenColumns := range map[string][]string{
		"api_keys":        {"tenant_id", "key_value"},
		"user_sessions":   {"tenant_id"},
		"cli_login_codes": {"tenant_id"},
	} {
		for _, column := range forbiddenColumns {
			var exists bool
			if err := repository.pool.QueryRow(ctx, `
select exists (
  select 1
  from information_schema.columns
  where table_schema = current_schema()
    and table_name = $1
    and column_name = $2
)
`, table, column).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Fatalf("legacy identity column %s.%s still exists", table, column)
			}
		}
	}

	var purposeNotNull bool
	var userIDNotNull bool
	if err := repository.pool.QueryRow(ctx, `
select
  (select is_nullable = 'NO' from information_schema.columns where table_schema = current_schema() and table_name = 'api_keys' and column_name = 'purpose'),
  (select is_nullable = 'NO' from information_schema.columns where table_schema = current_schema() and table_name = 'api_keys' and column_name = 'user_id')
`).Scan(&purposeNotNull, &userIDNotNull); err != nil {
		t.Fatal(err)
	}
	if !purposeNotNull || !userIDNotNull {
		t.Fatalf("credential ownership columns are nullable: purpose=%t user_id=%t", purposeNotNull, userIDNotNull)
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

func TestRaycastOnboardingMigrationPreservesExistingConnectorRows(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := migrateTestThrough(t, repository, ctx, "0016"); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "raycast-migration-owner@example.com", "Raycast Migration Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateTestThrough(t, repository, ctx, "0017"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `
insert into user_onboarding (user_id)
values ($1)
`, owner.ID); err != nil {
		t.Fatal(err)
	}
	for _, connector := range []string{"local", "chatgpt", "claude"} {
		if _, err := repository.pool.Exec(ctx, `
insert into user_onboarding_steps (user_id, connector)
values ($1, $2)
`, owner.ID, connector); err != nil {
			t.Fatalf("seed %s onboarding step: %v", connector, err)
		}
	}

	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := repository.GetOnboardingState(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Steps) != 3 || state.Steps[0].Connector != "chatgpt" || state.Steps[1].Connector != "claude" || state.Steps[2].Connector != "local" {
		t.Fatalf("existing onboarding rows changed or reordered incorrectly: %#v", state.Steps)
	}
	if _, err := repository.pool.Exec(ctx, `
insert into user_onboarding_steps (user_id, connector)
values ($1, 'raycast')
`, owner.ID); err != nil {
		t.Fatalf("Raycast connector rejected after migration 0018: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `
insert into user_onboarding_steps (user_id, connector)
values ($1, 'unknown')
`, owner.ID); err == nil {
		t.Fatal("unknown onboarding connector was accepted after migration 0018")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23514" {
			t.Fatalf("unknown connector error = %v, want check violation", err)
		}
	}
	var migrationName string
	if err := repository.pool.QueryRow(ctx, `select name from schema_migrations where version = '0018'`).Scan(&migrationName); err != nil {
		t.Fatal(err)
	}
	if migrationName != "0018_raycast_onboarding.sql" {
		t.Fatalf("migration 0018 name = %q", migrationName)
	}
}

func TestListThreadsDoesNotRunMigrations(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	auth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, ActorName: "Web dashboard"}
	thread, err := repository.CreateThread(ctx, owner.ID, "Hot path", auth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `drop table schema_migrations`); err != nil {
		t.Fatal(err)
	}

	threads, err := repository.ListThreads(ctx, owner.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != thread.ID {
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
		if strings.TrimSpace(os.Getenv("AGENTBOX_REQUIRE_POSTGRES_TESTS")) == "1" {
			t.Fatal("TEST_DATABASE_URL is required because AGENTBOX_REQUIRE_POSTGRES_TESTS=1")
		}
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

	extensionTx, err := adminPool.Begin(ctx)
	if err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	if _, err := extensionTx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('agentbox-test-pgcrypto', 0))`); err != nil {
		_ = extensionTx.Rollback(ctx)
		adminPool.Close()
		t.Fatal(err)
	}
	if _, err := extensionTx.Exec(ctx, `create extension if not exists pgcrypto with schema public`); err != nil {
		_ = extensionTx.Rollback(ctx)
		adminPool.Close()
		t.Fatal(err)
	}
	var pgcryptoSchema string
	err = extensionTx.QueryRow(ctx, `
select n.nspname
from pg_extension e
join pg_namespace n on n.oid = e.extnamespace
where e.extname = 'pgcrypto'
`).Scan(&pgcryptoSchema)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_ = extensionTx.Rollback(ctx)
		adminPool.Close()
		t.Fatal("pgcrypto was not created")
	case err != nil:
		_ = extensionTx.Rollback(ctx)
		adminPool.Close()
		t.Fatal(err)
	case pgcryptoSchema != "public":
		if _, err := extensionTx.Exec(ctx, `alter extension pgcrypto set schema public`); err != nil {
			_ = extensionTx.Rollback(ctx)
			adminPool.Close()
			t.Fatal(err)
		}
	}
	if err := extensionTx.Commit(ctx); err != nil {
		adminPool.Close()
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
