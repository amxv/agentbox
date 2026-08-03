package assets

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"agentbox/internal/agentbox/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type stubRemoteFileFetcher struct {
	contents []byte
	err      error
	url      string
	maxBytes int64
}

func (f *stubRemoteFileFetcher) Fetch(_ context.Context, downloadURL string, maxBytes int64) ([]byte, error) {
	f.url = downloadURL
	f.maxBytes = maxBytes
	if f.err != nil {
		return nil, f.err
	}
	return append([]byte(nil), f.contents...), nil
}

func testS3Client(server *httptest.Server) *s3.Client {
	awsConfig := aws.Config{
		Region:      "auto",
		Credentials: credentials.NewStaticCredentialsProvider("test-access", "test-secret", ""),
		HTTPClient:  server.Client(),
	}
	return s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(server.URL)
		options.UsePathStyle = true
	})
}

func TestR2StoreChatGPTFilePersistsFetchedBytesAndMetadata(t *testing.T) {
	var mutex sync.Mutex
	var method string
	var requestPath string
	var contentType string
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		contents, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		mutex.Lock()
		method = request.Method
		requestPath = request.URL.Path
		contentType = request.Header.Get("content-type")
		body = contents
		mutex.Unlock()
		w.Header().Set("ETag", `"test-etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fetcher := &stubRemoteFileFetcher{contents: []byte("# ChatGPT handoff\n")}
	store := &R2Store{
		cfg: config.Config{
			R2Bucket:         "agentbox-assets",
			MaxFileSizeBytes: 1024,
		},
		client:      testS3Client(server),
		fileFetcher: fetcher,
	}
	fileName := "handoff plan.md"
	mimeType := "text/markdown"
	asset, err := store.UploadChatGPTFile(t.Context(), "usr_chatgpt", "thr_chatgpt", ChatGPTFileInput{
		DownloadURL: "https://files.openai.example/download/signed-token",
		FileID:      "file_handoff",
		FileName:    &fileName,
		MimeType:    &mimeType,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.url != "https://files.openai.example/download/signed-token" || fetcher.maxBytes != 1024 {
		t.Fatalf("fetch call url=%q max=%d", fetcher.url, fetcher.maxBytes)
	}
	if asset.FileName != "handoff-plan.md" || asset.MimeType == nil || *asset.MimeType != mimeType || asset.SizeBytes != int64(len(fetcher.contents)) {
		t.Fatalf("asset = %#v", asset)
	}
	if !strings.HasPrefix(asset.StorageKey, "agentbox/usr_chatgpt/thr_chatgpt/file_handoff/") || !strings.HasSuffix(asset.StorageKey, "-handoff-plan.md") {
		t.Fatalf("storage key = %q", asset.StorageKey)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if method != http.MethodPut || !strings.HasSuffix(requestPath, "/"+asset.StorageKey) || contentType != mimeType || string(body) != string(fetcher.contents) {
		t.Fatalf("R2 request method=%q path=%q content-type=%q body=%q", method, requestPath, contentType, body)
	}
}

func TestR2StoreChatGPTFetchFailureWritesNoObject(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	fetcher := &stubRemoteFileFetcher{err: errors.New("blocked unsafe destination")}
	store := &R2Store{
		cfg: config.Config{
			R2Bucket:         "agentbox-assets",
			MaxFileSizeBytes: 1024,
		},
		client:      testS3Client(server),
		fileFetcher: fetcher,
	}
	_, err := store.UploadChatGPTFile(t.Context(), "usr_chatgpt", "thr_chatgpt", ChatGPTFileInput{
		DownloadURL: "https://files.openai.example/download/signed-token",
		FileID:      "file_handoff",
	})
	if err == nil || !strings.Contains(err.Error(), "blocked unsafe destination") {
		t.Fatalf("fetch error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("fetch failure issued %d R2 requests", requests)
	}
}
