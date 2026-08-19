package assets

import (
	"errors"
	"time"
)

// ErrObjectNotFound identifies a missing object in the configured asset store.
var ErrObjectNotFound = errors.New("object not found")

// ObjectMetadata is the storage metadata Agentbox needs to verify and finalize
// attachment objects without exposing provider-specific SDK types.
type ObjectMetadata struct {
	Bucket         string
	Key            string
	SizeBytes      int64
	ETag           string
	ContentType    *string
	ChecksumSHA256 string
	Metadata       map[string]string
	LastModified   *time.Time
}

// copyObjectRequest describes an internal object copy used when a staged upload
// is finalized into its durable attachment key.
type copyObjectRequest struct {
	SourceBucket      string
	SourceKey         string
	DestinationBucket string
	DestinationKey    string
	ExpectedETag      string
}
