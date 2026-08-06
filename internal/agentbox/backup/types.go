package backup

import (
	"context"
	"errors"
	"time"
)

var ErrObjectNotFound = errors.New("object not found")
var ErrPreflightNotReady = errors.New("backup preflight is not ready")

type TableCounts struct {
	Threads        int64 `json:"threads"`
	Messages       int64 `json:"messages"`
	Assets         int64 `json:"assets"`
	PendingUploads int64 `json:"pending_uploads"`
}

type OrphanCounts struct {
	MessagesWithoutThread       int64 `json:"messages_without_thread"`
	AssetsWithoutMessage        int64 `json:"assets_without_message"`
	PendingUploadsWithoutThread int64 `json:"pending_uploads_without_thread"`
}

func (c OrphanCounts) Total() int64 {
	return c.MessagesWithoutThread + c.AssetsWithoutMessage + c.PendingUploadsWithoutThread
}

type ObjectReference struct {
	Kind          string `json:"kind"`
	RecordID      string `json:"record_id"`
	StorageKey    string `json:"storage_key"`
	SizeBytes     int64  `json:"size_bytes"`
	MissingBlocks bool   `json:"missing_blocks_readiness"`
}

type ThreadOwnership struct {
	ThreadID    string
	OwnerUserID string
}

type SnapshotData struct {
	SnapshotID      string
	Counts          TableCounts
	Orphans         OrphanCounts
	References      []ObjectReference
	ThreadOwnership []ThreadOwnership
}

type ContentSnapshot interface {
	Data() SnapshotData
	Close(ctx context.Context) error
}

type SnapshotSource interface {
	OpenContentSnapshot(ctx context.Context) (ContentSnapshot, error)
}

type DatabaseDumper interface {
	Dump(ctx context.Context, snapshotID string, destinationPath string) error
}

type ObjectMetadata struct {
	Bucket         string            `json:"bucket"`
	Key            string            `json:"key"`
	SizeBytes      int64             `json:"size_bytes"`
	ETag           string            `json:"etag,omitempty"`
	ContentType    *string           `json:"content_type,omitempty"`
	ChecksumSHA256 string            `json:"checksum_sha256,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	LastModified   *time.Time        `json:"last_modified,omitempty"`
}

type CopyObjectRequest struct {
	SourceBucket      string
	SourceKey         string
	DestinationBucket string
	DestinationKey    string
	ExpectedETag      string
}

type ObjectStore interface {
	HeadObject(ctx context.Context, bucket string, key string) (ObjectMetadata, error)
	ListObjects(ctx context.Context, bucket string, prefix string) ([]ObjectMetadata, error)
	CopyObject(ctx context.Context, request CopyObjectRequest) (ObjectMetadata, error)
}

type Issue struct {
	Severity   string `json:"severity"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	StorageKey string `json:"storage_key,omitempty"`
	RecordID   string `json:"record_id,omitempty"`
}

type DatabaseManifest struct {
	DumpFile            string       `json:"dump_file"`
	DumpFormat          string       `json:"dump_format"`
	RestoreCommand      string       `json:"restore_command"`
	DumpSizeBytes       int64        `json:"dump_size_bytes"`
	DumpSHA256          string       `json:"dump_sha256,omitempty"`
	SnapshotExported    bool         `json:"snapshot_exported"`
	Counts              TableCounts  `json:"counts"`
	Orphans             OrphanCounts `json:"orphans"`
	ObjectReferenceRows int          `json:"object_reference_rows"`
}

type OwnerBackfillManifest struct {
	ProposedOwnerEmail    string   `json:"proposed_owner_email"`
	ProposedOwnerID       string   `json:"proposed_owner_id"`
	ThreadCount           int      `json:"thread_count"`
	ThreadIDs             []string `json:"thread_ids"`
	ThreadIDsSHA256       string   `json:"thread_ids_sha256"`
	AlreadyAssignedCount  int      `json:"already_assigned_count"`
	AmbiguousThreadIDs    []string `json:"ambiguous_thread_ids"`
	UnassignableThreadIDs []string `json:"unassignable_thread_ids"`
}

type ObjectBackup struct {
	StorageKey string            `json:"storage_key"`
	References []ObjectReference `json:"references"`
	Source     *ObjectMetadata   `json:"source,omitempty"`
	BackupKey  string            `json:"backup_key"`
	Backup     *ObjectMetadata   `json:"backup,omitempty"`
	Status     string            `json:"status"`
	Error      string            `json:"error,omitempty"`
}

type ObjectManifest struct {
	SourceBucket               string           `json:"source_bucket"`
	SourcePrefix               string           `json:"source_prefix"`
	BackupBucket               string           `json:"backup_bucket"`
	BackupPrefix               string           `json:"backup_prefix"`
	InventoryCount             int              `json:"inventory_count"`
	ReferenceRowCount          int              `json:"reference_row_count"`
	ReferencedObjectCount      int              `json:"referenced_object_count"`
	PresentObjectCount         int              `json:"present_object_count"`
	MissingObjectCount         int              `json:"missing_object_count"`
	UnmaterializedPendingCount int              `json:"unmaterialized_pending_count"`
	CopiedObjectCount          int              `json:"copied_object_count"`
	AlreadyBackedUpCount       int              `json:"already_backed_up_count"`
	UnreferencedCount          int              `json:"unreferenced_count"`
	Objects                    []ObjectBackup   `json:"objects"`
	Unreferenced               []ObjectMetadata `json:"unreferenced"`
}

type Manifest struct {
	SchemaVersion int                   `json:"schema_version"`
	RunID         string                `json:"run_id"`
	GeneratedAt   time.Time             `json:"generated_at"`
	Ready         bool                  `json:"ready"`
	RunDirectory  string                `json:"run_directory"`
	Database      DatabaseManifest      `json:"database"`
	OwnerBackfill OwnerBackfillManifest `json:"owner_backfill"`
	Objects       ObjectManifest        `json:"objects"`
	Issues        []Issue               `json:"issues"`
}

type Options struct {
	RunID              string
	OutputDir          string
	SourceBucket       string
	SourcePrefix       string
	BackupBucket       string
	BackupPrefix       string
	ProposedOwnerEmail string
	Now                func() time.Time
}
