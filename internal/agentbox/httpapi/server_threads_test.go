package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	authpkg "agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

func TestThreadRoutesAndMultipartAsset(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	if _, err := svc.CreateAPIKey(t.Context(), authContext("global", "local"), "local"); err != nil {
		t.Fatal(err)
	}
	repo.APIKeys[0].Key = "dev-key"
	repo.APIKeys[0].TokenHash = dbHashForTest("dev-key")
	server := NewServer(config.Config{}, svc)

	create := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(`{"title":"Go API"}`))
	createReq.Header.Set("authorization", "Bearer dev-key")
	server.ServeHTTP(create, createReq)
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
	if created.Thread.ID == "" || created.Thread.CreatedBy != "local" {
		t.Fatalf("created = %#v", created)
	}

	createWithMessage := httptest.NewRecorder()
	server.ServeHTTP(createWithMessage, httptest.NewRequest(http.MethodPost, "/api/threads?key=dev-key", strings.NewReader(`{"title":"Initial API","initial_message":"first body","body_content_type":"text/plain"}`)))
	if createWithMessage.Code != http.StatusCreated {
		t.Fatalf("create with message status = %d body=%s", createWithMessage.Code, createWithMessage.Body.String())
	}
	var initialCreated struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Message struct {
			ThreadID        string  `json:"thread_id"`
			Body            string  `json:"body"`
			BodyContentType *string `json:"body_content_type"`
		} `json:"message"`
	}
	if err := json.Unmarshal(createWithMessage.Body.Bytes(), &initialCreated); err != nil {
		t.Fatal(err)
	}
	if initialCreated.Message.ThreadID != initialCreated.Thread.ID || initialCreated.Message.Body != "first body" || initialCreated.Message.BodyContentType == nil || *initialCreated.Message.BodyContentType != "text/plain" {
		t.Fatalf("initial created = %#v", initialCreated)
	}

	jsonPost := httptest.NewRecorder()
	server.ServeHTTP(jsonPost, httptest.NewRequest(
		http.MethodPost,
		"/api/threads/"+created.Thread.ID+"/messages?key=dev-key",
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

	structuredHostFile := httptest.NewRecorder()
	server.ServeHTTP(structuredHostFile, httptest.NewRequest(
		http.MethodPost,
		"/api/threads/"+created.Thread.ID+"/messages?key=dev-key",
		strings.NewReader(`{"body":"must not fetch","file":{"download_url":"https://files.openai.example/download/token","file_id":"file_abc123"}}`),
	))
	if structuredHostFile.Code != http.StatusBadRequest || len(store.Uploads) != 0 {
		t.Fatalf("ordinary HTTP accepted host file: status=%d body=%s uploads=%#v", structuredHostFile.Code, structuredHostFile.Body.String(), store.Uploads)
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
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+created.Thread.ID+"/messages?key=dev-key", &body)
	req.Header.Set("content-type", writer.FormDataContentType())
	server.ServeHTTP(post, req)
	if post.Code != http.StatusCreated {
		t.Fatalf("post status = %d body=%s", post.Code, post.Body.String())
	}
	var posted struct {
		Message struct {
			ID              string  `json:"id"`
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

	getMessage := httptest.NewRecorder()
	server.ServeHTTP(getMessage, httptest.NewRequest(http.MethodGet, "/api/messages/"+posted.Message.ID+"?key=dev-key", nil))
	if getMessage.Code != http.StatusOK || !strings.Contains(getMessage.Body.String(), `"body":"hello with asset"`) || !strings.Contains(getMessage.Body.String(), `"file_name":"hello.txt"`) {
		t.Fatalf("message get status=%d body=%s", getMessage.Code, getMessage.Body.String())
	}

	missingMessage := httptest.NewRecorder()
	server.ServeHTTP(missingMessage, httptest.NewRequest(http.MethodGet, "/api/messages/msg_missing?key=dev-key", nil))
	if missingMessage.Code != http.StatusNotFound || !strings.Contains(missingMessage.Body.String(), `"code":"MESSAGE_NOT_FOUND"`) {
		t.Fatalf("missing message status=%d body=%s", missingMessage.Code, missingMessage.Body.String())
	}

	download := httptest.NewRecorder()
	server.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/api/assets/"+posted.Message.Assets[0].ID+"/download-url?key=dev-key&expires_in=9999", nil))
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

	search := httptest.NewRecorder()
	server.ServeHTTP(search, httptest.NewRequest(http.MethodGet, "/api/threads?key=dev-key&query=asset&limit=5", nil))
	if search.Code != http.StatusOK {
		t.Fatalf("search status = %d body=%s", search.Code, search.Body.String())
	}
	var searchPayload struct {
		Threads []struct {
			ID                 string   `json:"id"`
			MessageCount       int      `json:"message_count"`
			LastMessagePreview string   `json:"last_message_preview"`
			MatchedSnippets    []string `json:"matched_snippets"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(search.Body.Bytes(), &searchPayload); err != nil {
		t.Fatal(err)
	}
	if len(searchPayload.Threads) == 0 || searchPayload.Threads[0].MessageCount == 0 || searchPayload.Threads[0].LastMessagePreview == "" {
		t.Fatalf("search payload = %#v", searchPayload)
	}

	missingPost := httptest.NewRecorder()
	server.ServeHTTP(missingPost, httptest.NewRequest(
		http.MethodPost,
		"/api/threads/thr_missing/messages?key=dev-key",
		strings.NewReader(`{"body":"bad thread"}`),
	))
	if missingPost.Code != http.StatusNotFound {
		t.Fatalf("missing post status = %d body=%s", missingPost.Code, missingPost.Body.String())
	}
	var missingPayload struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(missingPost.Body.Bytes(), &missingPayload); err != nil {
		t.Fatal(err)
	}
	if missingPayload.Code != "THREAD_NOT_FOUND" || strings.Contains(missingPayload.Error, "SQLSTATE") || strings.Contains(missingPayload.Error, "constraint") {
		t.Fatalf("missing payload = %#v", missingPayload)
	}
}

func TestThreadViewRequiresNormalAuthenticationAndResolvesAssetsLazily(t *testing.T) {
	imageType := "image/png"
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	passwordHash, err := authpkg.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	repo.Users = append(repo.Users, testUser("global", "usr_viewer", "viewer@example.com", "Viewer Admin", "admin", passwordHash))
	viewerAuth := authContext("global", "tester")
	viewerAuth.UserID = "usr_viewer"
	viewerAuth.UserDisplayName = "Viewer Admin"
	thread, err := svc.CreateThread(t.Context(), viewerAuth, "Viewer")
	if err != nil {
		t.Fatal(err)
	}
	authorAuth := viewerAuth
	authorAuth.ActorName = "author"
	if _, err := repo.PostMessage(t.Context(), viewerAuth.UserID, thread.ID, authorAuth, "body", nil, []types.NewAsset{{
		StorageKey: "agentbox/thread/message/image.png",
		FileName:   "image.png",
		MimeType:   &imageType,
		SizeBytes:  10,
	}}); err != nil {
		t.Fatal(err)
	}
	store.PutAssetObject("agentbox/thread/message/image.png", 10, &imageType)
	store.HeadFailures = map[string]error{"assets\x00agentbox/thread/message/image.png": errors.New("thread detail must not inspect storage")}
	server := NewServer(config.Config{AdminKey: "adm", Environment: "production", SessionCookieName: config.DefaultSessionCookieName}, svc)

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/threads/"+thread.ID+"/view", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"viewer@example.com","password":"secret"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	sessionCookie := login.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/api/threads/"+thread.ID+"/view", nil)
	req.AddCookie(sessionCookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("viewer status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Thread struct {
			Messages []struct {
				Assets []struct {
					DownloadPath string `json:"download_path"`
					PreviewPath  string `json:"preview_path"`
				} `json:"assets"`
			} `json:"messages"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	asset := payload.Thread.Messages[0].Assets[0]
	if asset.DownloadPath == "" || asset.PreviewPath == "" || strings.Contains(recorder.Body.String(), "download_url") || strings.Contains(recorder.Body.String(), "preview_url") {
		t.Fatalf("viewer asset = %#v", asset)
	}
	delete(store.HeadFailures, "assets\x00agentbox/thread/message/image.png")
	previewRequest := httptest.NewRequest(http.MethodGet, asset.PreviewPath, nil)
	previewRequest.AddCookie(sessionCookie)
	previewResponse := httptest.NewRecorder()
	server.ServeHTTP(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusOK || !strings.Contains(previewResponse.Body.String(), `"available":true`) || !strings.Contains(previewResponse.Body.String(), `"preview_url"`) || !strings.Contains(previewResponse.Body.String(), "inline") {
		t.Fatalf("lazy preview status=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}
}

func TestHTTPUserPrivateThreadAndAssetIsolation(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	authA := authContext("global", "agent-a")
	authA.UserID = "usr_a"
	authA.UserDisplayName = "User A"
	authB := authContext("global", "agent-b")
	authB.UserID = "usr_b"
	authB.UserDisplayName = "User B"
	repo.Users = append(repo.Users,
		types.User{ID: authA.UserID, Email: "a@example.com", DisplayName: "User A"},
		types.User{ID: authB.UserID, Email: "b@example.com", DisplayName: "User B"},
	)
	keyA, err := svc.CreateAPIKey(t.Context(), authA, "shared")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := svc.CreateAPIKey(t.Context(), authB, "shared")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{}, svc)

	createA := httptest.NewRecorder()
	reqA := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(`{"title":"User A private needle"}`))
	reqA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(createA, reqA)
	if createA.Code != http.StatusCreated {
		t.Fatalf("createA status=%d body=%s", createA.Code, createA.Body.String())
	}
	var payloadA struct {
		Thread struct {
			ID          string `json:"id"`
			OwnerUserID string `json:"owner_user_id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(createA.Body.Bytes(), &payloadA); err != nil {
		t.Fatal(err)
	}
	if payloadA.Thread.OwnerUserID != authA.UserID || strings.Contains(createA.Body.String(), `tenant_id`) {
		t.Fatalf("createA leaked internal identity or used wrong owner: %s", createA.Body.String())
	}

	createB := httptest.NewRecorder()
	server.ServeHTTP(createB, httptest.NewRequest(http.MethodPost, "/api/threads?key="+keyB.Key, strings.NewReader(`{"title":"User B private needle"}`)))
	if createB.Code != http.StatusCreated {
		t.Fatalf("createB status=%d body=%s", createB.Code, createB.Body.String())
	}
	var payloadB struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(createB.Body.Bytes(), &payloadB); err != nil {
		t.Fatal(err)
	}

	listA := httptest.NewRecorder()
	reqListA := httptest.NewRequest(http.MethodGet, "/api/threads", nil)
	reqListA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(listA, reqListA)
	if listA.Code != http.StatusOK {
		t.Fatalf("listA status=%d body=%s", listA.Code, listA.Body.String())
	}
	if strings.Contains(listA.Body.String(), payloadB.Thread.ID) || !strings.Contains(listA.Body.String(), payloadA.Thread.ID) {
		t.Fatalf("listA leaked or missed thread: %s", listA.Body.String())
	}

	searchA := httptest.NewRecorder()
	reqSearchA := httptest.NewRequest(http.MethodGet, "/api/threads?query=needle", nil)
	reqSearchA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(searchA, reqSearchA)
	if searchA.Code != http.StatusOK || strings.Contains(searchA.Body.String(), payloadB.Thread.ID) || !strings.Contains(searchA.Body.String(), payloadA.Thread.ID) {
		t.Fatalf("searchA leaked or missed thread: status=%d body=%s", searchA.Code, searchA.Body.String())
	}

	getBWithA := httptest.NewRecorder()
	reqGetBWithA := httptest.NewRequest(http.MethodGet, "/api/threads/"+payloadB.Thread.ID, nil)
	reqGetBWithA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(getBWithA, reqGetBWithA)
	if getBWithA.Code != http.StatusNotFound {
		t.Fatalf("getBWithA status=%d body=%s", getBWithA.Code, getBWithA.Body.String())
	}

	postBWithA := httptest.NewRecorder()
	reqPostBWithA := httptest.NewRequest(http.MethodPost, "/api/threads/"+payloadB.Thread.ID+"/messages", strings.NewReader(`{"body":"blocked"}`))
	reqPostBWithA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(postBWithA, reqPostBWithA)
	if postBWithA.Code != http.StatusNotFound {
		t.Fatalf("postBWithA status=%d body=%s", postBWithA.Code, postBWithA.Body.String())
	}

	uploadBWithA := httptest.NewRecorder()
	reqUploadBWithA := httptest.NewRequest(http.MethodPost, "/api/threads/"+payloadB.Thread.ID+"/uploads", strings.NewReader(`{"files":[{"file_name":"blocked.txt","size_bytes":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`))
	reqUploadBWithA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(uploadBWithA, reqUploadBWithA)
	if uploadBWithA.Code != http.StatusNotFound {
		t.Fatalf("uploadBWithA status=%d body=%s", uploadBWithA.Code, uploadBWithA.Body.String())
	}

	intentA := httptest.NewRecorder()
	reqIntentA := httptest.NewRequest(http.MethodPost, "/api/threads/"+payloadA.Thread.ID+"/uploads", strings.NewReader(`{"files":[{"file_name":"user-a.txt","size_bytes":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`))
	reqIntentA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(intentA, reqIntentA)
	if intentA.Code != http.StatusCreated {
		t.Fatalf("intentA status=%d body=%s", intentA.Code, intentA.Body.String())
	}
	var intentAPayload struct {
		Uploads []struct {
			UploadID string `json:"upload_id"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(intentA.Body.Bytes(), &intentAPayload); err != nil {
		t.Fatal(err)
	}
	if len(intentAPayload.Uploads) != 1 || strings.Contains(intentA.Body.String(), "storage_key") || len(repo.Pending) == 0 || !strings.HasPrefix(repo.Pending[len(repo.Pending)-1].StorageKey, "agentbox/staging/"+authA.UserID+"/"+payloadA.Thread.ID+"/"+intentAPayload.Uploads[0].UploadID+"/") {
		t.Fatalf("intentAPayload = %#v", intentAPayload)
	}

	finalizeAWithB := httptest.NewRecorder()
	reqFinalizeAWithB := httptest.NewRequest(
		http.MethodPost,
		"/api/threads/"+payloadB.Thread.ID+"/messages",
		strings.NewReader(`{"body":"blocked","uploaded_assets":[{"upload_id":"`+intentAPayload.Uploads[0].UploadID+`"}]}`),
	)
	reqFinalizeAWithB.Header.Set("authorization", "Bearer "+keyB.Key)
	server.ServeHTTP(finalizeAWithB, reqFinalizeAWithB)
	if finalizeAWithB.Code != http.StatusBadRequest {
		t.Fatalf("finalizeAWithB status=%d body=%s", finalizeAWithB.Code, finalizeAWithB.Body.String())
	}

	messageB := types.Message{ID: "msg_b", ThreadID: payloadB.Thread.ID, Author: "agent-b", Body: "asset", CreatedAt: "2026-07-07T00:00:00.000Z"}
	repo.Messages = append(repo.Messages, messageB)
	repo.Assets = append(repo.Assets, types.Asset{
		ID:         "asset_b",
		MessageID:  messageB.ID,
		StorageKey: "agentbox/ten_b/thread/file.txt",
		FileName:   "file.txt",
		SizeBytes:  1,
		CreatedAt:  messageB.CreatedAt,
		CreatedBy:  "agent-b",
	})
	downloadBWithA := httptest.NewRecorder()
	reqDownloadBWithA := httptest.NewRequest(http.MethodGet, "/api/assets/asset_b/download-url", nil)
	reqDownloadBWithA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(downloadBWithA, reqDownloadBWithA)
	if downloadBWithA.Code != http.StatusNotFound {
		t.Fatalf("downloadBWithA status=%d body=%s", downloadBWithA.Code, downloadBWithA.Body.String())
	}

	messageA := types.Message{ID: "msg_a", ThreadID: payloadA.Thread.ID, Author: "agent-a", Body: "asset", CreatedAt: "2026-07-07T00:00:00.000Z"}
	repo.Messages = append(repo.Messages, messageA)
	repo.Assets = append(repo.Assets, types.Asset{
		ID:         "asset_legacy_a",
		MessageID:  messageA.ID,
		StorageKey: "agentbox/legacy-thread/message/legacy.txt",
		FileName:   "legacy.txt",
		SizeBytes:  1,
		CreatedAt:  messageA.CreatedAt,
		CreatedBy:  "agent-a",
	})
	store.PutAssetObject("agentbox/legacy-thread/message/legacy.txt", 1, nil)
	downloadLegacyA := httptest.NewRecorder()
	reqDownloadLegacyA := httptest.NewRequest(http.MethodGet, "/api/assets/asset_legacy_a/download-url", nil)
	reqDownloadLegacyA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(downloadLegacyA, reqDownloadLegacyA)
	if downloadLegacyA.Code != http.StatusOK {
		t.Fatalf("downloadLegacyA status=%d body=%s", downloadLegacyA.Code, downloadLegacyA.Body.String())
	}
	if !strings.Contains(downloadLegacyA.Body.String(), "agentbox%2Flegacy-thread%2Fmessage%2Flegacy.txt") && !strings.Contains(downloadLegacyA.Body.String(), "agentbox/legacy-thread/message/legacy.txt") {
		t.Fatalf("legacy download rewrote storage key: %s", downloadLegacyA.Body.String())
	}

	if err := svc.RevokeAPIKeyByID(t.Context(), authA, keyA.ID); err != nil {
		t.Fatal(err)
	}
	afterRevokeA := httptest.NewRecorder()
	reqAfterRevokeA := httptest.NewRequest(http.MethodGet, "/api/threads", nil)
	reqAfterRevokeA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(afterRevokeA, reqAfterRevokeA)
	if afterRevokeA.Code != http.StatusUnauthorized {
		t.Fatalf("afterRevokeA status=%d body=%s", afterRevokeA.Code, afterRevokeA.Body.String())
	}
	stillB := httptest.NewRecorder()
	server.ServeHTTP(stillB, httptest.NewRequest(http.MethodGet, "/api/threads?key="+keyB.Key, nil))
	if stillB.Code != http.StatusOK {
		t.Fatalf("stillB status=%d body=%s", stillB.Code, stillB.Body.String())
	}
}

func TestHTTPTeamSharedVisibilityIsImmediateAndParticipantMutable(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	authA := authContext("global", "agent-a")
	authA.UserID = "usr_share_a"
	authA.UserDisplayName = "Share A"
	authB := authContext("global", "agent-b")
	authB.UserID = "usr_share_b"
	authB.UserDisplayName = "Share B"
	authC := authContext("global", "agent-c")
	authC.UserID = "usr_share_c"
	authC.UserDisplayName = "Share C"
	repo.Users = append(repo.Users,
		types.User{ID: authA.UserID, Email: "a-share@example.com", DisplayName: authA.UserDisplayName},
		types.User{ID: authB.UserID, Email: "b-share@example.com", DisplayName: authB.UserDisplayName},
		types.User{ID: authC.UserID, Email: "c-share@example.com", DisplayName: authC.UserDisplayName},
	)
	team, err := repo.CreateTeam(t.Context(), "shared-team", "Shared Team")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddTeamMember(t.Context(), team.ID, authB.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddTeamMember(t.Context(), team.ID, authA.UserID); err != nil {
		t.Fatal(err)
	}
	keyA, err := svc.CreateAPIKey(t.Context(), authA, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := svc.CreateAPIKey(t.Context(), authB, "agent-b")
	if err != nil {
		t.Fatal(err)
	}
	keyC, err := svc.CreateAPIKey(t.Context(), authC, "agent-c")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.CreateThread(t.Context(), authA, "team-shared marker")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, svc)

	request := func(method string, path string, secret string, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("authorization", "Bearer "+secret)
		if body != "" {
			req.Header.Set("content-type", "application/json")
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}

	privateB := request(http.MethodGet, "/api/threads/"+thread.ID, keyB.Key, "")
	if privateB.Code != http.StatusNotFound {
		t.Fatalf("private thread visible before share: status=%d body=%s", privateB.Code, privateB.Body.String())
	}

	shareBody, _ := json.Marshal(map[string]any{"add_teams": []string{team.ID, team.ID}})
	shared := request(http.MethodPatch, "/api/threads/"+thread.ID+"/visibility", keyA.Key, string(shareBody))
	if shared.Code != http.StatusOK || !strings.Contains(shared.Body.String(), `"slug":"shared-team"`) {
		t.Fatalf("share status=%d body=%s", shared.Code, shared.Body.String())
	}
	retiredPut := request(http.MethodPut, "/api/threads/"+thread.ID+"/visibility", keyA.Key, `{"team_ids":[]}`)
	if retiredPut.Code != http.StatusMethodNotAllowed {
		t.Fatalf("retired visibility PUT remained live: status=%d body=%s", retiredPut.Code, retiredPut.Body.String())
	}

	listB := request(http.MethodGet, "/api/threads", keyB.Key, "")
	if listB.Code != http.StatusOK || !strings.Contains(listB.Body.String(), thread.ID) || !strings.Contains(listB.Body.String(), `"shared_with_me":true`) || !strings.Contains(listB.Body.String(), `"slug":"shared-team"`) {
		t.Fatalf("team member list status=%d body=%s", listB.Code, listB.Body.String())
	}
	sharedFilterB := request(http.MethodGet, "/api/threads?filter=shared", keyB.Key, "")
	if sharedFilterB.Code != http.StatusOK || !strings.Contains(sharedFilterB.Body.String(), thread.ID) {
		t.Fatalf("shared filter status=%d body=%s", sharedFilterB.Code, sharedFilterB.Body.String())
	}
	teamFilterB := request(http.MethodGet, "/api/threads?filter=team&team=shared-team", keyB.Key, "")
	if teamFilterB.Code != http.StatusOK || !strings.Contains(teamFilterB.Body.String(), thread.ID) {
		t.Fatalf("team filter status=%d body=%s", teamFilterB.Code, teamFilterB.Body.String())
	}
	privateFilterB := request(http.MethodGet, "/api/threads?filter=private", keyB.Key, "")
	if privateFilterB.Code != http.StatusOK || strings.Contains(privateFilterB.Body.String(), thread.ID) {
		t.Fatalf("private filter status=%d body=%s", privateFilterB.Code, privateFilterB.Body.String())
	}
	publicFilterB := request(http.MethodGet, "/api/threads?filter=public", keyB.Key, "")
	if publicFilterB.Code != http.StatusOK || strings.Contains(publicFilterB.Body.String(), thread.ID) {
		t.Fatalf("public filter status=%d body=%s", publicFilterB.Code, publicFilterB.Body.String())
	}
	invalidFilterB := request(http.MethodGet, "/api/threads?filter=team", keyB.Key, "")
	if invalidFilterB.Code != http.StatusBadRequest || !strings.Contains(invalidFilterB.Body.String(), `"code":"INVALID_ARGUMENT"`) {
		t.Fatalf("invalid filter status=%d body=%s", invalidFilterB.Code, invalidFilterB.Body.String())
	}
	searchB := request(http.MethodGet, "/api/threads?query=team-shared", keyB.Key, "")
	if searchB.Code != http.StatusOK || !strings.Contains(searchB.Body.String(), thread.ID) {
		t.Fatalf("team member search status=%d body=%s", searchB.Code, searchB.Body.String())
	}
	detailB := request(http.MethodGet, "/api/threads/"+thread.ID, keyB.Key, "")
	if detailB.Code != http.StatusOK || !strings.Contains(detailB.Body.String(), `"shared_teams"`) || !strings.Contains(detailB.Body.String(), team.ID) {
		t.Fatalf("team member detail status=%d body=%s", detailB.Code, detailB.Body.String())
	}
	postB := request(http.MethodPost, "/api/threads/"+thread.ID+"/messages", keyB.Key, `{"body":"team participant reply"}`)
	if postB.Code != http.StatusCreated || !strings.Contains(postB.Body.String(), `"author":"agent-b"`) {
		t.Fatalf("team member post status=%d body=%s", postB.Code, postB.Body.String())
	}

	uploadIntent := request(http.MethodPost, "/api/threads/"+thread.ID+"/uploads", keyB.Key, `{"files":[{"file_name":"team-note.txt","mime_type":"text/plain","size_bytes":4,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)
	if uploadIntent.Code != http.StatusCreated {
		t.Fatalf("team upload intent status=%d body=%s", uploadIntent.Code, uploadIntent.Body.String())
	}
	var uploadPayload struct {
		Uploads []struct {
			UploadID string `json:"upload_id"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(uploadIntent.Body.Bytes(), &uploadPayload); err != nil {
		t.Fatal(err)
	}
	if len(uploadPayload.Uploads) != 1 || uploadPayload.Uploads[0].UploadID == "" || strings.Contains(uploadIntent.Body.String(), "storage_key") {
		t.Fatalf("team upload payload=%#v", uploadPayload)
	}
	teamContentType := "text/plain"
	pendingStorageKey := ""
	for _, pending := range repo.Pending {
		if pending.ID == uploadPayload.Uploads[0].UploadID {
			pendingStorageKey = pending.StorageKey
			break
		}
	}
	if pendingStorageKey == "" {
		t.Fatalf("pending upload %s not found: %#v", uploadPayload.Uploads[0].UploadID, repo.Pending)
	}
	store.PutAssetObjectWithSHA(pendingStorageKey, 4, &teamContentType, strings.Repeat("a", 64))
	finalizeBody, _ := json.Marshal(map[string]any{
		"body":       "team upload finalization",
		"upload_ids": []string{uploadPayload.Uploads[0].UploadID},
	})
	finalized := request(http.MethodPost, "/api/threads/"+thread.ID+"/messages", keyB.Key, string(finalizeBody))
	if finalized.Code != http.StatusCreated {
		t.Fatalf("team upload finalization status=%d body=%s", finalized.Code, finalized.Body.String())
	}
	var finalizedPayload struct {
		Message types.Message `json:"message"`
	}
	if err := json.Unmarshal(finalized.Body.Bytes(), &finalizedPayload); err != nil {
		t.Fatal(err)
	}
	if len(finalizedPayload.Message.Assets) != 1 {
		t.Fatalf("finalized team asset payload=%#v", finalizedPayload)
	}
	assetID := finalizedPayload.Message.Assets[0].ID
	downloadB := request(http.MethodGet, "/api/assets/"+assetID+"/download", keyB.Key, "")
	if downloadB.Code != http.StatusOK || !strings.Contains(downloadB.Body.String(), `"download_url"`) {
		t.Fatalf("team asset signing status=%d body=%s", downloadB.Code, downloadB.Body.String())
	}

	unshared := request(http.MethodPatch, "/api/threads/"+thread.ID+"/visibility", keyB.Key, `{"remove_teams":["`+team.ID+`"]}`)
	if unshared.Code != http.StatusOK || !strings.Contains(unshared.Body.String(), `"shared_teams":[]`) {
		t.Fatalf("participant unshare status=%d body=%s", unshared.Code, unshared.Body.String())
	}
	revokedB := request(http.MethodGet, "/api/threads/"+thread.ID, keyB.Key, "")
	if revokedB.Code != http.StatusNotFound {
		t.Fatalf("participant retained access after removing its share: status=%d body=%s", revokedB.Code, revokedB.Body.String())
	}
	ownerStillHasAccess := request(http.MethodGet, "/api/threads/"+thread.ID, keyA.Key, "")
	if ownerStillHasAccess.Code != http.StatusOK {
		t.Fatalf("thread owner lost access: status=%d body=%s", ownerStillHasAccess.Code, ownerStillHasAccess.Body.String())
	}

	sharedAgain := request(http.MethodPatch, "/api/threads/"+thread.ID+"/visibility", keyA.Key, string(shareBody))
	if sharedAgain.Code != http.StatusOK {
		t.Fatalf("reshare status=%d body=%s", sharedAgain.Code, sharedAgain.Body.String())
	}
	outsiderC := request(http.MethodGet, "/api/threads/"+thread.ID, keyC.Key, "")
	if outsiderC.Code != http.StatusNotFound {
		t.Fatalf("unrelated API key bypassed membership: status=%d body=%s", outsiderC.Code, outsiderC.Body.String())
	}
	if _, err := repo.RemoveTeamMember(t.Context(), team.ID, authB.UserID); err != nil {
		t.Fatal(err)
	}
	membershipRevokedB := request(http.MethodGet, "/api/threads/"+thread.ID, keyB.Key, "")
	if membershipRevokedB.Code != http.StatusNotFound {
		t.Fatalf("removed member retained access: status=%d body=%s", membershipRevokedB.Code, membershipRevokedB.Body.String())
	}
	downloadRevokedB := request(http.MethodGet, "/api/assets/"+assetID+"/download", keyB.Key, "")
	if downloadRevokedB.Code != http.StatusNotFound {
		t.Fatalf("removed member retained asset signing: status=%d body=%s", downloadRevokedB.Code, downloadRevokedB.Body.String())
	}
	if _, err := repo.AddTeamMember(t.Context(), team.ID, authB.UserID); err != nil {
		t.Fatal(err)
	}
	membershipRestoredB := request(http.MethodGet, "/api/threads/"+thread.ID, keyB.Key, "")
	if membershipRestoredB.Code != http.StatusOK {
		t.Fatalf("re-added member did not regain access: status=%d body=%s", membershipRestoredB.Code, membershipRestoredB.Body.String())
	}
}

func TestHTTPPublicThreadLinkLifecycleIsReadOnlyAndTokenScoped(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	ownerAuth := types.AuthContext{UserID: "usr_http_public_owner", UserDisplayName: "HTTP Public Owner", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_http_public_owner", ActorName: "Web dashboard"}
	memberAuth := types.AuthContext{UserID: "usr_http_public_member", UserDisplayName: "HTTP Public Member", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_http_public_member", ActorName: "Web dashboard"}
	outsiderAuth := types.AuthContext{UserID: "usr_http_public_outsider", UserDisplayName: "HTTP Public Outsider", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_http_public_outsider", ActorName: "Web dashboard"}
	repo.Users = append(repo.Users,
		types.User{ID: ownerAuth.UserID, Email: "http-public-owner@example.com", DisplayName: ownerAuth.UserDisplayName},
		types.User{ID: memberAuth.UserID, Email: "http-public-member@example.com", DisplayName: memberAuth.UserDisplayName},
		types.User{ID: outsiderAuth.UserID, Email: "http-public-outsider@example.com", DisplayName: outsiderAuth.UserDisplayName},
	)
	team, err := repo.CreateTeam(t.Context(), "http-public-team", "HTTP Public Team")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddTeamMember(t.Context(), team.ID, memberAuth.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddTeamMember(t.Context(), team.ID, ownerAuth.UserID); err != nil {
		t.Fatal(err)
	}
	ownerKey, err := svc.CreateAPIKey(t.Context(), ownerAuth, "public-owner")
	if err != nil {
		t.Fatal(err)
	}
	memberKey, err := svc.CreateAPIKey(t.Context(), memberAuth, "public-member")
	if err != nil {
		t.Fatal(err)
	}
	outsiderKey, err := svc.CreateAPIKey(t.Context(), outsiderAuth, "public-outsider")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := repo.CreateThread(t.Context(), ownerAuth.UserID, "HTTP public marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(t.Context(), repo, ownerAuth.UserID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	mimeType := "image/png"
	message, err := repo.PostMessage(t.Context(), ownerAuth.UserID, thread.ID, ownerAuth, "HTTP public body", nil, []types.NewAsset{{StorageKey: "agentbox/" + ownerAuth.UserID + "/" + thread.ID + "/http-public.png", FileName: "http-public.png", MimeType: &mimeType, SizeBytes: 7}})
	if err != nil || len(message.Assets) != 1 {
		t.Fatalf("public HTTP fixture message=%#v err=%v", message, err)
	}
	store.PutAssetObject(message.Assets[0].StorageKey, 7, &mimeType)
	otherThread, err := repo.CreateThread(t.Context(), ownerAuth.UserID, "HTTP other marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, err := repo.PostMessage(t.Context(), ownerAuth.UserID, otherThread.ID, ownerAuth, "HTTP other body", nil, []types.NewAsset{{StorageKey: "agentbox/" + ownerAuth.UserID + "/" + otherThread.ID + "/http-other.png", FileName: "http-other.png", MimeType: &mimeType, SizeBytes: 5}})
	if err != nil || len(otherMessage.Assets) != 1 {
		t.Fatalf("other HTTP fixture message=%#v err=%v", otherMessage, err)
	}
	store.PutAssetObject(otherMessage.Assets[0].StorageKey, 5, &mimeType)

	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName, AppPublicURL: "https://agentbox.example"}, svc)
	request := func(method string, path string, secret string, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if secret != "" {
			req.Header.Set("authorization", "Bearer "+secret)
		}
		if body != "" {
			req.Header.Set("content-type", "application/json")
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}

	initial := request(http.MethodGet, "/api/threads/"+thread.ID+"/visibility", memberKey.Key, "")
	if initial.Code != http.StatusOK || !strings.Contains(initial.Body.String(), `"public":false`) || strings.Contains(initial.Body.String(), `"public_url":"https://`) {
		t.Fatalf("initial visibility status=%d body=%s", initial.Code, initial.Body.String())
	}
	createdResponse := request(http.MethodPatch, "/api/threads/"+thread.ID+"/visibility", memberKey.Key, `{"public":true}`)
	if createdResponse.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		Visibility types.ManagedThreadVisibility `json:"visibility"`
	}
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Visibility.PublicLink == nil || !strings.HasPrefix(created.Visibility.PublicURL, "https://agentbox.example/share/agpub_") || created.Visibility.PublicLink.Token != "" || created.Visibility.PublicLink.TokenHash != "" {
		t.Fatalf("created public visibility=%#v body=%s", created, createdResponse.Body.String())
	}
	createdToken := strings.TrimPrefix(created.Visibility.PublicURL, "https://agentbox.example/share/")
	idempotent := request(http.MethodPatch, "/api/threads/"+thread.ID+"/visibility", memberKey.Key, `{"public":true}`)
	if idempotent.Code != http.StatusOK || !strings.Contains(idempotent.Body.String(), createdToken) {
		t.Fatalf("idempotent publish status=%d body=%s", idempotent.Code, idempotent.Body.String())
	}
	metadata := request(http.MethodGet, "/api/threads/"+thread.ID+"/visibility", ownerKey.Key, "")
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"public_url":"https://agentbox.example/share/`+createdToken+`"`) || strings.Contains(metadata.Body.String(), "token_hash") || !strings.Contains(metadata.Body.String(), "token_prefix") {
		t.Fatalf("visibility metadata status=%d body=%s", metadata.Code, metadata.Body.String())
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		retired := request(method, "/api/threads/"+thread.ID+"/public-link", memberKey.Key, `{}`)
		if retired.Code != http.StatusNotFound {
			t.Fatalf("retired public-link %s remained live: status=%d body=%s", method, retired.Code, retired.Body.String())
		}
	}

	publicView := request(http.MethodGet, "/api/public/threads/"+createdToken, "", "")
	if publicView.Code != http.StatusOK || publicView.Header().Get("Cache-Control") != "no-store" || !strings.Contains(publicView.Body.String(), "HTTP public marker") || !strings.Contains(publicView.Body.String(), message.Assets[0].ID) || !strings.Contains(publicView.Body.String(), `"preview_path"`) {
		t.Fatalf("public view status=%d cache=%q body=%s", publicView.Code, publicView.Header().Get("Cache-Control"), publicView.Body.String())
	}
	for _, forbidden := range []string{"tenant_id", "owner_user_id", "created_by_user_id", "created_by_key_id", "storage_key", "token_hash"} {
		if strings.Contains(publicView.Body.String(), forbidden) {
			t.Fatalf("public HTTP payload leaked %q: %s", forbidden, publicView.Body.String())
		}
	}
	publicWrite := request(http.MethodPost, "/api/public/threads/"+createdToken, "", `{"body":"blocked"}`)
	if publicWrite.Code != http.StatusMethodNotAllowed {
		t.Fatalf("public link accepted write: status=%d body=%s", publicWrite.Code, publicWrite.Body.String())
	}
	publicDownload := request(http.MethodGet, "/api/public/threads/"+createdToken+"/assets/"+message.Assets[0].ID+"/download", "", "")
	if publicDownload.Code != http.StatusOK || !strings.Contains(publicDownload.Body.String(), `"download_url"`) {
		t.Fatalf("public download status=%d body=%s", publicDownload.Code, publicDownload.Body.String())
	}
	publicPreview := request(http.MethodGet, "/api/public/threads/"+createdToken+"/assets/"+message.Assets[0].ID+"/preview", "", "")
	if publicPreview.Code != http.StatusOK || !strings.Contains(publicPreview.Body.String(), `"available":true`) || !strings.Contains(publicPreview.Body.String(), `"preview_url"`) || !strings.Contains(publicPreview.Body.String(), "inline") {
		t.Fatalf("public preview status=%d location=%q body=%s", publicPreview.Code, publicPreview.Header().Get("Location"), publicPreview.Body.String())
	}

	if err := store.DeleteAssetObject(t.Context(), message.Assets[0].StorageKey); err != nil {
		t.Fatal(err)
	}
	missingView := request(http.MethodGet, "/api/public/threads/"+createdToken, "", "")
	if missingView.Code != http.StatusOK || !strings.Contains(missingView.Body.String(), `"download_path"`) || !strings.Contains(missingView.Body.String(), `"preview_path"`) {
		t.Fatalf("public thread failed because one object is missing: status=%d body=%s", missingView.Code, missingView.Body.String())
	}
	missingDownload := request(http.MethodGet, "/api/public/threads/"+createdToken+"/assets/"+message.Assets[0].ID+"/download", "", "")
	if missingDownload.Code != http.StatusOK || !strings.Contains(missingDownload.Body.String(), `"available":false`) || !strings.Contains(missingDownload.Body.String(), `"unavailable_reason"`) || strings.Contains(missingDownload.Body.String(), `"download_url"`) {
		t.Fatalf("missing public download was not asset-scoped: status=%d body=%s", missingDownload.Code, missingDownload.Body.String())
	}
	missingPreview := request(http.MethodGet, "/api/public/threads/"+createdToken+"/assets/"+message.Assets[0].ID+"/preview", "", "")
	if missingPreview.Code != http.StatusOK || !strings.Contains(missingPreview.Body.String(), `"available":false`) || strings.Contains(missingPreview.Body.String(), `"preview_url"`) {
		t.Fatalf("missing public preview was not asset-scoped: status=%d body=%s", missingPreview.Code, missingPreview.Body.String())
	}
	stillReadable := request(http.MethodGet, "/api/public/threads/"+createdToken, "", "")
	if stillReadable.Code != http.StatusOK || !strings.Contains(stillReadable.Body.String(), "HTTP public marker") {
		t.Fatalf("missing object invalidated public thread: status=%d body=%s", stillReadable.Code, stillReadable.Body.String())
	}
	crossThreadDownload := request(http.MethodGet, "/api/public/threads/"+createdToken+"/assets/"+otherMessage.Assets[0].ID+"/download", "", "")
	if crossThreadDownload.Code != http.StatusNotFound || !strings.Contains(crossThreadDownload.Body.String(), "PUBLIC_ASSET_NOT_FOUND") {
		t.Fatalf("cross-thread public download status=%d body=%s", crossThreadDownload.Code, crossThreadDownload.Body.String())
	}

	outsiderRotate := request(http.MethodPatch, "/api/threads/"+thread.ID+"/visibility", outsiderKey.Key, `{"regenerate_public_link":true}`)
	if outsiderRotate.Code != http.StatusNotFound {
		t.Fatalf("outsider rotated public link: status=%d body=%s", outsiderRotate.Code, outsiderRotate.Body.String())
	}
	rotatedResponse := request(http.MethodPatch, "/api/threads/"+thread.ID+"/visibility", ownerKey.Key, `{"regenerate_public_link":true}`)
	if rotatedResponse.Code != http.StatusOK {
		t.Fatalf("rotate visibility status=%d body=%s", rotatedResponse.Code, rotatedResponse.Body.String())
	}
	var rotated struct {
		Visibility types.ManagedThreadVisibility `json:"visibility"`
	}
	if err := json.Unmarshal(rotatedResponse.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	rotatedToken := strings.TrimPrefix(rotated.Visibility.PublicURL, "https://agentbox.example/share/")
	if rotated.Visibility.PublicLink == nil || !strings.HasPrefix(rotatedToken, "agpub_") || rotatedToken == createdToken {
		t.Fatalf("rotated public visibility=%#v", rotated)
	}
	oldView := request(http.MethodGet, "/api/public/threads/"+createdToken, "", "")
	if oldView.Code != http.StatusNotFound {
		t.Fatalf("old public URL remained active: status=%d body=%s", oldView.Code, oldView.Body.String())
	}
	newView := request(http.MethodGet, "/api/public/threads/"+rotatedToken, "", "")
	if newView.Code != http.StatusOK {
		t.Fatalf("rotated public URL inactive: status=%d body=%s", newView.Code, newView.Body.String())
	}
	unpublish := request(http.MethodPatch, "/api/threads/"+thread.ID+"/visibility", memberKey.Key, `{"public":false}`)
	if unpublish.Code != http.StatusOK || !strings.Contains(unpublish.Body.String(), `"public":false`) {
		t.Fatalf("unpublish status=%d body=%s", unpublish.Code, unpublish.Body.String())
	}
	revokedView := request(http.MethodGet, "/api/public/threads/"+rotatedToken, "", "")
	if revokedView.Code != http.StatusNotFound {
		t.Fatalf("revoked public URL remained active: status=%d body=%s", revokedView.Code, revokedView.Body.String())
	}
	revokedDownload := request(http.MethodGet, "/api/public/threads/"+rotatedToken+"/assets/"+message.Assets[0].ID+"/download", "", "")
	if revokedDownload.Code != http.StatusNotFound {
		t.Fatalf("revoked public URL signed attachment: status=%d body=%s", revokedDownload.Code, revokedDownload.Body.String())
	}
}

func TestAPIKeyScopesConstrainThreadAndAssetRoutes(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	adminAuth := authContext("global", "admin")
	adminAuth.SubjectType = types.AuthSubjectUserSession
	repo.Users = append(repo.Users, types.User{ID: adminAuth.UserID, Email: "admin@example.com", DisplayName: "Admin"})
	thread, err := svc.CreateThread(t.Context(), adminAuth, "Scoped")
	if err != nil {
		t.Fatal(err)
	}
	textType := "text/plain"
	message, err := repo.PostMessage(t.Context(), adminAuth.UserID, thread.ID, adminAuth, "seed asset", nil, []types.NewAsset{{
		StorageKey: "agentbox/" + adminAuth.UserID + "/scoped/message/seed.txt",
		FileName:   "seed.txt",
		MimeType:   &textType,
		SizeBytes:  int64(len("seed bytes")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Assets) != 1 {
		t.Fatalf("expected asset, got %#v", message.Assets)
	}
	store.PutAssetObject(message.Assets[0].StorageKey, int64(len("seed bytes")), &textType)
	restrictedKey, err := svc.CreateAPIKeyWithScopes(t.Context(), adminAuth, "keys-only", []string{"keys:read"})
	if err != nil {
		t.Fatal(err)
	}
	threadReadKey, err := svc.CreateAPIKeyWithScopes(t.Context(), adminAuth, "thread-reader", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	scopedKey, err := svc.CreateAPIKeyWithScopes(t.Context(), adminAuth, "worker", []string{"threads:read", "threads:write", "assets:read", "assets:write"})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{}, svc)

	for label, req := range map[string]*http.Request{
		"list":        httptest.NewRequest(http.MethodGet, "/api/threads?key="+url.QueryEscape(restrictedKey.Key), nil),
		"get":         httptest.NewRequest(http.MethodGet, "/api/threads/"+thread.ID+"?key="+url.QueryEscape(restrictedKey.Key), nil),
		"create":      httptest.NewRequest(http.MethodPost, "/api/threads?key="+url.QueryEscape(restrictedKey.Key), strings.NewReader(`{"title":"Nope"}`)),
		"post":        httptest.NewRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages?key="+url.QueryEscape(restrictedKey.Key), strings.NewReader(`{"body":"nope"}`)),
		"upload":      httptest.NewRequest(http.MethodPost, "/api/threads/"+thread.ID+"/uploads?key="+url.QueryEscape(restrictedKey.Key), strings.NewReader(`{"files":[{"file_name":"asset.txt"}]}`)),
		"downloadURL": httptest.NewRequest(http.MethodGet, "/api/assets/"+message.Assets[0].ID+"/download-url?key="+url.QueryEscape(restrictedKey.Key), nil),
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d body=%s", label, recorder.Code, recorder.Body.String())
		}
	}

	viewerThreadOnly := httptest.NewRecorder()
	server.ServeHTTP(viewerThreadOnly, httptest.NewRequest(http.MethodGet, "/api/threads/"+thread.ID+"/view?key="+url.QueryEscape(threadReadKey.Key), nil))
	if viewerThreadOnly.Code != http.StatusForbidden {
		t.Fatalf("thread-read-only viewer status=%d body=%s", viewerThreadOnly.Code, viewerThreadOnly.Body.String())
	}

	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/threads?key="+url.QueryEscape(scopedKey.Key), nil))
	if list.Code != http.StatusOK {
		t.Fatalf("scoped list status=%d body=%s", list.Code, list.Body.String())
	}
	post := httptest.NewRecorder()
	server.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages?key="+url.QueryEscape(scopedKey.Key), strings.NewReader(`{"body":"ok"}`)))
	if post.Code != http.StatusCreated {
		t.Fatalf("scoped post status=%d body=%s", post.Code, post.Body.String())
	}
	downloadURL := httptest.NewRecorder()
	server.ServeHTTP(downloadURL, httptest.NewRequest(http.MethodGet, "/api/assets/"+message.Assets[0].ID+"/download-url?key="+url.QueryEscape(scopedKey.Key), nil))
	if downloadURL.Code != http.StatusOK {
		t.Fatalf("scoped download-url status=%d body=%s", downloadURL.Code, downloadURL.Body.String())
	}
	viewerScoped := httptest.NewRecorder()
	server.ServeHTTP(viewerScoped, httptest.NewRequest(http.MethodGet, "/api/threads/"+thread.ID+"/view?key="+url.QueryEscape(scopedKey.Key), nil))
	if viewerScoped.Code != http.StatusOK {
		t.Fatalf("scoped viewer status=%d body=%s", viewerScoped.Code, viewerScoped.Body.String())
	}
	if !strings.Contains(viewerScoped.Body.String(), `"download_path"`) || strings.Contains(viewerScoped.Body.String(), `"download_url"`) || !strings.Contains(viewerScoped.Body.String(), "seed.txt") {
		t.Fatalf("scoped viewer missing lazy asset data: %s", viewerScoped.Body.String())
	}
}

func TestThreadListAndSearchExposeStableContinuationPages(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	user := types.User{ID: "usr_http_page", Email: "page@example.com", DisplayName: "Page User"}
	repo.Users = append(repo.Users, user)
	browser := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_http_page", ActorName: "Web dashboard"}
	key, err := svc.CreateAPIKey(t.Context(), browser, "page-client")
	if err != nil {
		t.Fatal(err)
	}
	summaryThreadID := ""
	for index, suffix := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		thread, err := svc.CreateThread(t.Context(), browser, "cursor marker "+suffix)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			summaryThreadID = thread.ID
		}
	}
	for _, body := range []string{"first summary message", "second summary message"} {
		if _, err := svc.PostMessage(t.Context(), browser, service.PostMessageParams{ThreadID: summaryThreadID, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	for index := range repo.Messages {
		if repo.Messages[index].ThreadID != summaryThreadID {
			continue
		}
		if repo.Messages[index].Body == "first summary message" {
			repo.Messages[index].CreatedAt = "2026-08-03T12:34:54.000Z"
		}
		if repo.Messages[index].Body == "second summary message" {
			repo.Messages[index].CreatedAt = "2026-08-03T12:34:55.000Z"
		}
	}
	for index := range repo.Threads {
		repo.Threads[index].UpdatedAt = "2026-08-03T12:34:56.123Z"
	}
	server := NewServer(config.Config{}, svc)

	type pagePayload struct {
		Threads []types.Thread       `json:"threads"`
		Page    types.ThreadPageInfo `json:"page"`
	}
	requestPage := func(extraQuery string, cursor string) pagePayload {
		t.Helper()
		path := "/api/threads?key=" + url.QueryEscape(key.Key) + "&limit=2" + extraQuery
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("page status=%d body=%s", response.Code, response.Body.String())
		}
		var payload pagePayload
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Page.Limit != 2 || len(payload.Threads) > 2 {
			t.Fatalf("page=%#v", payload)
		}
		return payload
	}
	traverse := func(extraQuery string) []string {
		t.Helper()
		ids := []string{}
		seen := map[string]bool{}
		summarySeen := false
		cursor := ""
		for pageNumber := 0; pageNumber < 10; pageNumber++ {
			page := requestPage(extraQuery, cursor)
			for _, thread := range page.Threads {
				if thread.MessageCount == nil || thread.LastMessagePreview == nil {
					t.Fatalf("thread summary omitted from list/search page: %#v", thread)
				}
				if thread.ID == summaryThreadID {
					summarySeen = true
					if *thread.MessageCount != 2 || *thread.LastMessagePreview != "second summary message" {
						t.Fatalf("thread summary=%d %q, want 2 and latest body", *thread.MessageCount, *thread.LastMessagePreview)
					}
				}
				if seen[thread.ID] {
					t.Fatalf("duplicate thread %s across continuation pages", thread.ID)
				}
				seen[thread.ID] = true
				ids = append(ids, thread.ID)
			}
			if !page.Page.HasMore {
				if page.Page.NextCursor != nil {
					t.Fatalf("terminal page exposed next cursor: %#v", page.Page)
				}
				if !summarySeen {
					t.Fatalf("summary thread %s was not traversed", summaryThreadID)
				}
				return ids
			}
			if page.Page.NextCursor == nil || *page.Page.NextCursor == "" {
				t.Fatalf("continuation page omitted cursor: %#v", page.Page)
			}
			cursor = *page.Page.NextCursor
		}
		t.Fatal("continuation traversal did not terminate")
		return nil
	}

	expected, err := svc.ListThreadsFiltered(t.Context(), browser, types.ThreadListParams{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	expectedIDs := make([]string, 0, len(expected))
	for _, thread := range expected {
		expectedIDs = append(expectedIDs, thread.ID)
	}
	if got := traverse(""); !reflect.DeepEqual(got, expectedIDs) {
		t.Fatalf("list traversal IDs=%v, want %v", got, expectedIDs)
	}
	if got := traverse("&query=cursor%20marker"); !reflect.DeepEqual(got, expectedIDs) {
		t.Fatalf("search traversal IDs=%v, want %v", got, expectedIDs)
	}

	invalid := httptest.NewRecorder()
	server.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/threads?key="+url.QueryEscape(key.Key)+"&cursor=not-valid!", nil))
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"code":"INVALID_ARGUMENT"`) || !strings.Contains(invalid.Body.String(), "cursor is invalid") {
		t.Fatalf("invalid cursor status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}
