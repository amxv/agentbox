package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

func TestHealth(t *testing.T) {
	svc := service.New(&db.MemoryRepository{}, &assets.FakeStore{})
	server := NewServer(config.Config{}, svc)

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true || payload["service"] != "agentbox" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestThreadRoutesAndMultipartAsset(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	server := NewServer(config.Config{}, svc)

	create := httptest.NewRecorder()
	server.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(`{"title":"Go API"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		Thread struct {
			ID        string `json:"id"`
			CreatedBy string `json:"created_by"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Thread.ID == "" || created.Thread.CreatedBy != "local-dev" {
		t.Fatalf("created = %#v", created)
	}

	jsonPost := httptest.NewRecorder()
	server.ServeHTTP(jsonPost, httptest.NewRequest(
		http.MethodPost,
		"/api/threads/"+created.Thread.ID+"/messages",
		strings.NewReader(`{"body":"| A | B |\n| --- | --- |\n| 1 | 2 |"}`),
	))
	if jsonPost.Code != http.StatusCreated {
		t.Fatalf("json post status = %d body=%s", jsonPost.Code, jsonPost.Body.String())
	}
	var jsonPosted struct {
		Message struct {
			BodyContentType *string `json:"body_content_type"`
		} `json:"message"`
	}
	if err := json.Unmarshal(jsonPost.Body.Bytes(), &jsonPosted); err != nil {
		t.Fatal(err)
	}
	if jsonPosted.Message.BodyContentType == nil || *jsonPosted.Message.BodyContentType != "text/markdown" {
		t.Fatalf("json message content type = %#v", jsonPosted.Message.BodyContentType)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("body", "hello with asset"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("asset", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("asset body")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	post := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+created.Thread.ID+"/messages", &body)
	req.Header.Set("content-type", writer.FormDataContentType())
	server.ServeHTTP(post, req)
	if post.Code != http.StatusCreated {
		t.Fatalf("post status = %d body=%s", post.Code, post.Body.String())
	}
	var posted struct {
		Message struct {
			Body            string  `json:"body"`
			BodyContentType *string `json:"body_content_type"`
			Assets          []struct {
				ID        string `json:"id"`
				FileName  string `json:"file_name"`
				SizeBytes int64  `json:"size_bytes"`
			} `json:"assets"`
		} `json:"message"`
	}
	if err := json.Unmarshal(post.Body.Bytes(), &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Message.Body != "hello with asset" || len(posted.Message.Assets) != 1 {
		t.Fatalf("posted = %#v", posted)
	}
	if posted.Message.BodyContentType == nil || *posted.Message.BodyContentType != "text/plain" {
		t.Fatalf("multipart message content type = %#v", posted.Message.BodyContentType)
	}
	if posted.Message.Assets[0].FileName != "hello.txt" || posted.Message.Assets[0].SizeBytes != int64(len("asset body")) {
		t.Fatalf("asset = %#v", posted.Message.Assets[0])
	}

	download := httptest.NewRecorder()
	server.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/api/assets/"+posted.Message.Assets[0].ID+"/download-url?expires_in=9999", nil))
	if download.Code != http.StatusOK {
		t.Fatalf("download status = %d body=%s", download.Code, download.Body.String())
	}
	var signed struct {
		AssetID     string `json:"asset_id"`
		ExpiresIn   int    `json:"expires_in"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.Unmarshal(download.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	if signed.AssetID != posted.Message.Assets[0].ID || signed.ExpiresIn != 3600 || signed.DownloadURL == "" {
		t.Fatalf("signed = %#v", signed)
	}
}

func TestViewerRoutesRequireAdminAndAddPreviewURLs(t *testing.T) {
	imageType := "image/png"
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	thread, err := svc.CreateThread(t.Context(), actor(), "Viewer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PostMessage(t.Context(), thread.ID, "author", "body", nil, &types.NewAsset{
		StorageKey: "agentbox/thread/message/image.png",
		FileName:   "image.png",
		MimeType:   &imageType,
		SizeBytes:  10,
	}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{AdminKey: "adm", Environment: "production"}, svc)

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/viewer/threads", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/viewer/threads/"+thread.ID, nil)
	req.Header.Set("x-agentbox-admin-key", "adm")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("viewer status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Thread struct {
			Messages []struct {
				Assets []struct {
					DownloadURL string  `json:"download_url"`
					PreviewURL  *string `json:"preview_url"`
				} `json:"assets"`
			} `json:"messages"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	asset := payload.Thread.Messages[0].Assets[0]
	if asset.DownloadURL == "" || asset.PreviewURL == nil || *asset.PreviewURL != asset.DownloadURL {
		t.Fatalf("viewer asset = %#v", asset)
	}
}

func TestMCPOriginValidation(t *testing.T) {
	svc := service.New(&db.MemoryRepository{}, &assets.FakeStore{})
	server := NewServer(config.Config{AllowedOrigins: []string{"https://allowed.test"}}, svc)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(`{}`))
	req.Header.Set("origin", "https://blocked.test")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func actor() types.Actor {
	return types.Actor{Name: "tester", KeyName: "test"}
}
