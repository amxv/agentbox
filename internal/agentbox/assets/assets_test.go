package assets

import (
	"context"
	"strings"
	"testing"
)

func TestFilenameMimeStorageAndPublicURLHelpers(t *testing.T) {
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
	key := MakeStorageKey("ten_1", "thr_1", "message", "report.txt")
	if !strings.HasPrefix(key, "agentbox/ten_1/thr_1/message/") || !strings.HasSuffix(key, "-report.txt") {
		t.Fatalf("storage key = %q", key)
	}
	publicURL := PublicURLForKey("https://cdn.example/", key)
	if publicURL == nil || *publicURL != "https://cdn.example/"+key {
		t.Fatalf("public URL = %#v", publicURL)
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
	store := &FakeStore{MaxFileSizeBytes: 10, PublicBaseURL: "https://cdn.example"}
	mimeType := "text/plain"
	asset, err := store.UploadAssetBytes(context.Background(), UploadBytesParams{
		TenantID: "ten_1",
		ThreadID: "thr_1",
		Bytes:    []byte("hello"),
		FileName: "report one.txt",
		MimeType: &mimeType,
	})
	if err != nil {
		t.Fatal(err)
	}
	if asset.FileName != "report-one.txt" || asset.SizeBytes != 5 || asset.PublicURL == nil {
		t.Fatalf("unexpected asset: %#v", asset)
	}
	if !strings.HasPrefix(asset.StorageKey, "agentbox/ten_1/thr_1/message/") {
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
