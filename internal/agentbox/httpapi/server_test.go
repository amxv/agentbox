package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	authpkg "agentbox/internal/agentbox/auth"
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
	if _, err := svc.CreateAPIKey(t.Context(), authContext(types.DefaultTenantID, "local"), "local"); err != nil {
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

func TestViewerRoutesRequireAdminAndAddPreviewURLs(t *testing.T) {
	imageType := "image/png"
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	passwordHash, err := authpkg.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	repo.Users = append(repo.Users, testUser(types.DefaultTenantID, "usr_viewer", "viewer@example.com", "Viewer Admin", "admin", passwordHash))
	thread, err := svc.CreateThread(t.Context(), authContext(types.DefaultTenantID, "tester"), "Viewer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PostMessage(t.Context(), types.DefaultTenantID, thread.ID, authContext(types.DefaultTenantID, "author"), "body", nil, []types.NewAsset{{
		StorageKey: "agentbox/thread/message/image.png",
		FileName:   "image.png",
		MimeType:   &imageType,
		SizeBytes:  10,
	}}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{AdminKey: "adm", Environment: "production", SessionCookieName: config.DefaultSessionCookieName}, svc)

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/viewer/threads", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"viewer@example.com","password":"secret"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	sessionCookie := login.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/api/viewer/threads/"+thread.ID, nil)
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

func TestBrowserSessionAuthLifecycleAndTenantKeys(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	passwordHash, err := authpkg.HashPassword("let-me-in")
	if err != nil {
		t.Fatal(err)
	}
	repo.Users = append(repo.Users,
		testUser("ten_a", "usr_a", "a@example.com", "Alice Admin", "admin", passwordHash),
		testUser("ten_b", "usr_b", "b@example.com", "Bob Admin", "admin", passwordHash),
	)
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, svc)

	badLogin := httptest.NewRecorder()
	server.ServeHTTP(badLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"a@example.com","password":"wrong"}`)))
	if badLogin.Code != http.StatusUnauthorized {
		t.Fatalf("badLogin status=%d body=%s", badLogin.Code, badLogin.Body.String())
	}

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"a@example.com","password":"let-me-in"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != config.DefaultSessionCookieName || cookies[0].Value == "" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookies = %#v", cookies)
	}
	sessionCookie := cookies[0]

	me := httptest.NewRecorder()
	reqMe := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	reqMe.AddCookie(sessionCookie)
	server.ServeHTTP(me, reqMe)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"actor_name":"Alice Admin"`) || !strings.Contains(me.Body.String(), `"tenant_id":"ten_a"`) {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}

	create := httptest.NewRecorder()
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(`{"title":"Session thread"}`))
	reqCreate.AddCookie(sessionCookie)
	server.ServeHTTP(create, reqCreate)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
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
	if created.Thread.CreatedBy != "Alice Admin" {
		t.Fatalf("created = %#v", created)
	}

	post := httptest.NewRecorder()
	reqPost := httptest.NewRequest(http.MethodPost, "/api/threads/"+created.Thread.ID+"/messages", strings.NewReader(`{"body":"from session"}`))
	reqPost.AddCookie(sessionCookie)
	server.ServeHTTP(post, reqPost)
	if post.Code != http.StatusCreated || !strings.Contains(post.Body.String(), `"author":"Alice Admin"`) {
		t.Fatalf("post status=%d body=%s", post.Code, post.Body.String())
	}

	keyCreate := httptest.NewRecorder()
	reqKeyCreate := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"name":"raycast"}`))
	reqKeyCreate.AddCookie(sessionCookie)
	server.ServeHTTP(keyCreate, reqKeyCreate)
	if keyCreate.Code != http.StatusCreated {
		t.Fatalf("keyCreate status=%d body=%s", keyCreate.Code, keyCreate.Body.String())
	}
	if !strings.Contains(keyCreate.Body.String(), `"name":"raycast"`) || !strings.Contains(keyCreate.Body.String(), `"key":"`) {
		t.Fatalf("keyCreate body=%s", keyCreate.Body.String())
	}

	keyList := httptest.NewRecorder()
	reqKeyList := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	reqKeyList.AddCookie(sessionCookie)
	server.ServeHTTP(keyList, reqKeyList)
	if keyList.Code != http.StatusOK || !strings.Contains(keyList.Body.String(), `"name":"raycast"`) {
		t.Fatalf("keyList status=%d body=%s", keyList.Code, keyList.Body.String())
	}

	loginB := httptest.NewRecorder()
	server.ServeHTTP(loginB, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"b@example.com","password":"let-me-in"}`)))
	if loginB.Code != http.StatusOK {
		t.Fatalf("loginB status=%d body=%s", loginB.Code, loginB.Body.String())
	}
	cookieB := loginB.Result().Cookies()[0]
	getAWithB := httptest.NewRecorder()
	reqGetAWithB := httptest.NewRequest(http.MethodGet, "/api/threads/"+created.Thread.ID, nil)
	reqGetAWithB.AddCookie(cookieB)
	server.ServeHTTP(getAWithB, reqGetAWithB)
	if getAWithB.Code != http.StatusNotFound {
		t.Fatalf("getAWithB status=%d body=%s", getAWithB.Code, getAWithB.Body.String())
	}

	logout := httptest.NewRecorder()
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqLogout.AddCookie(sessionCookie)
	server.ServeHTTP(logout, reqLogout)
	if logout.Code != http.StatusOK || len(logout.Result().Cookies()) == 0 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout status=%d cookies=%#v body=%s", logout.Code, logout.Result().Cookies(), logout.Body.String())
	}
	afterLogout := httptest.NewRecorder()
	reqAfterLogout := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	reqAfterLogout.AddCookie(sessionCookie)
	server.ServeHTTP(afterLogout, reqAfterLogout)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("afterLogout status=%d body=%s", afterLogout.Code, afterLogout.Body.String())
	}
}

func TestCLIAuthAuthorizeAndExchange(t *testing.T) {
	repo := &db.MemoryRepository{
		Tenants: []types.Tenant{{ID: "ten_acme", Slug: "acme", Name: "Acme"}},
	}
	svc := service.New(repo, &assets.FakeStore{})
	passwordHash, err := authpkg.HashPassword("let-me-in")
	if err != nil {
		t.Fatal(err)
	}
	repo.Users = append(repo.Users, testUser("ten_acme", "usr_acme", "admin@example.com", "Acme Admin", "admin", passwordHash))
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, svc)

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"tenant_id":"ten_acme","email":"admin@example.com","password":"let-me-in"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	sessionCookie := login.Result().Cookies()[0]

	unauthAuthorize := httptest.NewRecorder()
	server.ServeHTTP(unauthAuthorize, httptest.NewRequest(http.MethodPost, "/api/auth/cli/authorize", strings.NewReader(`{"state":"state","redirect_uri":"http://127.0.0.1:3456/callback"}`)))
	if unauthAuthorize.Code != http.StatusUnauthorized {
		t.Fatalf("unauthAuthorize status=%d body=%s", unauthAuthorize.Code, unauthAuthorize.Body.String())
	}

	authorize := httptest.NewRecorder()
	reqAuthorize := httptest.NewRequest(http.MethodPost, "/api/auth/cli/authorize", strings.NewReader(`{"state":"state","redirect_uri":"http://127.0.0.1:3456/callback"}`))
	reqAuthorize.AddCookie(sessionCookie)
	server.ServeHTTP(authorize, reqAuthorize)
	if authorize.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", authorize.Code, authorize.Body.String())
	}
	var authorized struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(authorize.Body.Bytes(), &authorized); err != nil {
		t.Fatal(err)
	}
	if authorized.Code == "" {
		t.Fatalf("authorize body=%s", authorize.Body.String())
	}

	exchange := httptest.NewRecorder()
	server.ServeHTTP(exchange, httptest.NewRequest(http.MethodPost, "/api/auth/cli/exchange", strings.NewReader(`{"code":"`+authorized.Code+`","state":"state","redirect_uri":"http://127.0.0.1:3456/callback","key_name":"cli-test"}`)))
	if exchange.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", exchange.Code, exchange.Body.String())
	}
	var exchanged struct {
		APIKey struct {
			Name   string `json:"name"`
			Secret string `json:"key"`
		} `json:"api_key"`
		Tenant types.Tenant `json:"tenant"`
		User   types.User   `json:"user"`
	}
	if err := json.Unmarshal(exchange.Body.Bytes(), &exchanged); err != nil {
		t.Fatal(err)
	}
	if exchanged.APIKey.Name != "cli-test" || exchanged.APIKey.Secret == "" || exchanged.Tenant.ID != "ten_acme" || exchanged.Tenant.Slug != "acme" || exchanged.User.ID != "usr_acme" {
		t.Fatalf("exchanged = %#v", exchanged)
	}
	if len(repo.APIKeys) != 1 || repo.APIKeys[0].UserID == nil || *repo.APIKeys[0].UserID != "usr_acme" {
		t.Fatalf("repo API keys = %#v", repo.APIKeys)
	}

	reuse := httptest.NewRecorder()
	server.ServeHTTP(reuse, httptest.NewRequest(http.MethodPost, "/api/auth/cli/exchange", strings.NewReader(`{"code":"`+authorized.Code+`","state":"state","redirect_uri":"http://127.0.0.1:3456/callback","key_name":"cli-test"}`)))
	if reuse.Code != http.StatusForbidden {
		t.Fatalf("reuse status=%d body=%s", reuse.Code, reuse.Body.String())
	}
}

func TestAdminKeyRoutesCreateListRevokeAndAuthenticate(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	server := NewServer(config.Config{AdminKey: "adm"}, svc)

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/keys", strings.NewReader(`{"name":"chatgpt"}`))
	createReq.Header.Set("x-agentbox-admin-key", "adm")
	create := httptest.NewRecorder()
	server.ServeHTTP(create, createReq)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		Key struct {
			Name      string `json:"name"`
			Secret    string `json:"key"`
			KeyMasked string `json:"key_masked"`
		} `json:"key"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Key.Name != "chatgpt" || created.Key.Secret == "" || created.Key.KeyMasked == "" {
		t.Fatalf("created = %#v", created)
	}

	apiCreate := httptest.NewRecorder()
	server.ServeHTTP(apiCreate, httptest.NewRequest(http.MethodPost, "/api/threads?key="+created.Key.Secret, strings.NewReader(`{"title":"DB key"}`)))
	if apiCreate.Code != http.StatusCreated {
		t.Fatalf("authenticated create status = %d body=%s", apiCreate.Code, apiCreate.Body.String())
	}
	var threadPayload struct {
		Thread struct {
			CreatedBy string `json:"created_by"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(apiCreate.Body.Bytes(), &threadPayload); err != nil {
		t.Fatal(err)
	}
	if threadPayload.Thread.CreatedBy != "chatgpt" {
		t.Fatalf("thread payload = %#v", threadPayload)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	listReq.Header.Set("x-agentbox-admin-key", "adm")
	list := httptest.NewRecorder()
	server.ServeHTTP(list, listReq)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"name":"chatgpt"`) || strings.Contains(list.Body.String(), created.Key.Secret) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/admin/keys/chatgpt", nil)
	revokeReq.Header.Set("x-agentbox-admin-key", "adm")
	revoke := httptest.NewRecorder()
	server.ServeHTTP(revoke, revokeReq)
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke status = %d body=%s", revoke.Code, revoke.Body.String())
	}

	afterRevoke := httptest.NewRecorder()
	server.ServeHTTP(afterRevoke, httptest.NewRequest(http.MethodGet, "/api/threads?key="+created.Key.Secret, nil))
	if afterRevoke.Code != http.StatusUnauthorized {
		t.Fatalf("after revoke status = %d body=%s", afterRevoke.Code, afterRevoke.Body.String())
	}
}

func TestAdminTenantProvisioningAuthorizationAndIdempotency(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	server := NewServer(config.Config{AdminKey: "adm"}, svc)

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/admin/tenants", strings.NewReader(`{"tenant_slug":"acme"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/tenants", strings.NewReader(`{
		"tenant_slug":"acme",
		"tenant_name":"Acme",
		"user_email":"admin@example.com",
		"user_name":"Acme Admin",
		"password":"secret-password",
		"create_key":true,
		"key_name":"workstation"
	}`))
	req.Header.Set("x-agentbox-admin-key", "adm")
	first := httptest.NewRecorder()
	server.ServeHTTP(first, req)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	var firstPayload struct {
		Tenant types.Tenant `json:"tenant"`
		User   types.User   `json:"user"`
		APIKey struct {
			Name   string `json:"name"`
			Secret string `json:"key"`
		} `json:"api_key"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstPayload); err != nil {
		t.Fatal(err)
	}
	if firstPayload.Tenant.ID != "ten_acme" || firstPayload.User.Email != "admin@example.com" || firstPayload.User.PasswordHash != nil || firstPayload.APIKey.Secret == "" {
		t.Fatalf("first payload = %#v", firstPayload)
	}
	if len(repo.Users) != 1 || repo.Users[0].PasswordHash == nil || *repo.Users[0].PasswordHash == "secret-password" {
		t.Fatalf("stored user = %#v", repo.Users)
	}

	normalKeyReq := httptest.NewRequest(http.MethodPost, "/api/admin/tenants", strings.NewReader(`{"tenant_slug":"other"}`))
	normalKeyReq.Header.Set("authorization", "Bearer "+firstPayload.APIKey.Secret)
	normalKeyAttempt := httptest.NewRecorder()
	server.ServeHTTP(normalKeyAttempt, normalKeyReq)
	if normalKeyAttempt.Code != http.StatusUnauthorized {
		t.Fatalf("normal key provisioning status=%d body=%s", normalKeyAttempt.Code, normalKeyAttempt.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/admin/tenants", strings.NewReader(`{
		"tenant_slug":"acme",
		"tenant_name":"Acme",
		"user_email":"admin@example.com",
		"user_name":"Acme Admin",
		"password":"secret-password",
		"create_key":true,
		"key_name":"workstation"
	}`))
	secondReq.Header.Set("x-agentbox-admin-key", "adm")
	second := httptest.NewRecorder()
	server.ServeHTTP(second, secondReq)
	if second.Code != http.StatusCreated {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	if len(repo.Tenants) != 1 || len(repo.Users) != 1 || len(repo.APIKeys) != 1 {
		t.Fatalf("repo counts tenants=%d users=%d keys=%d", len(repo.Tenants), len(repo.Users), len(repo.APIKeys))
	}
}

func TestAdminTenantUserAndKeyRoutes(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	server := NewServer(config.Config{AdminKey: "adm"}, svc)
	if _, err := svc.ProvisionTenant(t.Context(), service.ProvisionTenantParams{
		TenantSlug: "acme",
		TenantName: "Acme",
		UserEmail:  "admin@example.com",
		UserName:   "Acme Admin",
		Password:   "secret-password",
	}); err != nil {
		t.Fatal(err)
	}

	userReq := httptest.NewRequest(http.MethodPost, "/api/admin/tenants/acme/users", strings.NewReader(`{"email":"second@example.com","display_name":"Second Admin","role":"admin"}`))
	userReq.Header.Set("x-agentbox-admin-key", "adm")
	userRes := httptest.NewRecorder()
	server.ServeHTTP(userRes, userReq)
	if userRes.Code != http.StatusCreated || !strings.Contains(userRes.Body.String(), `"setup_token":"setup_`) {
		t.Fatalf("user status=%d body=%s", userRes.Code, userRes.Body.String())
	}

	keyReq := httptest.NewRequest(http.MethodPost, "/api/admin/tenants/acme/keys", strings.NewReader(`{"name":"raycast"}`))
	keyReq.Header.Set("x-agentbox-admin-key", "adm")
	keyRes := httptest.NewRecorder()
	server.ServeHTTP(keyRes, keyReq)
	if keyRes.Code != http.StatusCreated {
		t.Fatalf("key status=%d body=%s", keyRes.Code, keyRes.Body.String())
	}
	var keyPayload struct {
		Key struct {
			TenantID string `json:"tenant_id"`
			Name     string `json:"name"`
			Secret   string `json:"key"`
		} `json:"key"`
	}
	if err := json.Unmarshal(keyRes.Body.Bytes(), &keyPayload); err != nil {
		t.Fatal(err)
	}
	if keyPayload.Key.TenantID != "ten_acme" || keyPayload.Key.Name != "raycast" || keyPayload.Key.Secret == "" {
		t.Fatalf("key payload=%#v", keyPayload)
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

func TestDirectUploadIntentAndFinalize(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{PublicBaseURL: "https://assets.example.com"})
	if _, err := svc.CreateAPIKey(t.Context(), authContext(types.DefaultTenantID, "user"), "user"); err != nil {
		t.Fatal(err)
	}
	repo.APIKeys[0].Key = "user-key"
	repo.APIKeys[0].TokenHash = dbHashForTest("user-key")
	server := NewServer(config.Config{}, svc)

	create := httptest.NewRecorder()
	server.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/threads?key=user-key", strings.NewReader(`{"title":"Uploads"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	intent := httptest.NewRecorder()
	server.ServeHTTP(intent, httptest.NewRequest(http.MethodPost, "/api/threads/"+created.Thread.ID+"/uploads?key=user-key", strings.NewReader(`{"files":[{"file_name":"note.md","mime_type":"text/markdown","size_bytes":12}]}`)))
	if intent.Code != http.StatusCreated {
		t.Fatalf("intent status = %d body=%s", intent.Code, intent.Body.String())
	}
	var intentPayload struct {
		Uploads []struct {
			UploadID        string            `json:"upload_id"`
			UploadURL       string            `json:"upload_url"`
			StorageKey      string            `json:"storage_key"`
			RequiredHeaders map[string]string `json:"required_headers"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(intent.Body.Bytes(), &intentPayload); err != nil {
		t.Fatal(err)
	}
	if len(intentPayload.Uploads) != 1 || intentPayload.Uploads[0].UploadID == "" || intentPayload.Uploads[0].UploadURL == "" || intentPayload.Uploads[0].StorageKey == "" || intentPayload.Uploads[0].RequiredHeaders["content-type"] != "text/markdown" {
		t.Fatalf("intent payload = %#v", intentPayload)
	}
	if !strings.HasPrefix(intentPayload.Uploads[0].StorageKey, "agentbox/"+types.DefaultTenantID+"/"+created.Thread.ID+"/"+intentPayload.Uploads[0].UploadID+"/") {
		t.Fatalf("storage key = %q", intentPayload.Uploads[0].StorageKey)
	}

	postBody := `{"body":"attached","uploaded_assets":[{"upload_id":"` + intentPayload.Uploads[0].UploadID + `"}]}`
	post := httptest.NewRecorder()
	server.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/threads/"+created.Thread.ID+"/messages?key=user-key", strings.NewReader(postBody)))
	if post.Code != http.StatusCreated {
		t.Fatalf("post status = %d body=%s", post.Code, post.Body.String())
	}
	var posted struct {
		Message struct {
			Author string `json:"author"`
			Assets []struct {
				FileName  string  `json:"file_name"`
				PublicURL *string `json:"public_url"`
			} `json:"assets"`
		} `json:"message"`
	}
	if err := json.Unmarshal(post.Body.Bytes(), &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Message.Author != "user" || len(posted.Message.Assets) != 1 || posted.Message.Assets[0].FileName != "note.md" || posted.Message.Assets[0].PublicURL == nil {
		t.Fatalf("posted = %#v", posted)
	}
}

func TestHTTPTenantIsolationAndAuthMethods(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	keyA, err := svc.CreateAPIKey(t.Context(), authContext("ten_a", "tenant-a"), "shared")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := svc.CreateAPIKey(t.Context(), authContext("ten_b", "tenant-b"), "shared")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{}, svc)

	createA := httptest.NewRecorder()
	reqA := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(`{"title":"Tenant A"}`))
	reqA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(createA, reqA)
	if createA.Code != http.StatusCreated {
		t.Fatalf("createA status=%d body=%s", createA.Code, createA.Body.String())
	}
	var payloadA struct {
		Thread struct {
			ID       string `json:"id"`
			TenantID string `json:"tenant_id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(createA.Body.Bytes(), &payloadA); err != nil {
		t.Fatal(err)
	}

	createB := httptest.NewRecorder()
	server.ServeHTTP(createB, httptest.NewRequest(http.MethodPost, "/api/threads?key="+keyB.Key, strings.NewReader(`{"title":"Tenant B"}`)))
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
	reqUploadBWithA := httptest.NewRequest(http.MethodPost, "/api/threads/"+payloadB.Thread.ID+"/uploads", strings.NewReader(`{"files":[{"file_name":"blocked.txt","size_bytes":1}]}`))
	reqUploadBWithA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(uploadBWithA, reqUploadBWithA)
	if uploadBWithA.Code != http.StatusNotFound {
		t.Fatalf("uploadBWithA status=%d body=%s", uploadBWithA.Code, uploadBWithA.Body.String())
	}

	intentA := httptest.NewRecorder()
	reqIntentA := httptest.NewRequest(http.MethodPost, "/api/threads/"+payloadA.Thread.ID+"/uploads", strings.NewReader(`{"files":[{"file_name":"tenant-a.txt","size_bytes":1}]}`))
	reqIntentA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(intentA, reqIntentA)
	if intentA.Code != http.StatusCreated {
		t.Fatalf("intentA status=%d body=%s", intentA.Code, intentA.Body.String())
	}
	var intentAPayload struct {
		Uploads []struct {
			UploadID   string `json:"upload_id"`
			StorageKey string `json:"storage_key"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(intentA.Body.Bytes(), &intentAPayload); err != nil {
		t.Fatal(err)
	}
	if len(intentAPayload.Uploads) != 1 || !strings.HasPrefix(intentAPayload.Uploads[0].StorageKey, "agentbox/ten_a/"+payloadA.Thread.ID+"/"+intentAPayload.Uploads[0].UploadID+"/") {
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

	messageB := types.Message{ID: "msg_b", TenantID: "ten_b", ThreadID: payloadB.Thread.ID, Author: "tenant-b", Body: "asset", CreatedAt: "2026-07-07T00:00:00.000Z"}
	repo.Messages = append(repo.Messages, messageB)
	repo.Assets = append(repo.Assets, types.Asset{
		ID:         "asset_b",
		TenantID:   "ten_b",
		MessageID:  messageB.ID,
		StorageKey: "agentbox/ten_b/thread/file.txt",
		FileName:   "file.txt",
		SizeBytes:  1,
		CreatedAt:  messageB.CreatedAt,
		CreatedBy:  "tenant-b",
	})
	downloadBWithA := httptest.NewRecorder()
	reqDownloadBWithA := httptest.NewRequest(http.MethodGet, "/api/assets/asset_b/download-url", nil)
	reqDownloadBWithA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(downloadBWithA, reqDownloadBWithA)
	if downloadBWithA.Code != http.StatusNotFound {
		t.Fatalf("downloadBWithA status=%d body=%s", downloadBWithA.Code, downloadBWithA.Body.String())
	}

	messageA := types.Message{ID: "msg_a", TenantID: "ten_a", ThreadID: payloadA.Thread.ID, Author: "tenant-a", Body: "asset", CreatedAt: "2026-07-07T00:00:00.000Z"}
	repo.Messages = append(repo.Messages, messageA)
	repo.Assets = append(repo.Assets, types.Asset{
		ID:         "asset_legacy_a",
		TenantID:   "ten_a",
		MessageID:  messageA.ID,
		StorageKey: "agentbox/legacy-thread/message/legacy.txt",
		FileName:   "legacy.txt",
		SizeBytes:  1,
		CreatedAt:  messageA.CreatedAt,
		CreatedBy:  "tenant-a",
	})
	downloadLegacyA := httptest.NewRecorder()
	reqDownloadLegacyA := httptest.NewRequest(http.MethodGet, "/api/assets/asset_legacy_a/download-url", nil)
	reqDownloadLegacyA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(downloadLegacyA, reqDownloadLegacyA)
	if downloadLegacyA.Code != http.StatusOK {
		t.Fatalf("downloadLegacyA status=%d body=%s", downloadLegacyA.Code, downloadLegacyA.Body.String())
	}
	if !strings.Contains(downloadLegacyA.Body.String(), "agentbox/legacy-thread/message/legacy.txt") || strings.Contains(downloadLegacyA.Body.String(), "agentbox/ten_a/legacy-thread") {
		t.Fatalf("legacy download rewrote storage key: %s", downloadLegacyA.Body.String())
	}

	if err := svc.RevokeAPIKey(t.Context(), authContext("ten_a", "tenant-a"), "shared"); err != nil {
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

func authContext(tenantID string, actorName string) types.AuthContext {
	return types.AuthContext{
		TenantID:    tenantID,
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   actorName,
	}
}

func testUser(tenantID string, userID string, email string, displayName string, role string, passwordHash string) types.User {
	now := "2026-07-07T00:00:00.000Z"
	return types.User{
		ID:           userID,
		TenantID:     tenantID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func dbHashForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
