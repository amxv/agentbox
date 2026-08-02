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
	if _, err := repository.pool.Exec(ctx, `insert into threads (id, tenant_id, owner_user_id, title, created_by) values ('thr_snapshot', 'ten_default', $1, 'Snapshot', 'test')`, owner.ID); err != nil {
		t.Fatal(err)
	}

	statements := []struct {
		sql  string
		args []any
	}{
		{sql: `insert into messages (id, tenant_id, thread_id, author, body) values ('msg_snapshot', 'ten_default', 'thr_snapshot', 'test', 'body')`},
		{sql: `insert into assets (id, tenant_id, message_id, storage_key, file_name, size_bytes, created_by) values ('ast_snapshot', 'ten_default', 'msg_snapshot', 'agentbox/existing.bin', 'existing.bin', 12, 'test')`},
		{sql: `insert into assets (id, tenant_id, message_id, storage_key, file_name, size_bytes, created_by, created_by_user_id, purged_at, purged_by_user_id) values ('ast_snapshot_purged', 'ten_default', 'msg_snapshot', 'agentbox/deleted.bin', 'deleted.bin', 9, 'test', $1, now(), $1)`, args: []any{owner.ID}},
		{sql: `insert into pending_uploads (id, tenant_id, thread_id, storage_key, file_name, size_bytes, expires_at, created_by) values ('upl_snapshot', 'ten_default', 'thr_snapshot', 'agentbox/pending.bin', 'pending.bin', 15, now() + interval '1 hour', 'test')`},
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
	if data.References[0] != (backup.ObjectReference{Kind: "asset", RecordID: "ast_snapshot", StorageKey: "agentbox/existing.bin", SizeBytes: 12}) {
		t.Fatalf("unexpected asset reference: %#v", data.References[0])
	}
	if data.References[1] != (backup.ObjectReference{Kind: "pending_upload", RecordID: "upl_snapshot", StorageKey: "agentbox/pending.bin", SizeBytes: 15}) {
		t.Fatalf("unexpected pending-upload reference: %#v", data.References[1])
	}
	if err := snapshot.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(ctx); err != nil {
		t.Fatalf("second Close should be idempotent: %v", err)
	}
}

func TestPGDumpCreatesReadableArchiveFromExportedSnapshot(t *testing.T) {
	pgDumpPath, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Skip("pg_dump is not installed; PostgreSQL backup integration runs in CI")
	}
	pgRestorePath, err := exec.LookPath("pg_restore")
	if err != nil {
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
	if _, err := repository.pool.Exec(ctx, `insert into threads (id, tenant_id, owner_user_id, title, created_by) values ('thr_dump', 'ten_default', $1, 'Dump fixture', 'test')`, owner.ID); err != nil {
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
