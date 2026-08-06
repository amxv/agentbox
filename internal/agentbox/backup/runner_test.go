package backup_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/backup"
)

func TestRunnerProducesReadyManifestAndIsIdempotent(t *testing.T) {
	outputDir := t.TempDir()
	store := &assets.FakeStore{}
	store.PutObject("primary", "agentbox/a.bin", 3, "etag-a")
	store.PutObject("primary", "agentbox/b.bin", 5, "etag-b")
	store.PutObject("primary", "agentbox/unreferenced.bin", 7, "etag-extra")

	snapshotSource := &fakeSnapshotSource{data: backup.SnapshotData{
		SnapshotID: "00000003-0000001B-1",
		Counts: backup.TableCounts{
			Threads:        1,
			Messages:       2,
			Assets:         2,
			PendingUploads: 1,
		},
		ThreadOwnership: []backup.ThreadOwnership{{ThreadID: "thr_2"}, {ThreadID: "thr_1"}},
		References: []backup.ObjectReference{
			{Kind: "asset", RecordID: "ast_1", MissingBlocks: true, StorageKey: "agentbox/a.bin", SizeBytes: 3},
			{Kind: "asset", RecordID: "ast_2", MissingBlocks: true, StorageKey: "agentbox/b.bin", SizeBytes: 5},
			{Kind: "pending_upload", RecordID: "upl_1", StorageKey: "agentbox/b.bin", SizeBytes: 5},
		},
	}}
	dumper := &fakeDumper{contents: []byte("recoverable postgres dump")}
	runner := backup.Runner{Snapshots: snapshotSource, Dumper: dumper, Objects: store}
	options := backup.Options{
		ProposedOwnerEmail: "owner@example.com",
		RunID:              "run-1",
		OutputDir:          outputDir,
		SourceBucket:       "primary",
		BackupBucket:       "primary",
		BackupPrefix:       "agentbox-recovery/run-1",
		Now: func() time.Time {
			return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		},
	}

	manifest, err := runner.Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Ready {
		t.Fatalf("manifest is not ready: %#v", manifest.Issues)
	}
	if manifest.OwnerBackfill.ProposedOwnerEmail != "owner@example.com" || manifest.OwnerBackfill.ProposedOwnerID == "" || manifest.OwnerBackfill.ThreadCount != 2 || strings.Join(manifest.OwnerBackfill.ThreadIDs, ",") != "thr_1,thr_2" || manifest.OwnerBackfill.ThreadIDsSHA256 == "" {
		t.Fatalf("owner backfill manifest=%#v", manifest.OwnerBackfill)
	}
	if manifest.Objects.ReferencedObjectCount != 2 || manifest.Objects.CopiedObjectCount != 2 {
		t.Fatalf("unexpected object summary: %#v", manifest.Objects)
	}
	if manifest.Objects.UnreferencedCount != 1 || len(manifest.Issues) != 1 || manifest.Issues[0].Severity != "warning" {
		t.Fatalf("unexpected unreferenced-object report: %#v", manifest)
	}
	if len(store.CopyCalls) != 2 {
		t.Fatalf("copy calls = %d, want 2", len(store.CopyCalls))
	}
	wantChecksumBytes := sha256.Sum256(dumper.contents)
	wantChecksum := hex.EncodeToString(wantChecksumBytes[:])
	if manifest.Database.DumpSHA256 != wantChecksum || manifest.Database.DumpSizeBytes != int64(len(dumper.contents)) {
		t.Fatalf("unexpected dump metadata: %#v", manifest.Database)
	}
	dumpContents, err := os.ReadFile(filepath.Join(manifest.RunDirectory, manifest.Database.DumpFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(dumpContents) != string(dumper.contents) {
		t.Fatalf("dump contents = %q", dumpContents)
	}

	manifestPath := filepath.Join(manifest.RunDirectory, "manifest.json")
	if err := backup.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	writtenManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(writtenManifest), `"ready": true`) || !strings.Contains(string(writtenManifest), `"agentbox/a.bin"`) {
		t.Fatalf("manifest file is incomplete: %s", writtenManifest)
	}

	secondManifest, err := runner.Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !secondManifest.Ready || secondManifest.Objects.CopiedObjectCount != 0 || secondManifest.Objects.AlreadyBackedUpCount != 2 {
		t.Fatalf("unexpected rerun summary: %#v", secondManifest.Objects)
	}
	if len(store.CopyCalls) != 2 {
		t.Fatalf("rerun issued additional copies: %d", len(store.CopyCalls))
	}
	if snapshotSource.closeCount != 2 {
		t.Fatalf("snapshot close count = %d, want 2", snapshotSource.closeCount)
	}
}

func TestRunnerReportsMissingReferencedObjectAndStillWritesDump(t *testing.T) {
	store := &assets.FakeStore{}
	store.PutObject("primary", "agentbox/present-1.bin", 3, "etag-1")
	store.PutObject("primary", "agentbox/present-2.bin", 4, "etag-2")
	runner := backup.Runner{
		Snapshots: &fakeSnapshotSource{data: backup.SnapshotData{
			SnapshotID: "snapshot-missing",
			Counts:     backup.TableCounts{Threads: 1, Messages: 1, Assets: 3},
			References: []backup.ObjectReference{
				{Kind: "asset", RecordID: "ast_1", MissingBlocks: true, StorageKey: "agentbox/present-1.bin", SizeBytes: 3},
				{Kind: "asset", RecordID: "ast_2", MissingBlocks: true, StorageKey: "agentbox/present-2.bin", SizeBytes: 4},
				{Kind: "asset", RecordID: "ast_3", MissingBlocks: true, StorageKey: "agentbox/missing.bin", SizeBytes: 5},
			},
		}},
		Dumper:  &fakeDumper{contents: []byte("dump survives missing R2 object")},
		Objects: store,
	}

	manifest, err := runner.Run(context.Background(), backup.Options{
		ProposedOwnerEmail: "owner@example.com",
		RunID:              "missing",
		OutputDir:          t.TempDir(),
		SourceBucket:       "primary",
		BackupBucket:       "recovery",
		BackupPrefix:       "run/missing",
	})
	if !errors.Is(err, backup.ErrPreflightNotReady) {
		t.Fatalf("Run error = %v, want ErrPreflightNotReady", err)
	}
	if manifest.Ready || manifest.Objects.MissingObjectCount != 1 || manifest.Objects.CopiedObjectCount != 2 {
		t.Fatalf("unexpected manifest summary: %#v", manifest)
	}
	if !manifestHasIssue(manifest, "referenced_object_missing", "agentbox/missing.bin") {
		t.Fatalf("missing object was not named in issues: %#v", manifest.Issues)
	}
	if _, err := os.Stat(filepath.Join(manifest.RunDirectory, manifest.Database.DumpFile)); err != nil {
		t.Fatalf("database dump was not retained: %v", err)
	}
}

func TestRunnerBlocksReadinessForOrphansAndDumpFailure(t *testing.T) {
	runner := backup.Runner{
		Snapshots: &fakeSnapshotSource{data: backup.SnapshotData{
			SnapshotID: "snapshot-orphans",
			Orphans: backup.OrphanCounts{
				AssetsWithoutMessage: 1,
			},
		}},
		Dumper:  &fakeDumper{err: errors.New("pg_dump failed")},
		Objects: &assets.FakeStore{},
	}
	manifest, err := runner.Run(context.Background(), backup.Options{
		ProposedOwnerEmail: "owner@example.com",
		RunID:              "orphaned",
		OutputDir:          t.TempDir(),
		SourceBucket:       "primary",
		BackupBucket:       "recovery",
		BackupPrefix:       "run/orphaned",
	})
	if !errors.Is(err, backup.ErrPreflightNotReady) || manifest.Ready {
		t.Fatalf("Run = ready %v, error %v", manifest.Ready, err)
	}
	if !manifestHasIssue(manifest, "database_orphan_rows", "") || !manifestHasIssue(manifest, "database_dump_failed", "") {
		t.Fatalf("expected orphan and dump issues: %#v", manifest.Issues)
	}
}

func TestRunnerTreatsUnmaterializedPendingIntentAsWarning(t *testing.T) {
	runner := backup.Runner{
		Snapshots: &fakeSnapshotSource{data: backup.SnapshotData{
			SnapshotID:      "snapshot-pending",
			ThreadOwnership: []backup.ThreadOwnership{{ThreadID: "thr_pending"}},
			References:      []backup.ObjectReference{{Kind: "pending_upload", RecordID: "upl_pending", StorageKey: "agentbox/not-materialized.bin", SizeBytes: 4}},
		}},
		Dumper:  &fakeDumper{contents: []byte("dump")},
		Objects: &assets.FakeStore{},
	}
	manifest, err := runner.Run(context.Background(), backup.Options{ProposedOwnerEmail: "owner@example.com", RunID: "pending", OutputDir: t.TempDir(), SourceBucket: "primary", BackupBucket: "recovery", BackupPrefix: "run/pending"})
	if err != nil || !manifest.Ready || manifest.Objects.MissingObjectCount != 0 || manifest.Objects.UnmaterializedPendingCount != 1 || !manifestHasIssue(manifest, "pending_upload_not_materialized", "agentbox/not-materialized.bin") {
		t.Fatalf("pending intent manifest=%#v err=%v", manifest, err)
	}
}

func TestRunnerBlocksAmbiguousOwnerBackfill(t *testing.T) {
	runner := backup.Runner{
		Snapshots: &fakeSnapshotSource{data: backup.SnapshotData{
			SnapshotID: "snapshot-owner-ambiguous",
			ThreadOwnership: []backup.ThreadOwnership{
				{ThreadID: "thr_unowned"},
				{ThreadID: "thr_other", OwnerUserID: "usr_someone_else"},
			},
		}},
		Dumper:  &fakeDumper{contents: []byte("dump")},
		Objects: &assets.FakeStore{},
	}
	manifest, err := runner.Run(context.Background(), backup.Options{ProposedOwnerEmail: "owner@example.com", RunID: "owner-ambiguous", OutputDir: t.TempDir(), SourceBucket: "primary", BackupBucket: "recovery", BackupPrefix: "run/owner-ambiguous"})
	if !errors.Is(err, backup.ErrPreflightNotReady) || manifest.Ready || len(manifest.OwnerBackfill.AmbiguousThreadIDs) != 1 || manifest.OwnerBackfill.AmbiguousThreadIDs[0] != "thr_other" || !manifestHasIssue(manifest, "owner_backfill_ambiguous", "") {
		t.Fatalf("ambiguous owner manifest=%#v err=%v", manifest, err)
	}
}

func TestRunnerRejectsBackupPrefixInsideSourceInventory(t *testing.T) {
	_, err := (backup.Runner{}).Run(context.Background(), backup.Options{
		ProposedOwnerEmail: "owner@example.com",
		RunID:              "bad-prefix",
		OutputDir:          t.TempDir(),
		SourceBucket:       "primary",
		SourcePrefix:       "agentbox",
		BackupBucket:       "primary",
		BackupPrefix:       "agentbox/recovery",
	})
	if err == nil || !strings.Contains(err.Error(), "outside source inventory prefix") {
		t.Fatalf("error = %v", err)
	}
}

type fakeSnapshotSource struct {
	data       backup.SnapshotData
	err        error
	closeCount int
}

func (s *fakeSnapshotSource) OpenContentSnapshot(context.Context) (backup.ContentSnapshot, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &fakeSnapshot{data: s.data, onClose: func() { s.closeCount++ }}, nil
}

type fakeSnapshot struct {
	data    backup.SnapshotData
	onClose func()
	closed  bool
}

func (s *fakeSnapshot) Data() backup.SnapshotData {
	return s.data
}

func (s *fakeSnapshot) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.onClose != nil {
		s.onClose()
	}
	return nil
}

type fakeDumper struct {
	contents []byte
	err      error
	calls    int
}

func (d *fakeDumper) Dump(_ context.Context, _ string, destinationPath string) error {
	d.calls++
	if d.err != nil {
		return d.err
	}
	return os.WriteFile(destinationPath, d.contents, 0o600)
}

func manifestHasIssue(manifest backup.Manifest, code string, storageKey string) bool {
	for _, issue := range manifest.Issues {
		if issue.Code == code && (storageKey == "" || issue.StorageKey == storageKey) {
			return true
		}
	}
	return false
}
