package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agentbox/internal/agentbox/identity"
)

const manifestSchemaVersion = 2

type Runner struct {
	Snapshots SnapshotSource
	Dumper    DatabaseDumper
	Objects   ObjectStore
}

func (r Runner) Run(ctx context.Context, options Options) (Manifest, error) {
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	generatedAt := now().UTC()
	if strings.TrimSpace(options.RunID) == "" {
		options.RunID = generatedAt.Format("20060102T150405Z")
	}
	if strings.TrimSpace(options.OutputDir) == "" {
		return Manifest{}, fmt.Errorf("output directory is required")
	}
	if strings.TrimSpace(options.SourceBucket) == "" {
		return Manifest{}, fmt.Errorf("source bucket is required")
	}
	options.ProposedOwnerEmail = identity.NormalizeEmail(options.ProposedOwnerEmail)
	if options.ProposedOwnerEmail == "" {
		return Manifest{}, fmt.Errorf("proposed owner email is required")
	}
	if strings.TrimSpace(options.BackupBucket) == "" {
		options.BackupBucket = options.SourceBucket
	}
	options.BackupPrefix = strings.Trim(options.BackupPrefix, "/")
	if options.BackupPrefix == "" {
		return Manifest{}, fmt.Errorf("backup prefix is required")
	}
	if options.SourceBucket == options.BackupBucket && objectKeyHasPrefix(options.BackupPrefix, options.SourcePrefix) {
		return Manifest{}, fmt.Errorf("backup prefix %q must be outside source inventory prefix %q when using the same bucket", options.BackupPrefix, options.SourcePrefix)
	}

	runDirectory := filepath.Join(options.OutputDir, options.RunID)
	if err := os.MkdirAll(runDirectory, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("create preflight output directory: %w", err)
	}

	manifest := Manifest{
		SchemaVersion: manifestSchemaVersion,
		RunID:         options.RunID,
		GeneratedAt:   generatedAt,
		RunDirectory:  runDirectory,
		Database: DatabaseManifest{
			DumpFile:       "database.dump",
			DumpFormat:     "PostgreSQL custom",
			RestoreCommand: "pg_restore --clean --if-exists --no-owner --no-acl --dbname <recovery-database-url> database.dump",
		},
		OwnerBackfill: OwnerBackfillManifest{
			ProposedOwnerEmail:    options.ProposedOwnerEmail,
			ProposedOwnerID:       identity.ProposedOwnerID(options.ProposedOwnerEmail),
			ThreadIDs:             []string{},
			AmbiguousThreadIDs:    []string{},
			UnassignableThreadIDs: []string{},
		},
		Objects: ObjectManifest{
			SourceBucket: options.SourceBucket,
			SourcePrefix: options.SourcePrefix,
			BackupBucket: options.BackupBucket,
			BackupPrefix: options.BackupPrefix,
			Objects:      []ObjectBackup{},
			Unreferenced: []ObjectMetadata{},
		},
		Issues: []Issue{},
	}

	var snapshotData SnapshotData
	if r.Snapshots == nil {
		manifest.addError("database_snapshot_unavailable", "database snapshot source is not configured", "", "")
	} else {
		snapshot, err := r.Snapshots.OpenContentSnapshot(ctx)
		if err != nil {
			manifest.addError("database_snapshot_failed", err.Error(), "", "")
		} else {
			snapshotData = snapshot.Data()
			manifest.Database.SnapshotExported = snapshotData.SnapshotID != ""
			manifest.Database.Counts = snapshotData.Counts
			manifest.Database.Orphans = snapshotData.Orphans
			manifest.Database.ObjectReferenceRows = len(snapshotData.References)
			manifest.Objects.ReferenceRowCount = len(snapshotData.References)
			if snapshotData.Orphans.Total() > 0 {
				manifest.addError("database_orphan_rows", fmt.Sprintf("database contains %d orphaned content rows", snapshotData.Orphans.Total()), "", "")
			}
			populateOwnerBackfill(snapshotData.ThreadOwnership, &manifest)

			dumpPath := filepath.Join(runDirectory, manifest.Database.DumpFile)
			temporaryDumpPath := dumpPath + ".tmp"
			_ = os.Remove(temporaryDumpPath)
			if r.Dumper == nil {
				manifest.addError("database_dump_unavailable", "database dumper is not configured", "", "")
			} else if err := r.Dumper.Dump(ctx, snapshotData.SnapshotID, temporaryDumpPath); err != nil {
				manifest.addError("database_dump_failed", err.Error(), "", "")
				_ = os.Remove(temporaryDumpPath)
			} else if err := replaceFile(temporaryDumpPath, dumpPath); err != nil {
				manifest.addError("database_dump_finalize_failed", err.Error(), "", "")
			} else {
				size, checksum, err := fileMetadata(dumpPath)
				if err != nil {
					manifest.addError("database_dump_verify_failed", err.Error(), "", "")
				} else {
					manifest.Database.DumpSizeBytes = size
					manifest.Database.DumpSHA256 = checksum
				}
			}

			closeContext, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
			if err := snapshot.Close(closeContext); err != nil {
				manifest.addError("database_snapshot_close_failed", err.Error(), "", "")
			}
			cancelClose()
		}
	}

	r.backupObjects(ctx, options, snapshotData.References, &manifest)
	manifest.Ready = !manifest.hasErrors()
	if !manifest.Ready {
		return manifest, ErrPreflightNotReady
	}
	return manifest, nil
}

func (r Runner) backupObjects(ctx context.Context, options Options, references []ObjectReference, manifest *Manifest) {
	if r.Objects == nil {
		manifest.addError("object_store_unavailable", "object store is not configured", "", "")
		return
	}

	inventory, err := r.Objects.ListObjects(ctx, options.SourceBucket, options.SourcePrefix)
	if err != nil {
		manifest.addError("object_inventory_failed", err.Error(), "", "")
	} else {
		filtered := make([]ObjectMetadata, 0, len(inventory))
		for _, object := range inventory {
			if options.SourceBucket == options.BackupBucket && objectKeyHasPrefix(object.Key, options.BackupPrefix) {
				continue
			}
			filtered = append(filtered, object)
		}
		inventory = filtered
		manifest.Objects.InventoryCount = len(inventory)
	}

	referencesByKey := make(map[string][]ObjectReference)
	for _, reference := range references {
		referencesByKey[reference.StorageKey] = append(referencesByKey[reference.StorageKey], reference)
	}
	keys := make([]string, 0, len(referencesByKey))
	for key := range referencesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	manifest.Objects.ReferencedObjectCount = len(keys)

	for _, key := range keys {
		item := ObjectBackup{
			StorageKey: key,
			References: referencesByKey[key],
			BackupKey:  options.BackupPrefix + "/" + key,
			Status:     "pending",
		}
		if conflictingReferenceSizes(item.References) {
			item.Status = "reference_conflict"
			item.Error = "database rows referencing this object disagree on size"
			manifest.addError("object_reference_size_conflict", item.Error, key, item.References[0].RecordID)
			manifest.Objects.Objects = append(manifest.Objects.Objects, item)
			continue
		}

		source, err := r.Objects.HeadObject(ctx, options.SourceBucket, key)
		if errors.Is(err, ErrObjectNotFound) {
			if referencesBlockMissing(item.References) {
				item.Status = "missing"
				item.Error = "referenced object is missing from the source bucket"
				manifest.Objects.MissingObjectCount++
				manifest.addError("referenced_object_missing", item.Error, key, firstRecordID(item.References))
			} else {
				item.Status = "not_materialized"
				item.Error = "active pending-upload intent has no source object yet"
				manifest.Objects.UnmaterializedPendingCount++
				manifest.Issues = append(manifest.Issues, Issue{
					Severity:   "warning",
					Code:       "pending_upload_not_materialized",
					Message:    item.Error,
					StorageKey: key,
					RecordID:   firstRecordID(item.References),
				})
			}
			manifest.Objects.Objects = append(manifest.Objects.Objects, item)
			continue
		}

		if err != nil {
			item.Status = "head_failed"
			item.Error = err.Error()
			manifest.addError("referenced_object_head_failed", err.Error(), key, firstRecordID(item.References))
			manifest.Objects.Objects = append(manifest.Objects.Objects, item)
			continue
		}
		item.Source = &source
		manifest.Objects.PresentObjectCount++
		if source.SizeBytes != item.References[0].SizeBytes {
			item.Status = "source_size_mismatch"
			item.Error = fmt.Sprintf("database size %d does not match source object size %d", item.References[0].SizeBytes, source.SizeBytes)
			manifest.addError("referenced_object_size_mismatch", item.Error, key, firstRecordID(item.References))
			manifest.Objects.Objects = append(manifest.Objects.Objects, item)
			continue
		}

		existingBackup, err := r.Objects.HeadObject(ctx, options.BackupBucket, item.BackupKey)
		if err == nil && objectMetadataMatches(source, existingBackup) {
			item.Status = "already_backed_up"
			item.Backup = &existingBackup
			manifest.Objects.AlreadyBackedUpCount++
			manifest.Objects.Objects = append(manifest.Objects.Objects, item)
			continue
		}
		if err != nil && !errors.Is(err, ErrObjectNotFound) {
			item.Status = "backup_head_failed"
			item.Error = err.Error()
			manifest.addError("backup_object_head_failed", err.Error(), key, firstRecordID(item.References))
			manifest.Objects.Objects = append(manifest.Objects.Objects, item)
			continue
		}

		copied, err := r.Objects.CopyObject(ctx, CopyObjectRequest{
			SourceBucket:      options.SourceBucket,
			SourceKey:         key,
			DestinationBucket: options.BackupBucket,
			DestinationKey:    item.BackupKey,
			ExpectedETag:      source.ETag,
		})
		if err != nil {
			item.Status = "copy_failed"
			item.Error = err.Error()
			manifest.addError("backup_object_copy_failed", err.Error(), key, firstRecordID(item.References))
			manifest.Objects.Objects = append(manifest.Objects.Objects, item)
			continue
		}
		item.Backup = &copied
		if !objectMetadataMatches(source, copied) {
			item.Status = "backup_verify_failed"
			item.Error = "backup object metadata does not match the source object"
			manifest.addError("backup_object_verify_failed", item.Error, key, firstRecordID(item.References))
			manifest.Objects.Objects = append(manifest.Objects.Objects, item)
			continue
		}
		item.Status = "copied"
		manifest.Objects.CopiedObjectCount++
		manifest.Objects.Objects = append(manifest.Objects.Objects, item)
	}

	referencedKeys := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		referencedKeys[key] = struct{}{}
	}
	for _, object := range inventory {
		if _, ok := referencedKeys[object.Key]; ok {
			continue
		}
		manifest.Objects.Unreferenced = append(manifest.Objects.Unreferenced, object)
	}
	sort.Slice(manifest.Objects.Unreferenced, func(i, j int) bool {
		return manifest.Objects.Unreferenced[i].Key < manifest.Objects.Unreferenced[j].Key
	})
	manifest.Objects.UnreferencedCount = len(manifest.Objects.Unreferenced)
	if manifest.Objects.UnreferencedCount > 0 {
		manifest.Issues = append(manifest.Issues, Issue{
			Severity: "warning",
			Code:     "unreferenced_source_objects",
			Message:  fmt.Sprintf("source inventory contains %d objects not referenced by assets or pending uploads", manifest.Objects.UnreferencedCount),
		})
	}
}

func populateOwnerBackfill(ownership []ThreadOwnership, manifest *Manifest) {
	ownerID := manifest.OwnerBackfill.ProposedOwnerID
	threadIDs := make([]string, 0, len(ownership))
	for _, item := range ownership {
		threadID := strings.TrimSpace(item.ThreadID)
		if threadID == "" {
			manifest.OwnerBackfill.UnassignableThreadIDs = append(manifest.OwnerBackfill.UnassignableThreadIDs, item.ThreadID)
			continue
		}
		threadIDs = append(threadIDs, threadID)
		switch strings.TrimSpace(item.OwnerUserID) {
		case "":
		case ownerID:
			manifest.OwnerBackfill.AlreadyAssignedCount++
		default:
			manifest.OwnerBackfill.AmbiguousThreadIDs = append(manifest.OwnerBackfill.AmbiguousThreadIDs, threadID)
		}
	}
	sort.Strings(threadIDs)
	sort.Strings(manifest.OwnerBackfill.AmbiguousThreadIDs)
	sort.Strings(manifest.OwnerBackfill.UnassignableThreadIDs)
	manifest.OwnerBackfill.ThreadIDs = threadIDs
	manifest.OwnerBackfill.ThreadCount = len(threadIDs)
	digest := sha256.Sum256([]byte(strings.Join(threadIDs, "\n")))
	manifest.OwnerBackfill.ThreadIDsSHA256 = hex.EncodeToString(digest[:])
	if len(manifest.OwnerBackfill.AmbiguousThreadIDs) > 0 || len(manifest.OwnerBackfill.UnassignableThreadIDs) > 0 {
		manifest.addError(
			"owner_backfill_ambiguous",
			fmt.Sprintf("%d threads already have a different owner and %d thread IDs are unassignable", len(manifest.OwnerBackfill.AmbiguousThreadIDs), len(manifest.OwnerBackfill.UnassignableThreadIDs)),
			"",
			"",
		)
	}
}

func referencesBlockMissing(references []ObjectReference) bool {
	for _, reference := range references {
		if reference.MissingBlocks || reference.Kind != "pending_upload" {
			return true
		}
	}
	return false
}

func WriteManifest(path string, manifest Manifest) error {
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backup manifest: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	temporaryPath := path + ".tmp"
	if err := os.WriteFile(temporaryPath, contents, 0o600); err != nil {
		return fmt.Errorf("write temporary manifest: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("finalize manifest: %w", err)
	}
	return nil
}

func (m *Manifest) addError(code string, message string, storageKey string, recordID string) {
	m.Issues = append(m.Issues, Issue{
		Severity:   "error",
		Code:       code,
		Message:    message,
		StorageKey: storageKey,
		RecordID:   recordID,
	})
}

func (m Manifest) hasErrors() bool {
	for _, issue := range m.Issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}

func replaceFile(source string, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

func fileMetadata(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func objectKeyHasPrefix(key string, prefix string) bool {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return false
	}
	key = strings.TrimPrefix(key, "/")
	return key == prefix || strings.HasPrefix(key, prefix+"/")
}

func conflictingReferenceSizes(references []ObjectReference) bool {
	if len(references) < 2 {
		return false
	}
	size := references[0].SizeBytes
	for _, reference := range references[1:] {
		if reference.SizeBytes != size {
			return true
		}
	}
	return false
}

func firstRecordID(references []ObjectReference) string {
	if len(references) == 0 {
		return ""
	}
	return references[0].RecordID
}

func objectMetadataMatches(source ObjectMetadata, backup ObjectMetadata) bool {
	if source.SizeBytes != backup.SizeBytes {
		return false
	}
	if source.ETag != "" && backup.ETag != "" && source.ETag != backup.ETag {
		return false
	}
	return true
}
