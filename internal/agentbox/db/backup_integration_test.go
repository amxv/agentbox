package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/agentbox/backup"
)

func TestOpenContentSnapshotReportsCountsReferencesAndOrphans(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `insert into threads (id, owner_user_id, title, created_by) values ('thr_snapshot', $1, 'Snapshot', 'test')`, owner.ID); err != nil {
		t.Fatal(err)
	}

	statements := []struct {
		sql  string
		args []any
	}{
		{sql: `insert into messages (id, thread_id, author, body) values ('msg_snapshot', 'thr_snapshot', 'test', 'body')`},
		{sql: `insert into assets (id, message_id, storage_key, file_name, size_bytes, created_by) values ('ast_snapshot', 'msg_snapshot', 'agentbox/existing.bin', 'existing.bin', 12, 'test')`},
		{sql: `insert into assets (id, message_id, storage_key, file_name, size_bytes, created_by, created_by_user_id, purged_at, purged_by_user_id) values ('ast_snapshot_purged', 'msg_snapshot', 'agentbox/deleted.bin', 'deleted.bin', 9, 'test', $1, now(), $1)`, args: []any{owner.ID}},
		{sql: `insert into pending_uploads (id, thread_id, storage_key, file_name, size_bytes, expires_at, created_by) values ('upl_snapshot', 'thr_snapshot', 'agentbox/pending.bin', 'pending.bin', 15, now() + interval '1 hour', 'test')`},
		{sql: `insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before) values ('ucl_snapshot', 'upl_snapshot', 'agentbox/pending.bin', 'staging', now() + interval '1 hour')`},
	}
	for _, statement := range statements {
		if _, err := repository.pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := repository.OpenContentSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := snapshot.Data()
	if data.SnapshotID == "" {
		t.Fatal("PostgreSQL snapshot ID is empty")
	}
	if data.Counts != (backup.TableCounts{Threads: 1, Messages: 1, Assets: 2, PendingUploads: 1}) {
		t.Fatalf("unexpected counts: %#v", data.Counts)
	}
	if data.Orphans.Total() != 0 {
		t.Fatalf("unexpected orphan counts: %#v", data.Orphans)
	}
	if len(data.References) != 2 {
		t.Fatalf("references = %#v", data.References)
	}
	if data.References[0] != (backup.ObjectReference{Kind: "asset", RecordID: "ast_snapshot", StorageKey: "agentbox/existing.bin", SizeBytes: 12, MissingBlocks: true}) {
		t.Fatalf("unexpected asset reference: %#v", data.References[0])
	}
	if data.References[1] != (backup.ObjectReference{Kind: "pending_upload", RecordID: "upl_snapshot:staging", StorageKey: "agentbox/pending.bin", SizeBytes: 15, MissingBlocks: false}) {
		t.Fatalf("unexpected pending-upload reference: %#v", data.References[1])
	}
	if err := snapshot.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(ctx); err != nil {
		t.Fatalf("second Close should be idempotent: %v", err)
	}
}

func TestOpenContentSnapshotRunsAgainstExactLegacy0005Schema(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.migrateThrough(ctx, "0005"); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`insert into threads (id, tenant_id, title, created_by) values ('thr_legacy_backup', 'ten_default', 'Legacy backup', 'legacy')`,
		`insert into messages (id, thread_id, tenant_id, author, body) values ('msg_legacy_backup', 'thr_legacy_backup', 'ten_default', 'legacy', 'body')`,
		`insert into assets (id, message_id, tenant_id, storage_key, file_name, size_bytes, created_by) values ('ast_legacy_backup', 'msg_legacy_backup', 'ten_default', 'opaque/legacy.bin', 'legacy.bin', 7, 'legacy')`,
		`insert into pending_uploads (id, thread_id, tenant_id, storage_key, file_name, size_bytes, expires_at, created_by) values ('upl_legacy_backup', 'thr_legacy_backup', 'ten_default', 'opaque/not-yet-uploaded.bin', 'pending.bin', 9, now() + interval '1 hour', 'legacy')`,
	}
	for _, statement := range statements {
		if _, err := repository.pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := repository.OpenContentSnapshot(ctx)
	if err != nil {
		t.Fatalf("legacy 0005 preflight snapshot failed: %v", err)
	}
	defer snapshot.Close(ctx)
	data := snapshot.Data()
	if data.Counts != (backup.TableCounts{Threads: 1, Messages: 1, Assets: 1, PendingUploads: 1}) {
		t.Fatalf("legacy counts=%#v", data.Counts)
	}
	if len(data.References) != 2 || !data.References[0].MissingBlocks || data.References[1].MissingBlocks {
		t.Fatalf("legacy references=%#v", data.References)
	}
	if len(data.ThreadOwnership) != 1 || data.ThreadOwnership[0].ThreadID != "thr_legacy_backup" || data.ThreadOwnership[0].OwnerUserID != "" {
		t.Fatalf("legacy ownership proposal input=%#v", data.ThreadOwnership)
	}
}

func TestOpenContentSnapshotRepresentsStagingAndFinalCleanupStates(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `insert into threads (id, owner_user_id, title, created_by) values ('thr_pending_inventory', $1, 'Pending inventory', 'test')`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `insert into messages (id, thread_id, author, body) values ('msg_pending_inventory', 'thr_pending_inventory', 'test', 'body')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `insert into assets (id, message_id, storage_key, file_name, size_bytes, created_by) values ('ast_pending_inventory', 'msg_pending_inventory', 'shared/key.bin', 'shared.bin', 3, 'test')`); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`insert into pending_uploads (id, thread_id, storage_key, file_name, size_bytes, expires_at, created_by, consumed_at) values ('upl_consumed', 'thr_pending_inventory', 'consumed/key.bin', 'consumed.bin', 1, now()+interval '1 hour', 'test', now())`,
		`insert into pending_uploads (id, thread_id, storage_key, file_name, size_bytes, expires_at, created_by) values ('upl_expired', 'thr_pending_inventory', 'expired/key.bin', 'expired.bin', 1, now()-interval '1 minute', 'test')`,
		`insert into pending_uploads (id, thread_id, storage_key, file_name, size_bytes, expires_at, created_by) values ('upl_asset_backed', 'thr_pending_inventory', 'shared/key.bin', 'shared.bin', 3, now()+interval '1 hour', 'test')`,
		`insert into pending_uploads (id, thread_id, storage_key, file_name, size_bytes, expires_at, created_by) values ('upl_active', 'thr_pending_inventory', 'active/key.bin', 'active.bin', 2, now()+interval '1 hour', 'test')`,
		`insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before) values ('ucl_consumed', 'upl_consumed', 'consumed/key.bin', 'staging', now())`,
		`insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before) values ('ucl_expired', 'upl_expired', 'expired/key.bin', 'staging', now())`,
		`insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before) values ('ucl_asset_backed', 'upl_asset_backed', 'shared/key.bin', 'staging', now())`,
		`insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before) values ('ucl_active_staging', 'upl_active', 'active/key.bin', 'staging', now()+interval '1 hour')`,
		`insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before) values ('ucl_active_final', 'upl_active', 'final/key.bin', 'final_candidate', now()+interval '10 minutes')`,
		`insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before, cleaned_at) values ('ucl_cleaned', 'upl_expired', 'cleaned/key.bin', 'staging', now(), now())`,
	}
	for _, statement := range statements {
		if _, err := repository.pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := repository.OpenContentSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close(ctx)
	data := snapshot.Data()
	if len(data.References) != 5 {
		t.Fatalf("inventory should contain one asset and four unmanaged cleanup objects: %#v", data.References)
	}
	references := map[string]backup.ObjectReference{}
	for _, reference := range data.References {
		references[reference.RecordID] = reference
	}
	for _, expected := range []string{"ast_pending_inventory", "upl_consumed:staging", "upl_expired:staging", "upl_active:staging", "upl_active:final_candidate"} {
		if _, ok := references[expected]; !ok {
			t.Fatalf("cleanup state %q missing from backup inventory: %#v", expected, data.References)
		}
	}
	if _, ok := references["upl_asset_backed:staging"]; ok {
		t.Fatalf("canonical asset-backed cleanup key was duplicated: %#v", data.References)
	}
	if _, ok := references["upl_expired:cleaned"]; ok {
		t.Fatalf("cleaned object remained in backup inventory: %#v", data.References)
	}
}
func TestPGDumpCreatesReadableArchiveFromExportedSnapshot(t *testing.T) {
	pgDumpPath, err := exec.LookPath("pg_dump")
	if err != nil {
		if strings.TrimSpace(os.Getenv("AGENTBOX_REQUIRE_POSTGRES_TESTS")) == "1" {
			t.Fatal("pg_dump is required because AGENTBOX_REQUIRE_POSTGRES_TESTS=1")
		}
		t.Skip("pg_dump is not installed; PostgreSQL backup integration runs in CI")
	}
	pgRestorePath, err := exec.LookPath("pg_restore")
	if err != nil {
		if strings.TrimSpace(os.Getenv("AGENTBOX_REQUIRE_POSTGRES_TESTS")) == "1" {
			t.Fatal("pg_restore is required because AGENTBOX_REQUIRE_POSTGRES_TESTS=1")
		}
		t.Skip("pg_restore is not installed; PostgreSQL backup integration runs in CI")
	}

	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `insert into threads (id, owner_user_id, title, created_by) values ('thr_dump', $1, 'Dump fixture', 'test')`, owner.ID); err != nil {
		t.Fatal(err)
	}

	snapshot, err := repository.OpenContentSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dumpPath := filepath.Join(t.TempDir(), "database.dump")
	dumper := backup.PGDump{
		DatabaseURL: os.Getenv("TEST_DATABASE_URL"),
		Binary:      pgDumpPath,
	}
	if err := dumper.Dump(ctx, snapshot.Data().SnapshotID, dumpPath); err != nil {
		_ = snapshot.Close(ctx)
		t.Fatal(err)
	}
	if err := snapshot.Close(ctx); err != nil {
		t.Fatal(err)
	}

	output, err := exec.CommandContext(ctx, pgRestorePath, "--list", dumpPath).CombinedOutput()
	if err != nil {
		t.Fatalf("pg_restore --list failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "threads") || !strings.Contains(string(output), "TABLE DATA") {
		t.Fatalf("custom archive did not contain thread table data:\n%s", output)
	}
	info, err := os.Stat(dumpPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("pg_dump produced an empty archive")
	}
}
