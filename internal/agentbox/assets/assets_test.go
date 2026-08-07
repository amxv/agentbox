package assets

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"agentbox/internal/agentbox/backup"
	"agentbox/internal/agentbox/config"
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
	fileName := " report.txt "
	mimeType := " text/plain "
	file, err := NormalizeChatGPTFileInput(ChatGPTFileInput{
		DownloadURL: " https://files.openai.example/download/token ",
		FileID:      " file_abc123 ",
		FileName:    &fileName,
		MimeType:    &mimeType,
	})
	if err != nil {
		t.Fatal(err)
	}
	if file.DownloadURL != "https://files.openai.example/download/token" || file.FileID != "file_abc123" || file.FileName == nil || *file.FileName != "report.txt" || file.MimeType == nil || *file.MimeType != "text/plain" {
		t.Fatalf("unexpected normalized file: %#v", file)
	}
	for _, input := range []ChatGPTFileInput{
		{DownloadURL: "https://files.openai.example/download/token"},
		{DownloadURL: "file_abc123", FileID: "file_abc123"},
		{DownloadURL: "sandbox:/mnt/data/report.txt", FileID: "file_abc123"},
		{DownloadURL: "/mnt/data/report.txt", FileID: "file_abc123"},
		{DownloadURL: "https://user:secret@files.openai.example/report", FileID: "file_abc123"},
	} {
		if _, err := NormalizeChatGPTFileInput(input); err == nil {
			t.Fatalf("expected invalid structured input error for %#v", input)
		}
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
	if !strings.HasPrefix(asset.StorageKey, "agentbox/final/sha256/") || !strings.Contains(asset.StorageKey, "/usr_1/thr_1/message/") || asset.ContentSHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
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

	read, err := store.ReadAssetObjectRange(context.Background(), ReadAssetRangeParams{
		StorageKey:   asset.StorageKey,
		OffsetBytes:  1,
		MaxBytes:     3,
		ExpectedETag: asset.ContentSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(read) != "ell" {
		t.Fatalf("range read = %q", read)
	}
	if len(store.ReadCalls) != 1 || store.ReadCalls[0].OffsetBytes != 1 || store.ReadCalls[0].MaxBytes != 3 {
		t.Fatalf("range calls = %#v", store.ReadCalls)
	}
}

func TestR2RangeReadUsesDirectBoundedGetAndIfMatch(t *testing.T) {
	var gotRange string
	var gotIfMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotRange = request.Header.Get("Range")
		gotIfMatch = request.Header.Get("If-Match")
		if request.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", request.Method)
		}
		w.Header().Set("Content-Range", "bytes 6-10/12")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "world")
	}))
	defer server.Close()

	store := &R2Store{
		cfg:    config.Config{R2Bucket: "agentbox-assets"},
		client: testS3Client(server),
	}
	contents, err := store.ReadAssetObjectRange(t.Context(), ReadAssetRangeParams{
		StorageKey:   "agentbox/final/test.txt",
		OffsetBytes:  6,
		MaxBytes:     5,
		ExpectedETag: `"etag-original"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "world" || gotRange != "bytes=6-10" || gotIfMatch != `"etag-original"` {
		t.Fatalf("contents=%q range=%q if-match=%q", contents, gotRange, gotIfMatch)
	}
}

func TestR2PresignedUploadBindsActualLengthTypeAndSHA256AtStorageBoundary(t *testing.T) {
	store, err := NewR2Store(t.Context(), config.Config{
		R2AccountID:       "example-account",
		R2AccessKeyID:     "test-access-key",
		R2SecretAccessKey: "test-secret-key",
		R2Bucket:          "private-assets",
		MaxFileSizeBytes:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	mimeType := "application/octet-stream"
	digest := strings.Repeat("a", 64)
	upload, err := store.CreatePresignedAssetUploadURL(t.Context(), PresignedUploadParams{
		UserID: "usr_boundary", ThreadID: "thr_boundary", UploadID: "upl_boundary",
		FileName: "payload.bin", MimeType: &mimeType, SizeBytes: 17, SHA256: digest, ExpiresInSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(upload.UploadURL)
	if err != nil {
		t.Fatal(err)
	}
	signedHeaders := ";" + parsed.Query().Get("X-Amz-SignedHeaders") + ";"
	for _, required := range []string{";content-length;", ";content-type;", ";host;", ";x-amz-meta-agentbox-sha256;"} {
		if !strings.Contains(signedHeaders, required) {
			t.Fatalf("signed headers %q omit %q", signedHeaders, required)
		}
	}
	if parsed.Query().Get("X-Amz-Checksum-Sha256") == "" || parsed.Query().Get("x-id") != "PutObject" {
		t.Fatalf("presigned checksum/action query=%q", parsed.RawQuery)
	}
	if upload.RequiredHeaders["content-type"] != mimeType || upload.RequiredHeaders["x-amz-meta-agentbox-sha256"] != digest || upload.SizeBytes != 17 || upload.SHA256 != digest {
		t.Fatalf("required upload contract=%#v", upload)
	}
	if !strings.HasPrefix(upload.StorageKey, "agentbox/staging/usr_boundary/thr_boundary/upl_boundary/") || strings.Contains(upload.StorageKey, "/final/") {
		t.Fatalf("presigned upload used a canonical key: %q", upload.StorageKey)
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
