package assets

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentbox/internal/agentbox/backup"
)

func TestFilenameMimeAndStorageHelpers(t *testing.T) {
	if got := SanitizeFilename(" report one!.txt "); got != "report-one-.txt" {
		t.Fatalf("SanitizeFilename = %q", got)
	}
	if got := SanitizeFilename("###"); got != "file.bin" {
		t.Fatalf("fallback filename = %q", got)
	}
	mimeType := InferMimeType("report.txt", nil)
	if mimeType == nil || *mimeType != "text/plain; charset=utf-8" {
		t.Fatalf("mime type = %#v", mimeType)
	}
	key := MakeStorageKey("usr_1", "thr_1", "message", "report.txt")
	if !strings.HasPrefix(key, "agentbox/usr_1/thr_1/message/") || !strings.HasSuffix(key, "-report.txt") {
		t.Fatalf("storage key = %q", key)
	}
}

func TestNormalizeChatGPTFileInput(t *testing.T) {
	file, err := NormalizeChatGPTFileInput(ChatGPTFileInput{RawString: "https://example.com/files/report.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if file.DownloadURL == "" || file.FileName == nil || *file.FileName != "report.txt" {
		t.Fatalf("unexpected normalized file: %#v", file)
	}
	if _, err := NormalizeChatGPTFileInput(ChatGPTFileInput{RawString: "file_abc123"}); err == nil {
		t.Fatal("expected plain file ID string error")
	}
}

func TestFakeStoreUploadAndSignedURL(t *testing.T) {
	store := &FakeStore{MaxFileSizeBytes: 10}
	mimeType := "text/plain"
	asset, err := store.UploadAssetBytes(context.Background(), UploadBytesParams{
		UserID:   "usr_1",
		ThreadID: "thr_1",
		Bytes:    []byte("hello"),
		FileName: "report one.txt",
		MimeType: &mimeType,
	})
	if err != nil {
		t.Fatal(err)
	}
	if asset.FileName != "report-one.txt" || asset.SizeBytes != 5 {
		t.Fatalf("unexpected asset: %#v", asset)
	}
	if !strings.HasPrefix(asset.StorageKey, "agentbox/usr_1/thr_1/message/") {
		t.Fatalf("storage key = %q", asset.StorageKey)
	}
	url, err := store.CreateSignedAssetDownloadURL(context.Background(), SignedURLParams{
		StorageKey:       asset.StorageKey,
		FileName:         asset.FileName,
		MimeType:         asset.MimeType,
		ExpiresInSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "X-Amz-Expires=60") {
		t.Fatalf("signed URL = %q", url)
	}
}

func TestFakeStoreObjectMaintenance(t *testing.T) {
	store := &FakeStore{}
	store.PutObject("primary", "agentbox/a.bin", 3, `"etag-a"`)
	store.PutObject("primary", "agentbox/b.bin", 5, "etag-b")
	store.PutObject("other", "agentbox/c.bin", 7, "etag-c")

	object, err := store.HeadObject(context.Background(), "primary", "agentbox/a.bin")
	if err != nil {
		t.Fatal(err)
	}
	if object.SizeBytes != 3 || object.ETag != "etag-a" {
		t.Fatalf("unexpected object metadata: %#v", object)
	}

	objects, err := store.ListObjects(context.Background(), "primary", "agentbox/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 || objects[0].Key != "agentbox/a.bin" || objects[1].Key != "agentbox/b.bin" {
		t.Fatalf("unexpected object inventory: %#v", objects)
	}

	copied, err := store.CopyObject(context.Background(), backup.CopyObjectRequest{
		SourceBucket:      "primary",
		SourceKey:         "agentbox/a.bin",
		DestinationBucket: "recovery",
		DestinationKey:    "run/agentbox/a.bin",
		ExpectedETag:      `"etag-a"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if copied.Bucket != "recovery" || copied.Key != "run/agentbox/a.bin" || copied.SizeBytes != 3 || copied.ETag != "etag-a" {
		t.Fatalf("unexpected copied metadata: %#v", copied)
	}
	if len(store.CopyCalls) != 1 {
		t.Fatalf("copy calls = %d, want 1", len(store.CopyCalls))
	}

	_, err = store.HeadObject(context.Background(), "primary", "agentbox/missing.bin")
	if !errors.Is(err, backup.ErrObjectNotFound) {
		t.Fatalf("HeadObject error = %v, want ErrObjectNotFound", err)
	}
	_, err = store.CopyObject(context.Background(), backup.CopyObjectRequest{
		SourceBucket:      "primary",
		SourceKey:         "agentbox/a.bin",
		DestinationBucket: "recovery",
		DestinationKey:    "run/changed.bin",
		ExpectedETag:      "different",
	})
	if err == nil || !strings.Contains(err.Error(), "ETag changed") {
		t.Fatalf("CopyObject error = %v, want conditional-copy failure", err)
	}
}
