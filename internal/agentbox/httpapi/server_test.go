package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func setThreadVisibilityForTest(ctx context.Context, repository interface {
	ManageThreadVisibility(context.Context, string, string, types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error)
}, userID string, threadID string, desiredTeamIDs []string) (types.ThreadVisibility, error) {
	current, err := repository.ManageThreadVisibility(ctx, userID, threadID, types.ManageThreadVisibilityInput{})
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	desired := map[string]bool{}
	for _, teamID := range desiredTeamIDs {
		desired[teamID] = true
	}
	currentIDs := map[string]bool{}
	input := types.ManageThreadVisibilityInput{}
	for _, team := range current.SharedTeams {
		currentIDs[team.ID] = true
		if !desired[team.ID] {
			input.RemoveTeams = append(input.RemoveTeams, team.ID)
		}
	}
	for _, teamID := range desiredTeamIDs {
		if !currentIDs[teamID] {
			input.AddTeams = append(input.AddTeams, teamID)
		}
	}
	state, err := repository.ManageThreadVisibility(ctx, userID, threadID, input)
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	return types.ThreadVisibility{ThreadID: state.ThreadID, OwnerUserID: state.OwnerUserID, SharedTeams: state.SharedTeams}, nil
}

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

func TestMaintenanceModeBlocksProductRoutesButAllowsCutoverPathsAndExplicitBypass(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	server := NewServer(config.Config{
		AdminKey:             "admin-secret",
		MaintenanceMode:      true,
		MaintenanceBypassKey: "bypass-secret",
		SessionCookieName:    config.DefaultSessionCookieName,
	}, svc)

	blocked := httptest.NewRecorder()
	server.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/api/threads", nil))
	if blocked.Code != http.StatusServiceUnavailable || !strings.Contains(blocked.Body.String(), `"code":"MAINTENANCE_MODE"`) {
		t.Fatalf("blocked status=%d body=%s", blocked.Code, blocked.Body.String())
	}

	health := httptest.NewRecorder()
	server.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", health.Code, health.Body.String())
	}

	setupToken := httptest.NewRecorder()
	setupRequest := httptest.NewRequest(http.MethodPost, "/api/admin/owner/setup-token", strings.NewReader(`{}`))
	setupRequest.Header.Set("x-agentbox-admin-key", "admin-secret")
	server.ServeHTTP(setupToken, setupRequest)
	if setupToken.Code != http.StatusCreated {
		t.Fatalf("setup-token status=%d body=%s", setupToken.Code, setupToken.Body.String())
	}

	bypassed := httptest.NewRecorder()
	bypassRequest := httptest.NewRequest(http.MethodGet, "/api/threads", nil)
	bypassRequest.Header.Set("x-agentbox-maintenance-key", "bypass-secret")
	server.ServeHTTP(bypassed, bypassRequest)
	if bypassed.Code != http.StatusUnauthorized {
		t.Fatalf("bypass should reach normal auth boundary: status=%d body=%s", bypassed.Code, bypassed.Body.String())
	}

	owner := types.User{ID: "usr_maintenance_owner", Email: "owner@example.com", DisplayName: "Owner", IsOwner: true}
	repo.Users = append(repo.Users, owner)
	ownerSecret := "maintenance-owner-session"
	if _, err := repo.CreateUserSession(t.Context(), types.UserSession{
		ID:         "sess_maintenance_owner",
		UserID:     owner.ID,
		SecretHash: dbHashForTest(ownerSecret),
	}); err != nil {
		t.Fatal(err)
	}
	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/owner/users", nil)
	ownerRequest.AddCookie(&http.Cookie{Name: config.DefaultSessionCookieName, Value: ownerSecret})
	ownerResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerResponse, ownerRequest)
	if ownerResponse.Code != http.StatusOK {
		t.Fatalf("owner browser should pass maintenance gate: status=%d body=%s", ownerResponse.Code, ownerResponse.Body.String())
	}

	ownerKey, err := svc.CreateAPIKey(t.Context(), types.AuthContext{
		UserID:          owner.ID,
		UserDisplayName: owner.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		SessionID:       "sess_maintenance_owner",
		ActorName:       "Web dashboard",
		IsOwner:         true,
	}, "owner-api")
	if err != nil {
		t.Fatal(err)
	}
	ownerKeyRequest := httptest.NewRequest(http.MethodGet, "/api/owner/users?key="+url.QueryEscape(ownerKey.Key), nil)
	ownerKeyResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerKeyResponse, ownerKeyRequest)
	if ownerKeyResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("owner API key bypassed maintenance: status=%d body=%s", ownerKeyResponse.Code, ownerKeyResponse.Body.String())
	}
}

func TestThreadRoutesAndMultipartAsset(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
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

func TestThreadViewRequiresNormalAuthenticationAndAddsPreviewURLs(t *testing.T) {
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
	if asset.DownloadURL == "" || asset.PreviewURL == nil || *asset.PreviewURL == asset.DownloadURL || !strings.Contains(*asset.PreviewURL, "inline") {
		t.Fatalf("viewer asset = %#v", asset)
	}
}

func TestBrowserSessionAuthLifecycleAndUserKeys(t *testing.T) {
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
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"user_display_name":"Alice Admin"`) || !strings.Contains(me.Body.String(), `"actor_name":"Web dashboard"`) {
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
	if created.Thread.CreatedBy != "Web dashboard" {
		t.Fatalf("created = %#v", created)
	}

	post := httptest.NewRecorder()
	reqPost := httptest.NewRequest(http.MethodPost, "/api/threads/"+created.Thread.ID+"/messages", strings.NewReader(`{"body":"from session"}`))
	reqPost.AddCookie(sessionCookie)
	server.ServeHTTP(post, reqPost)
	if post.Code != http.StatusCreated || !strings.Contains(post.Body.String(), `"author":"Web dashboard"`) || !strings.Contains(post.Body.String(), `"created_by_user_display_name":"Alice Admin"`) {
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

func TestAuthMeSupportsAPIKeyAndAliasWithoutLeakingSecret(t *testing.T) {
	repo := &db.MemoryRepository{
		Users: []types.User{{ID: "usr_acme", Email: "admin@example.com", DisplayName: "Acme Admin"}},
	}
	svc := service.New(repo, &assets.FakeStore{})
	key, err := svc.CreateAPIKeyWithScopes(t.Context(), types.AuthContext{
		SubjectType: types.AuthSubjectUserSession,
		ActorName:   "Acme Admin",
		UserID:      "usr_acme",
	}, "raycast", []string{"threads:read", "mcp:use"})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{}, svc)

	for _, path := range []string{"/api/auth/me", "/api/me"} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("authorization", "Bearer "+key.Key)
		server.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
		body := recorder.Body.String()
		if !strings.Contains(body, `"user_id":"usr_acme"`) ||
			!strings.Contains(body, `"user_display_name":"Acme Admin"`) ||
			!strings.Contains(body, `"subject_type":"api_key"`) ||
			!strings.Contains(body, `"actor_name":"raycast"`) ||
			!strings.Contains(body, `"key_id":"`) ||
			!strings.Contains(body, `"scopes":["threads:read","mcp:use"]`) {
			t.Fatalf("%s metadata body=%s", path, body)
		}
		if strings.Contains(body, `tenant_id`) || strings.Contains(body, `tenant_slug`) || strings.Contains(body, key.Key) || strings.Contains(body, key.TokenHash) {
			t.Fatalf("%s leaked secret material: %s", path, body)
		}
	}
}

func TestHTTPUserCredentialsAreIsolatedAndRotatable(t *testing.T) {
	passwordHash, err := authpkg.HashPassword("let-me-in")
	if err != nil {
		t.Fatal(err)
	}
	repo := &db.MemoryRepository{
		Users: []types.User{
			testUser("global", "usr_a", "a@example.com", "User A", "admin", passwordHash),
			testUser("global", "usr_b", "b@example.com", "User B", "member", passwordHash),
		},
	}
	repo.Users[0].IsOwner = true
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, service.New(repo, &assets.FakeStore{}))

	login := func(email string) *http.Cookie {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"`+email+`","password":"let-me-in"}`)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("login %s status=%d body=%s", email, recorder.Code, recorder.Body.String())
		}
		cookies := recorder.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("login %s cookies=%#v", email, cookies)
		}
		return cookies[0]
	}
	create := func(cookie *http.Cookie) types.APIKey {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"name":"chatgpt","purpose":"chatgpt"}`))
		request.AddCookie(cookie)
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create credential status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Key struct {
				ID      string `json:"id"`
				UserID  string `json:"user_id"`
				Name    string `json:"name"`
				Purpose string `json:"purpose"`
				Secret  string `json:"key"`
			} `json:"key"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return types.APIKey{ID: payload.Key.ID, UserID: payload.Key.UserID, Name: payload.Key.Name, Purpose: payload.Key.Purpose, Key: payload.Key.Secret}
	}
	list := func(cookie *http.Cookie) []types.APIKey {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
		request.AddCookie(cookie)
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list credentials status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Keys []types.APIKey `json:"keys"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Keys
	}

	cookieA := login("a@example.com")
	cookieB := login("b@example.com")
	firstA := create(cookieA)
	keyB := create(cookieB)
	rotatedA := create(cookieA)
	if firstA.ID != rotatedA.ID || firstA.Key == rotatedA.Key {
		t.Fatalf("rotation did not replace only the secret: first=%#v rotated=%#v", firstA, rotatedA)
	}
	if firstA.UserID != "usr_a" || rotatedA.UserID != "usr_a" || keyB.UserID != "usr_b" || keyB.ID == rotatedA.ID {
		t.Fatalf("credential ownership crossed users: first=%#v rotated=%#v b=%#v", firstA, rotatedA, keyB)
	}
	if rotatedA.Purpose != "chatgpt" || keyB.Purpose != "chatgpt" {
		t.Fatalf("credential purpose was not persisted: a=%#v b=%#v", rotatedA, keyB)
	}
	if keys := list(cookieA); len(keys) != 1 || keys[0].ID != rotatedA.ID || keys[0].UserID != "usr_a" {
		t.Fatalf("user A list crossed users: %#v", keys)
	}
	if keys := list(cookieB); len(keys) != 1 || keys[0].ID != keyB.ID || keys[0].UserID != "usr_b" {
		t.Fatalf("user B list crossed users: %#v", keys)
	}

	oldAuth := httptest.NewRecorder()
	oldRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	oldRequest.Header.Set("authorization", "Bearer "+firstA.Key)
	server.ServeHTTP(oldAuth, oldRequest)
	if oldAuth.Code != http.StatusUnauthorized {
		t.Fatalf("rotated secret still authenticated: status=%d body=%s", oldAuth.Code, oldAuth.Body.String())
	}
	ownerKeyAuth := httptest.NewRecorder()
	ownerKeyRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ownerKeyRequest.Header.Set("authorization", "Bearer "+rotatedA.Key)
	server.ServeHTTP(ownerKeyAuth, ownerKeyRequest)
	if ownerKeyAuth.Code != http.StatusOK || strings.Contains(ownerKeyAuth.Body.String(), `"is_owner":true`) {
		t.Fatalf("owner credential inherited browser-only owner authority: status=%d body=%s", ownerKeyAuth.Code, ownerKeyAuth.Body.String())
	}

	revokeA := httptest.NewRecorder()
	revokeARequest := httptest.NewRequest(http.MethodDelete, "/api/keys/chatgpt", nil)
	revokeARequest.AddCookie(cookieA)
	server.ServeHTTP(revokeA, revokeARequest)
	if revokeA.Code != http.StatusOK {
		t.Fatalf("revoke A status=%d body=%s", revokeA.Code, revokeA.Body.String())
	}
	bAuth := httptest.NewRecorder()
	bRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	bRequest.Header.Set("authorization", "Bearer "+keyB.Key)
	server.ServeHTTP(bAuth, bRequest)
	if bAuth.Code != http.StatusOK || !strings.Contains(bAuth.Body.String(), `"user_id":"usr_b"`) {
		t.Fatalf("revoking A affected B: status=%d body=%s", bAuth.Code, bAuth.Body.String())
	}
}

func TestCLIAuthAuthorizeAndExchange(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	passwordHash, err := authpkg.HashPassword("let-me-in")
	if err != nil {
		t.Fatal(err)
	}
	repo.Users = append(repo.Users, testUser("ten_acme", "usr_acme", "admin@example.com", "Acme Admin", "admin", passwordHash))
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, svc)

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"let-me-in"}`)))
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
		User types.User `json:"user"`
	}
	if err := json.Unmarshal(exchange.Body.Bytes(), &exchanged); err != nil {
		t.Fatal(err)
	}
	if exchanged.APIKey.Name != "cli-test" || exchanged.APIKey.Secret == "" || exchanged.User.ID != "usr_acme" {
		t.Fatalf("exchanged = %#v", exchanged)
	}
	if len(repo.APIKeys) != 1 || repo.APIKeys[0].UserID != "usr_acme" {
		t.Fatalf("repo API keys = %#v", repo.APIKeys)
	}

	reuse := httptest.NewRecorder()
	server.ServeHTTP(reuse, httptest.NewRequest(http.MethodPost, "/api/auth/cli/exchange", strings.NewReader(`{"code":"`+authorized.Code+`","state":"state","redirect_uri":"http://127.0.0.1:3456/callback","key_name":"cli-test"}`)))
	if reuse.Code != http.StatusForbidden {
		t.Fatalf("reuse status=%d body=%s", reuse.Code, reuse.Body.String())
	}
}

func TestOwnerSetupAndRecoveryRequireDeploymentSecretAndRejectReplay(t *testing.T) {
	repo := &db.MemoryRepository{}
	server := NewServer(config.Config{
		AdminKey:          "deployment-secret",
		AppPublicURL:      "https://agentbox.example",
		SessionCookieName: config.DefaultSessionCookieName,
	}, service.New(repo, &assets.FakeStore{}))

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/admin/owner/setup-token", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized issue status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	issue := func() map[string]any {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/admin/owner/setup-token", strings.NewReader(`{"expires_in_minutes":15}`))
		request.Header.Set("x-agentbox-admin-key", "deployment-secret")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("issue status=%d body=%s", response.Code, response.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	complete := func(token string, email string, displayName string, password string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]string{
			"token":        token,
			"email":        email,
			"display_name": displayName,
			"password":     password,
		})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/auth/owner/setup", bytes.NewReader(body)))
		return response
	}

	bootstrap := issue()
	bootstrapToken, _ := bootstrap["token"].(string)
	if bootstrap["purpose"] != "bootstrap" || !strings.HasPrefix(bootstrapToken, "agos_") || !strings.HasPrefix(bootstrap["setup_url"].(string), "https://agentbox.example/owner/setup?token=") {
		t.Fatalf("bootstrap payload=%#v", bootstrap)
	}
	completed := complete(bootstrapToken, "owner@example.com", "Owner", "initial-password")
	if completed.Code != http.StatusOK || !strings.Contains(completed.Body.String(), `"is_owner":true`) {
		t.Fatalf("complete status=%d body=%s", completed.Code, completed.Body.String())
	}
	cookies := completed.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == "" {
		t.Fatalf("owner session cookies=%#v", cookies)
	}
	replay := complete(bootstrapToken, "owner@example.com", "Owner", "initial-password")
	if replay.Code != http.StatusBadRequest || !strings.Contains(replay.Body.String(), "INVALID_OWNER_SETUP_TOKEN") {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	ownerIssueRequest := httptest.NewRequest(http.MethodPost, "/api/admin/owner/setup-token", nil)
	ownerIssueRequest.AddCookie(cookies[0])
	ownerIssue := httptest.NewRecorder()
	server.ServeHTTP(ownerIssue, ownerIssueRequest)
	if ownerIssue.Code != http.StatusUnauthorized {
		t.Fatalf("owner browser issued deployment token: status=%d body=%s", ownerIssue.Code, ownerIssue.Body.String())
	}

	createKeyRequest := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"name":"owner-api","purpose":"custom"}`))
	createKeyRequest.AddCookie(cookies[0])
	createKey := httptest.NewRecorder()
	server.ServeHTTP(createKey, createKeyRequest)
	if createKey.Code != http.StatusCreated {
		t.Fatalf("owner key status=%d body=%s", createKey.Code, createKey.Body.String())
	}
	var keyPayload struct {
		Key struct {
			Secret string `json:"key"`
		} `json:"key"`
	}
	if err := json.Unmarshal(createKey.Body.Bytes(), &keyPayload); err != nil {
		t.Fatal(err)
	}
	keyIssueRequest := httptest.NewRequest(http.MethodPost, "/api/admin/owner/setup-token", nil)
	keyIssueRequest.Header.Set("authorization", "Bearer "+keyPayload.Key.Secret)
	keyIssue := httptest.NewRecorder()
	server.ServeHTTP(keyIssue, keyIssueRequest)
	if keyIssue.Code != http.StatusUnauthorized {
		t.Fatalf("owner API key issued deployment token: status=%d body=%s", keyIssue.Code, keyIssue.Body.String())
	}

	recovery := issue()
	recoveryToken, _ := recovery["token"].(string)
	if recovery["purpose"] != "recovery" {
		t.Fatalf("recovery payload=%#v", recovery)
	}
	wrongEmail := complete(recoveryToken, "other@example.com", "Wrong", "recovered-password")
	if wrongEmail.Code != http.StatusConflict || !strings.Contains(wrongEmail.Body.String(), "OWNER_EMAIL_MISMATCH") {
		t.Fatalf("wrong email status=%d body=%s", wrongEmail.Code, wrongEmail.Body.String())
	}
	recovered := complete(recoveryToken, "OWNER@example.com", "Recovered Owner", "recovered-password")
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), "Recovered Owner") {
		t.Fatalf("recovery status=%d body=%s", recovered.Code, recovered.Body.String())
	}
}

func TestOwnerAttachmentPurgeHTTPIsBrowserOnlyAndRendersTombstones(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{DeleteFailures: map[string]error{}}
	owner := types.User{ID: "usr_http_purge_owner", Email: "http-purge-owner@example.com", DisplayName: "Owner", IsOwner: true}
	target := types.User{ID: "usr_http_purge_target", Email: "http-purge-target@example.com", DisplayName: "Target"}
	other := types.User{ID: "usr_http_purge_other", Email: "http-purge-other@example.com", DisplayName: "Other"}
	repo.Users = append(repo.Users, owner, target, other)
	ownerSecret := "owner-purge-session"
	otherSecret := "other-purge-session"
	for _, session := range []types.UserSession{
		{ID: "sess_http_purge_owner", UserID: owner.ID, SecretHash: dbHashForTest(ownerSecret)},
		{ID: "sess_http_purge_other", UserID: other.ID, SecretHash: dbHashForTest(otherSecret)},
	} {
		if _, err := repo.CreateUserSession(t.Context(), session); err != nil {
			t.Fatal(err)
		}
	}
	team, err := repo.CreateTeam(t.Context(), "http-purge-team", "HTTP Purge Team")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.ID, target.ID, other.ID} {
		if _, err := repo.AddTeamMember(t.Context(), team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	targetAuth := types.AuthContext{UserID: target.ID, UserDisplayName: target.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_target", ActorName: "Web dashboard"}
	thread, err := repo.CreateThread(t.Context(), target.ID, "HTTP purge thread", targetAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(t.Context(), repo, target.ID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	storageKey := "agentbox/http-purge/exact.bin"
	message, err := repo.PostMessage(t.Context(), target.ID, thread.ID, targetAuth, "attachment", nil, []types.NewAsset{{StorageKey: storageKey, FileName: "exact.bin", SizeBytes: 42}})
	if err != nil {
		t.Fatal(err)
	}
	assetID := message.Assets[0].ID
	publicToken := "agpub_http_purge"
	repo.ThreadPublicLinks = append(repo.ThreadPublicLinks, types.ThreadPublicLink{ThreadID: thread.ID, Token: publicToken, TokenHash: dbHashForTest(publicToken), TokenPrefix: "agpub_http_p", CreatedAt: thread.CreatedAt, UpdatedAt: thread.UpdatedAt})
	ownerKeySecret := "owner-purge-api-key"
	if _, err := repo.CreateAPIKey(t.Context(), owner.ID, "owner-purge-key", "custom", dbHashForTest(ownerKeySecret), "owner-purge", []string{"threads:read"}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, service.New(repo, store))
	ownerCookie := &http.Cookie{Name: config.DefaultSessionCookieName, Value: ownerSecret}
	otherCookie := &http.Cookie{Name: config.DefaultSessionCookieName, Value: otherSecret}

	requestPurge := func(cookie *http.Cookie, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/owner/users/"+target.ID+"/purge-attachments", strings.NewReader(`{"limit":10}`))
		request.Header.Set("content-type", "application/json")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if bearer != "" {
			request.Header.Set("authorization", "Bearer "+bearer)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	active := requestPurge(ownerCookie, "")
	if active.Code != http.StatusBadRequest || !strings.Contains(active.Body.String(), "USER_ACTIVE") {
		t.Fatalf("active purge status=%d body=%s", active.Code, active.Body.String())
	}
	ordinary := requestPurge(otherCookie, "")
	if ordinary.Code != http.StatusForbidden || !strings.Contains(ordinary.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("ordinary purge status=%d body=%s", ordinary.Code, ordinary.Body.String())
	}
	ownerKey := requestPurge(nil, ownerKeySecret)
	if ownerKey.Code != http.StatusForbidden || !strings.Contains(ownerKey.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner key purge status=%d body=%s", ownerKey.Code, ownerKey.Body.String())
	}
	if _, err := repo.SetUserDisabled(t.Context(), target.ID, true); err != nil {
		t.Fatal(err)
	}
	purged := requestPurge(ownerCookie, "")
	if purged.Code != http.StatusOK || !strings.Contains(purged.Body.String(), `"purged":1`) || !strings.Contains(purged.Body.String(), `"complete":true`) {
		t.Fatalf("purge status=%d body=%s", purged.Code, purged.Body.String())
	}
	if !reflect.DeepEqual(store.DeleteCalls, []string{storageKey}) {
		t.Fatalf("delete calls=%v", store.DeleteCalls)
	}
	repeated := requestPurge(ownerCookie, "")
	if repeated.Code != http.StatusOK || !strings.Contains(repeated.Body.String(), `"attempted":0`) || len(store.DeleteCalls) != 1 {
		t.Fatalf("repeated purge status=%d body=%s calls=%v", repeated.Code, repeated.Body.String(), store.DeleteCalls)
	}

	authenticatedThreadRequest := httptest.NewRequest(http.MethodGet, "/api/threads/"+thread.ID+"/view", nil)
	authenticatedThreadRequest.AddCookie(ownerCookie)
	authenticatedThread := httptest.NewRecorder()
	server.ServeHTTP(authenticatedThread, authenticatedThreadRequest)
	if authenticatedThread.Code != http.StatusOK || !strings.Contains(authenticatedThread.Body.String(), "exact.bin") || !strings.Contains(authenticatedThread.Body.String(), `"purged_at":`) || strings.Contains(authenticatedThread.Body.String(), "download_url") || strings.Contains(authenticatedThread.Body.String(), "preview_url") || strings.Contains(authenticatedThread.Body.String(), "storage_key") {
		t.Fatalf("authenticated tombstone status=%d body=%s", authenticatedThread.Code, authenticatedThread.Body.String())
	}
	authenticatedDownloadRequest := httptest.NewRequest(http.MethodGet, "/api/assets/"+assetID+"/download", nil)
	authenticatedDownloadRequest.AddCookie(ownerCookie)
	authenticatedDownload := httptest.NewRecorder()
	server.ServeHTTP(authenticatedDownload, authenticatedDownloadRequest)
	if authenticatedDownload.Code != http.StatusGone || !strings.Contains(authenticatedDownload.Body.String(), "ATTACHMENT_PURGED") {
		t.Fatalf("authenticated purged download status=%d body=%s", authenticatedDownload.Code, authenticatedDownload.Body.String())
	}
	publicThread := httptest.NewRecorder()
	server.ServeHTTP(publicThread, httptest.NewRequest(http.MethodGet, "/api/public/threads/"+publicToken, nil))
	if publicThread.Code != http.StatusOK || !strings.Contains(publicThread.Body.String(), "exact.bin") || !strings.Contains(publicThread.Body.String(), `"purged_at":`) || strings.Contains(publicThread.Body.String(), "download_path") {
		t.Fatalf("public tombstone status=%d body=%s", publicThread.Code, publicThread.Body.String())
	}
	publicDownload := httptest.NewRecorder()
	server.ServeHTTP(publicDownload, httptest.NewRequest(http.MethodGet, "/api/public/threads/"+publicToken+"/assets/"+assetID+"/download", nil))
	if publicDownload.Code != http.StatusGone || !strings.Contains(publicDownload.Body.String(), "ATTACHMENT_PURGED") {
		t.Fatalf("public purged download status=%d body=%s", publicDownload.Code, publicDownload.Body.String())
	}
}

func TestOwnerContentHTTPIsReadOnlyBrowserOnlyAndSeparateFromNormalAccess(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	owner := types.User{ID: "usr_http_content_owner", Email: "content-owner@example.com", DisplayName: "Content Owner", IsOwner: true}
	member := types.User{ID: "usr_http_content_member", Email: "content-member@example.com", DisplayName: "Content Member"}
	repo.Users = append(repo.Users, owner, member)
	ownerSecret := "owner-content-session-secret"
	memberSecret := "member-content-session-secret"
	for _, session := range []types.UserSession{
		{ID: "sess_http_content_owner", UserID: owner.ID, SecretHash: dbHashForTest(ownerSecret)},
		{ID: "sess_http_content_member", UserID: member.ID, SecretHash: dbHashForTest(memberSecret)},
	} {
		if _, err := repo.CreateUserSession(t.Context(), session); err != nil {
			t.Fatal(err)
		}
	}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_member", ActorName: "Web dashboard"}
	privateThread, err := repo.CreateThread(t.Context(), member.ID, "Private owner-view audit marker", memberAuth)
	if err != nil {
		t.Fatal(err)
	}
	message, err := repo.PostMessage(t.Context(), member.ID, privateThread.ID, memberAuth, "secret searchable message", nil, []types.NewAsset{{StorageKey: "agentbox/owner-view/secret.txt", FileName: "secret.txt", SizeBytes: 21}})
	if err != nil {
		t.Fatal(err)
	}
	store.PutAssetObject(message.Assets[0].StorageKey, 21, nil)
	ownerKeySecret := "owner-content-api-key"
	if _, err := repo.CreateAPIKey(t.Context(), owner.ID, "owner-content-key", "custom", dbHashForTest(ownerKeySecret), "owner-cont", []string{"threads:read", "assets:read"}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, service.New(repo, store))
	ownerCookie := &http.Cookie{Name: config.DefaultSessionCookieName, Value: ownerSecret}
	memberCookie := &http.Cookie{Name: config.DefaultSessionCookieName, Value: memberSecret}

	request := func(method string, path string, cookie *http.Cookie, bearer string, adminKey string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if bearer != "" {
			req.Header.Set("authorization", "Bearer "+bearer)
		}
		if adminKey != "" {
			req.Header.Set("x-agentbox-admin-key", adminKey)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}

	ownerNormal := request(http.MethodGet, "/api/threads/"+privateThread.ID, ownerCookie, "", "")
	if ownerNormal.Code != http.StatusNotFound {
		t.Fatalf("normal owner thread bypass status=%d body=%s", ownerNormal.Code, ownerNormal.Body.String())
	}
	ownerNormalView := request(http.MethodGet, "/api/threads/"+privateThread.ID+"/view", ownerCookie, "", "")
	if ownerNormalView.Code != http.StatusNotFound {
		t.Fatalf("normal owner view bypass status=%d body=%s", ownerNormalView.Code, ownerNormalView.Body.String())
	}
	ownerKeyNormal := request(http.MethodGet, "/api/threads/"+privateThread.ID+"/view", nil, ownerKeySecret, "")
	if ownerKeyNormal.Code != http.StatusNotFound {
		t.Fatalf("owner API key normal bypass status=%d body=%s", ownerKeyNormal.Code, ownerKeyNormal.Body.String())
	}
	memberView := request(http.MethodGet, "/api/threads/"+privateThread.ID+"/view", memberCookie, "", "")
	if memberView.Code != http.StatusOK || !strings.Contains(memberView.Body.String(), "secret.txt") || !strings.Contains(memberView.Body.String(), "download_url") {
		t.Fatalf("member normal view status=%d body=%s", memberView.Code, memberView.Body.String())
	}
	legacyViewer := request(http.MethodGet, "/api/viewer/threads/"+privateThread.ID, memberCookie, "", "")
	if legacyViewer.Code != http.StatusNotFound {
		t.Fatalf("legacy viewer route status=%d body=%s", legacyViewer.Code, legacyViewer.Body.String())
	}

	ownerList := request(http.MethodGet, "/api/owner/content/threads", ownerCookie, "", "")
	if ownerList.Code != http.StatusOK || !strings.Contains(ownerList.Body.String(), privateThread.ID) || !strings.Contains(ownerList.Body.String(), member.Email) || strings.Contains(ownerList.Body.String(), "password_hash") {
		t.Fatalf("owner content list status=%d body=%s", ownerList.Code, ownerList.Body.String())
	}
	ownerFiltered := request(http.MethodGet, "/api/owner/content/threads?user_id="+url.QueryEscape(member.ID), ownerCookie, "", "")
	if ownerFiltered.Code != http.StatusOK || !strings.Contains(ownerFiltered.Body.String(), privateThread.ID) {
		t.Fatalf("owner content filter status=%d body=%s", ownerFiltered.Code, ownerFiltered.Body.String())
	}
	ownerSearch := request(http.MethodGet, "/api/owner/content/search?query="+url.QueryEscape("searchable message"), ownerCookie, "", "")
	if ownerSearch.Code != http.StatusOK || !strings.Contains(ownerSearch.Body.String(), privateThread.ID) || !strings.Contains(ownerSearch.Body.String(), "searchable message") {
		t.Fatalf("owner content search status=%d body=%s", ownerSearch.Code, ownerSearch.Body.String())
	}
	ownerDetail := request(http.MethodGet, "/api/owner/content/threads/"+privateThread.ID, ownerCookie, "", "")
	if ownerDetail.Code != http.StatusOK || !strings.Contains(ownerDetail.Body.String(), message.ID) || !strings.Contains(ownerDetail.Body.String(), `"owner":{"id":"`+member.ID+`"`) || !strings.Contains(ownerDetail.Body.String(), "download_url") || strings.Contains(ownerDetail.Body.String(), "storage_key") {
		t.Fatalf("owner content detail status=%d body=%s", ownerDetail.Code, ownerDetail.Body.String())
	}
	ownerAsset := request(http.MethodGet, "/api/owner/content/assets/"+message.Assets[0].ID+"/download", ownerCookie, "", "")
	if ownerAsset.Code != http.StatusOK || !strings.Contains(ownerAsset.Body.String(), "download_url") {
		t.Fatalf("owner content asset status=%d body=%s", ownerAsset.Code, ownerAsset.Body.String())
	}
	readOnly := request(http.MethodPost, "/api/owner/content/threads", ownerCookie, "", "")
	if readOnly.Code != http.StatusMethodNotAllowed {
		t.Fatalf("owner content mutation method status=%d body=%s", readOnly.Code, readOnly.Body.String())
	}
	memberOwnerContent := request(http.MethodGet, "/api/owner/content/threads", memberCookie, "", "")
	if memberOwnerContent.Code != http.StatusForbidden || !strings.Contains(memberOwnerContent.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("member owner content status=%d body=%s", memberOwnerContent.Code, memberOwnerContent.Body.String())
	}
	ownerKeyContent := request(http.MethodGet, "/api/owner/content/threads", nil, ownerKeySecret, "")
	if ownerKeyContent.Code != http.StatusForbidden || !strings.Contains(ownerKeyContent.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner key content status=%d body=%s", ownerKeyContent.Code, ownerKeyContent.Body.String())
	}
	adminContent := request(http.MethodGet, "/api/owner/content/threads", nil, "", "deployment-secret")
	if adminContent.Code != http.StatusUnauthorized {
		t.Fatalf("deployment admin content status=%d body=%s", adminContent.Code, adminContent.Body.String())
	}
}

func TestOwnerInvitationAndUserLifecycleHTTPAuthorization(t *testing.T) {
	repo := &db.MemoryRepository{}
	server := NewServer(config.Config{
		AdminKey:          "deployment-secret",
		AppPublicURL:      "https://agentbox.example",
		SessionCookieName: config.DefaultSessionCookieName,
	}, service.New(repo, &assets.FakeStore{}))

	issueOwnerToken := httptest.NewRequest(http.MethodPost, "/api/admin/owner/setup-token", nil)
	issueOwnerToken.Header.Set("x-agentbox-admin-key", "deployment-secret")
	issueOwnerResponse := httptest.NewRecorder()
	server.ServeHTTP(issueOwnerResponse, issueOwnerToken)
	if issueOwnerResponse.Code != http.StatusCreated {
		t.Fatalf("issue owner token status=%d body=%s", issueOwnerResponse.Code, issueOwnerResponse.Body.String())
	}
	var ownerTokenPayload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(issueOwnerResponse.Body.Bytes(), &ownerTokenPayload); err != nil {
		t.Fatal(err)
	}
	ownerSetupBody, _ := json.Marshal(map[string]string{
		"token":        ownerTokenPayload.Token,
		"email":        "owner@example.com",
		"display_name": "Owner",
		"password":     "owner-password",
	})
	ownerSetupResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerSetupResponse, httptest.NewRequest(http.MethodPost, "/api/auth/owner/setup", bytes.NewReader(ownerSetupBody)))
	if ownerSetupResponse.Code != http.StatusOK {
		t.Fatalf("owner setup status=%d body=%s", ownerSetupResponse.Code, ownerSetupResponse.Body.String())
	}
	var ownerSetupPayload struct {
		Owner types.User `json:"owner"`
	}
	if err := json.Unmarshal(ownerSetupResponse.Body.Bytes(), &ownerSetupPayload); err != nil {
		t.Fatal(err)
	}
	ownerID := ownerSetupPayload.Owner.ID
	if ownerID == "" {
		t.Fatalf("owner setup payload=%s", ownerSetupResponse.Body.String())
	}
	ownerCookies := ownerSetupResponse.Result().Cookies()
	if len(ownerCookies) != 1 {
		t.Fatalf("owner cookies=%#v", ownerCookies)
	}
	ownerCookie := ownerCookies[0]

	deploymentSecretRequest := httptest.NewRequest(http.MethodGet, "/api/owner/users", nil)
	deploymentSecretRequest.Header.Set("x-agentbox-admin-key", "deployment-secret")
	deploymentSecretResponse := httptest.NewRecorder()
	server.ServeHTTP(deploymentSecretResponse, deploymentSecretRequest)
	if deploymentSecretResponse.Code != http.StatusUnauthorized {
		t.Fatalf("deployment secret accessed owner users: status=%d body=%s", deploymentSecretResponse.Code, deploymentSecretResponse.Body.String())
	}

	createTeam := func(slug string, name string) types.Team {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"slug": slug, "name": name})
		request := httptest.NewRequest(http.MethodPost, "/api/owner/teams", bytes.NewReader(body))
		request.AddCookie(ownerCookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create team %s status=%d body=%s", slug, response.Code, response.Body.String())
		}
		var payload struct {
			Team types.Team `json:"team"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Team
	}
	engineering := createTeam("engineering", "Engineering")
	operations := createTeam("operations", "Operations")

	invitationBody, _ := json.Marshal(map[string]any{
		"expires_in_minutes": 120,
		"team_ids":           []string{engineering.ID, operations.ID, engineering.ID},
	})
	createInvitationRequest := httptest.NewRequest(http.MethodPost, "/api/owner/invitations", bytes.NewReader(invitationBody))
	createInvitationRequest.AddCookie(ownerCookie)
	createInvitationResponse := httptest.NewRecorder()
	server.ServeHTTP(createInvitationResponse, createInvitationRequest)
	if createInvitationResponse.Code != http.StatusCreated {
		t.Fatalf("create invitation status=%d body=%s", createInvitationResponse.Code, createInvitationResponse.Body.String())
	}
	var invitationPayload struct {
		Invitation types.SignupInvitation `json:"invitation"`
		Token      string                 `json:"token"`
		SignupURL  string                 `json:"signup_url"`
	}
	if err := json.Unmarshal(createInvitationResponse.Body.Bytes(), &invitationPayload); err != nil {
		t.Fatal(err)
	}
	if invitationPayload.Invitation.ID == "" || !strings.HasPrefix(invitationPayload.Token, "aginv_") || !strings.HasPrefix(invitationPayload.SignupURL, "https://agentbox.example/signup?token=") || len(invitationPayload.Invitation.Teams) != 2 {
		t.Fatalf("invitation payload=%#v", invitationPayload)
	}

	inspectBody, _ := json.Marshal(map[string]string{"token": invitationPayload.Token})
	inspectResponse := httptest.NewRecorder()
	server.ServeHTTP(inspectResponse, httptest.NewRequest(http.MethodPost, "/api/auth/invitations/inspect", bytes.NewReader(inspectBody)))
	if inspectResponse.Code != http.StatusOK || !strings.Contains(inspectResponse.Body.String(), `"valid":true`) {
		t.Fatalf("inspect status=%d body=%s", inspectResponse.Code, inspectResponse.Body.String())
	}

	registerBody, _ := json.Marshal(map[string]string{
		"token":        invitationPayload.Token,
		"email":        "member@example.com",
		"display_name": "Member",
		"password":     "member-password",
	})
	registerResponse := httptest.NewRecorder()
	server.ServeHTTP(registerResponse, httptest.NewRequest(http.MethodPost, "/api/auth/invitations/register", bytes.NewReader(registerBody)))
	if registerResponse.Code != http.StatusCreated || !strings.Contains(registerResponse.Body.String(), `"redirect":"/onboarding"`) {
		t.Fatalf("register status=%d body=%s", registerResponse.Code, registerResponse.Body.String())
	}
	memberCookies := registerResponse.Result().Cookies()
	if len(memberCookies) != 1 {
		t.Fatalf("member cookies=%#v", memberCookies)
	}
	var registrationPayload struct {
		User types.User `json:"user"`
	}
	if err := json.Unmarshal(registerResponse.Body.Bytes(), &registrationPayload); err != nil {
		t.Fatal(err)
	}
	memberID := registrationPayload.User.ID
	if memberID == "" || registrationPayload.User.IsOwner {
		t.Fatalf("registered user=%#v", registrationPayload.User)
	}

	memberTeamsRequest := httptest.NewRequest(http.MethodGet, "/api/me/teams", nil)
	memberTeamsRequest.AddCookie(memberCookies[0])
	memberTeamsResponse := httptest.NewRecorder()
	server.ServeHTTP(memberTeamsResponse, memberTeamsRequest)
	if memberTeamsResponse.Code != http.StatusOK || !strings.Contains(memberTeamsResponse.Body.String(), engineering.ID) || !strings.Contains(memberTeamsResponse.Body.String(), operations.ID) {
		t.Fatalf("member team list status=%d body=%s", memberTeamsResponse.Code, memberTeamsResponse.Body.String())
	}

	ownerTeamsRequest := httptest.NewRequest(http.MethodGet, "/api/owner/teams", nil)
	ownerTeamsRequest.AddCookie(ownerCookie)
	ownerTeamsResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerTeamsResponse, ownerTeamsRequest)
	if ownerTeamsResponse.Code != http.StatusOK || !strings.Contains(ownerTeamsResponse.Body.String(), `"email":"member@example.com"`) {
		t.Fatalf("owner team list status=%d body=%s", ownerTeamsResponse.Code, ownerTeamsResponse.Body.String())
	}

	renameBody, _ := json.Marshal(map[string]string{"name": "Product Engineering"})
	renameRequest := httptest.NewRequest(http.MethodPatch, "/api/owner/teams/"+engineering.ID, bytes.NewReader(renameBody))
	renameRequest.AddCookie(ownerCookie)
	renameResponse := httptest.NewRecorder()
	server.ServeHTTP(renameResponse, renameRequest)
	if renameResponse.Code != http.StatusOK || !strings.Contains(renameResponse.Body.String(), `"slug":"engineering"`) || !strings.Contains(renameResponse.Body.String(), `"name":"Product Engineering"`) {
		t.Fatalf("rename team status=%d body=%s", renameResponse.Code, renameResponse.Body.String())
	}

	addOwnerRequest := httptest.NewRequest(http.MethodPut, "/api/owner/teams/"+engineering.ID+"/members/"+ownerID, nil)
	addOwnerRequest.AddCookie(ownerCookie)
	addOwnerResponse := httptest.NewRecorder()
	server.ServeHTTP(addOwnerResponse, addOwnerRequest)
	if addOwnerResponse.Code != http.StatusOK {
		t.Fatalf("add owner membership status=%d body=%s", addOwnerResponse.Code, addOwnerResponse.Body.String())
	}
	duplicateAddRequest := httptest.NewRequest(http.MethodPut, "/api/owner/teams/"+engineering.ID+"/members/"+ownerID, nil)
	duplicateAddRequest.AddCookie(ownerCookie)
	duplicateAddResponse := httptest.NewRecorder()
	server.ServeHTTP(duplicateAddResponse, duplicateAddRequest)
	if duplicateAddResponse.Code != http.StatusOK {
		t.Fatalf("duplicate add status=%d body=%s", duplicateAddResponse.Code, duplicateAddResponse.Body.String())
	}

	removeMemberRequest := httptest.NewRequest(http.MethodDelete, "/api/owner/teams/"+operations.ID+"/members/"+memberID, nil)
	removeMemberRequest.AddCookie(ownerCookie)
	removeMemberResponse := httptest.NewRecorder()
	server.ServeHTTP(removeMemberResponse, removeMemberRequest)
	if removeMemberResponse.Code != http.StatusOK {
		t.Fatalf("remove membership status=%d body=%s", removeMemberResponse.Code, removeMemberResponse.Body.String())
	}
	duplicateRemoveRequest := httptest.NewRequest(http.MethodDelete, "/api/owner/teams/"+operations.ID+"/members/"+memberID, nil)
	duplicateRemoveRequest.AddCookie(ownerCookie)
	duplicateRemoveResponse := httptest.NewRecorder()
	server.ServeHTTP(duplicateRemoveResponse, duplicateRemoveRequest)
	if duplicateRemoveResponse.Code != http.StatusOK {
		t.Fatalf("duplicate remove status=%d body=%s", duplicateRemoveResponse.Code, duplicateRemoveResponse.Body.String())
	}
	memberTeamsAfterRemovalRequest := httptest.NewRequest(http.MethodGet, "/api/me/teams", nil)
	memberTeamsAfterRemovalRequest.AddCookie(memberCookies[0])
	memberTeamsAfterRemovalResponse := httptest.NewRecorder()
	server.ServeHTTP(memberTeamsAfterRemovalResponse, memberTeamsAfterRemovalRequest)
	if memberTeamsAfterRemovalResponse.Code != http.StatusOK || !strings.Contains(memberTeamsAfterRemovalResponse.Body.String(), engineering.ID) || strings.Contains(memberTeamsAfterRemovalResponse.Body.String(), operations.ID) {
		t.Fatalf("member teams after removal status=%d body=%s", memberTeamsAfterRemovalResponse.Code, memberTeamsAfterRemovalResponse.Body.String())
	}

	memberOwnerRequest := httptest.NewRequest(http.MethodGet, "/api/owner/users", nil)
	memberOwnerRequest.AddCookie(memberCookies[0])
	memberOwnerResponse := httptest.NewRecorder()
	server.ServeHTTP(memberOwnerResponse, memberOwnerRequest)
	if memberOwnerResponse.Code != http.StatusForbidden || !strings.Contains(memberOwnerResponse.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("member owner access status=%d body=%s", memberOwnerResponse.Code, memberOwnerResponse.Body.String())
	}

	createOwnerKeyRequest := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"name":"owner-api"}`))
	createOwnerKeyRequest.AddCookie(ownerCookie)
	createOwnerKeyResponse := httptest.NewRecorder()
	server.ServeHTTP(createOwnerKeyResponse, createOwnerKeyRequest)
	if createOwnerKeyResponse.Code != http.StatusCreated {
		t.Fatalf("create owner key status=%d body=%s", createOwnerKeyResponse.Code, createOwnerKeyResponse.Body.String())
	}
	var ownerKeyPayload struct {
		Key struct {
			Secret string `json:"key"`
		} `json:"key"`
	}
	if err := json.Unmarshal(createOwnerKeyResponse.Body.Bytes(), &ownerKeyPayload); err != nil {
		t.Fatal(err)
	}
	ownerKeyRequest := httptest.NewRequest(http.MethodGet, "/api/owner/users", nil)
	ownerKeyRequest.Header.Set("authorization", "Bearer "+ownerKeyPayload.Key.Secret)
	ownerKeyResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerKeyResponse, ownerKeyRequest)
	if ownerKeyResponse.Code != http.StatusForbidden || !strings.Contains(ownerKeyResponse.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner key owner access status=%d body=%s", ownerKeyResponse.Code, ownerKeyResponse.Body.String())
	}
	ownerKeyTeamRequest := httptest.NewRequest(http.MethodPost, "/api/owner/teams", strings.NewReader(`{"slug":"blocked","name":"Blocked"}`))
	ownerKeyTeamRequest.Header.Set("authorization", "Bearer "+ownerKeyPayload.Key.Secret)
	ownerKeyTeamResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerKeyTeamResponse, ownerKeyTeamRequest)
	if ownerKeyTeamResponse.Code != http.StatusForbidden || !strings.Contains(ownerKeyTeamResponse.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner key mutated teams status=%d body=%s", ownerKeyTeamResponse.Code, ownerKeyTeamResponse.Body.String())
	}
	ownerKeyOwnTeamsRequest := httptest.NewRequest(http.MethodGet, "/api/me/teams", nil)
	ownerKeyOwnTeamsRequest.Header.Set("authorization", "Bearer "+ownerKeyPayload.Key.Secret)
	ownerKeyOwnTeamsResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerKeyOwnTeamsResponse, ownerKeyOwnTeamsRequest)
	if ownerKeyOwnTeamsResponse.Code != http.StatusOK || !strings.Contains(ownerKeyOwnTeamsResponse.Body.String(), engineering.ID) {
		t.Fatalf("owner key own-team list status=%d body=%s", ownerKeyOwnTeamsResponse.Code, ownerKeyOwnTeamsResponse.Body.String())
	}

	createMemberCredentialRequest := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"name":"member-chatgpt","purpose":"chatgpt"}`))
	createMemberCredentialRequest.AddCookie(memberCookies[0])
	createMemberCredentialResponse := httptest.NewRecorder()
	server.ServeHTTP(createMemberCredentialResponse, createMemberCredentialRequest)
	if createMemberCredentialResponse.Code != http.StatusCreated {
		t.Fatalf("create member credential status=%d body=%s", createMemberCredentialResponse.Code, createMemberCredentialResponse.Body.String())
	}
	var memberCredentialPayload struct {
		Key struct {
			ID     string `json:"id"`
			Secret string `json:"key"`
		} `json:"key"`
	}
	if err := json.Unmarshal(createMemberCredentialResponse.Body.Bytes(), &memberCredentialPayload); err != nil {
		t.Fatal(err)
	}
	if memberCredentialPayload.Key.ID == "" || memberCredentialPayload.Key.Secret == "" {
		t.Fatalf("member credential payload=%#v", memberCredentialPayload)
	}

	ownerCredentialsRequest := httptest.NewRequest(http.MethodGet, "/api/owner/credentials", nil)
	ownerCredentialsRequest.AddCookie(ownerCookie)
	ownerCredentialsResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerCredentialsResponse, ownerCredentialsRequest)
	if ownerCredentialsResponse.Code != http.StatusOK || !strings.Contains(ownerCredentialsResponse.Body.String(), memberCredentialPayload.Key.ID) || !strings.Contains(ownerCredentialsResponse.Body.String(), `"purpose":"chatgpt"`) || !strings.Contains(ownerCredentialsResponse.Body.String(), `"token_prefix":`) || strings.Contains(ownerCredentialsResponse.Body.String(), memberCredentialPayload.Key.Secret) || strings.Contains(ownerCredentialsResponse.Body.String(), "token_hash") {
		t.Fatalf("owner credential metadata status=%d body=%s", ownerCredentialsResponse.Code, ownerCredentialsResponse.Body.String())
	}
	memberCredentialsRequest := httptest.NewRequest(http.MethodGet, "/api/owner/credentials", nil)
	memberCredentialsRequest.AddCookie(memberCookies[0])
	memberCredentialsResponse := httptest.NewRecorder()
	server.ServeHTTP(memberCredentialsResponse, memberCredentialsRequest)
	if memberCredentialsResponse.Code != http.StatusForbidden || !strings.Contains(memberCredentialsResponse.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("member listed owner credentials status=%d body=%s", memberCredentialsResponse.Code, memberCredentialsResponse.Body.String())
	}
	ownerKeyCredentialsRequest := httptest.NewRequest(http.MethodGet, "/api/owner/credentials", nil)
	ownerKeyCredentialsRequest.Header.Set("authorization", "Bearer "+ownerKeyPayload.Key.Secret)
	ownerKeyCredentialsResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerKeyCredentialsResponse, ownerKeyCredentialsRequest)
	if ownerKeyCredentialsResponse.Code != http.StatusForbidden || !strings.Contains(ownerKeyCredentialsResponse.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner key listed owner credentials status=%d body=%s", ownerKeyCredentialsResponse.Code, ownerKeyCredentialsResponse.Body.String())
	}
	deploymentCredentialsRequest := httptest.NewRequest(http.MethodGet, "/api/owner/credentials", nil)
	deploymentCredentialsRequest.Header.Set("x-agentbox-admin-key", "deployment-secret")
	deploymentCredentialsResponse := httptest.NewRecorder()
	server.ServeHTTP(deploymentCredentialsResponse, deploymentCredentialsRequest)
	if deploymentCredentialsResponse.Code != http.StatusUnauthorized {
		t.Fatalf("deployment secret listed owner credentials status=%d body=%s", deploymentCredentialsResponse.Code, deploymentCredentialsResponse.Body.String())
	}
	revokeMemberCredential := func() {
		t.Helper()
		request := httptest.NewRequest(http.MethodDelete, "/api/owner/credentials/"+memberCredentialPayload.Key.ID, nil)
		request.AddCookie(ownerCookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), memberCredentialPayload.Key.ID) {
			t.Fatalf("owner revoke credential status=%d body=%s", response.Code, response.Body.String())
		}
	}
	revokeMemberCredential()
	revokeMemberCredential()
	revokedCredentialRequest := httptest.NewRequest(http.MethodGet, "/api/threads", nil)
	revokedCredentialRequest.Header.Set("authorization", "Bearer "+memberCredentialPayload.Key.Secret)
	revokedCredentialResponse := httptest.NewRecorder()
	server.ServeHTTP(revokedCredentialResponse, revokedCredentialRequest)
	if revokedCredentialResponse.Code != http.StatusUnauthorized {
		t.Fatalf("owner-revoked credential authenticated status=%d body=%s", revokedCredentialResponse.Code, revokedCredentialResponse.Body.String())
	}
	ownerCredentialsAfterRevokeRequest := httptest.NewRequest(http.MethodGet, "/api/owner/credentials", nil)
	ownerCredentialsAfterRevokeRequest.AddCookie(ownerCookie)
	ownerCredentialsAfterRevokeResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerCredentialsAfterRevokeResponse, ownerCredentialsAfterRevokeRequest)
	if ownerCredentialsAfterRevokeResponse.Code != http.StatusOK || !strings.Contains(ownerCredentialsAfterRevokeResponse.Body.String(), `"revoked_at":`) {
		t.Fatalf("revoked metadata status=%d body=%s", ownerCredentialsAfterRevokeResponse.Code, ownerCredentialsAfterRevokeResponse.Body.String())
	}

	listUsersRequest := httptest.NewRequest(http.MethodGet, "/api/owner/users", nil)
	listUsersRequest.AddCookie(ownerCookie)
	listUsersResponse := httptest.NewRecorder()
	server.ServeHTTP(listUsersResponse, listUsersRequest)
	if listUsersResponse.Code != http.StatusOK || !strings.Contains(listUsersResponse.Body.String(), `"email":"member@example.com"`) {
		t.Fatalf("list users status=%d body=%s", listUsersResponse.Code, listUsersResponse.Body.String())
	}

	disableRequest := httptest.NewRequest(http.MethodPost, "/api/owner/users/"+memberID+"/disable", nil)
	disableRequest.AddCookie(ownerCookie)
	disableResponse := httptest.NewRecorder()
	server.ServeHTTP(disableResponse, disableRequest)
	if disableResponse.Code != http.StatusOK || !strings.Contains(disableResponse.Body.String(), `"disabled_at":`) {
		t.Fatalf("disable status=%d body=%s", disableResponse.Code, disableResponse.Body.String())
	}
	if memberTeams, err := repo.ListUserTeams(t.Context(), memberID); err != nil || len(memberTeams) != 0 {
		t.Fatalf("disable retained team memberships=%#v err=%v", memberTeams, err)
	}
	memberMeRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	memberMeRequest.AddCookie(memberCookies[0])
	memberMeResponse := httptest.NewRecorder()
	server.ServeHTTP(memberMeResponse, memberMeRequest)
	if memberMeResponse.Code != http.StatusUnauthorized {
		t.Fatalf("disabled member session status=%d body=%s", memberMeResponse.Code, memberMeResponse.Body.String())
	}

	enableRequest := httptest.NewRequest(http.MethodPost, "/api/owner/users/"+memberID+"/enable", nil)
	enableRequest.AddCookie(ownerCookie)
	enableResponse := httptest.NewRecorder()
	server.ServeHTTP(enableResponse, enableRequest)
	if enableResponse.Code != http.StatusOK || strings.Contains(enableResponse.Body.String(), `"disabled_at":`) {
		t.Fatalf("enable status=%d body=%s", enableResponse.Code, enableResponse.Body.String())
	}
	if memberTeams, err := repo.ListUserTeams(t.Context(), memberID); err != nil || len(memberTeams) != 0 {
		t.Fatalf("enable restored team memberships=%#v err=%v", memberTeams, err)
	}

	unusedRequest := httptest.NewRequest(http.MethodPost, "/api/owner/invitations", nil)
	unusedRequest.AddCookie(ownerCookie)
	unusedResponse := httptest.NewRecorder()
	server.ServeHTTP(unusedResponse, unusedRequest)
	if unusedResponse.Code != http.StatusCreated {
		t.Fatalf("unused invitation status=%d body=%s", unusedResponse.Code, unusedResponse.Body.String())
	}
	var unusedPayload struct {
		Invitation types.SignupInvitation `json:"invitation"`
		Token      string                 `json:"token"`
	}
	if err := json.Unmarshal(unusedResponse.Body.Bytes(), &unusedPayload); err != nil {
		t.Fatal(err)
	}
	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/owner/invitations/"+unusedPayload.Invitation.ID, nil)
	revokeRequest.AddCookie(ownerCookie)
	revokeResponse := httptest.NewRecorder()
	server.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}
	revokedInspectBody, _ := json.Marshal(map[string]string{"token": unusedPayload.Token})
	revokedInspectResponse := httptest.NewRecorder()
	server.ServeHTTP(revokedInspectResponse, httptest.NewRequest(http.MethodPost, "/api/auth/invitations/inspect", bytes.NewReader(revokedInspectBody)))
	if revokedInspectResponse.Code != http.StatusBadRequest || !strings.Contains(revokedInspectResponse.Body.String(), "INVALID_INVITATION") {
		t.Fatalf("revoked inspect status=%d body=%s", revokedInspectResponse.Code, revokedInspectResponse.Body.String())
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
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	if _, err := svc.CreateAPIKey(t.Context(), authContext("global", "user"), "user"); err != nil {
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
	if !strings.HasPrefix(intentPayload.Uploads[0].StorageKey, "agentbox/usr_user/"+created.Thread.ID+"/"+intentPayload.Uploads[0].UploadID+"/") {
		t.Fatalf("storage key = %q", intentPayload.Uploads[0].StorageKey)
	}
	contentType := "text/markdown"
	store.PutAssetObject(intentPayload.Uploads[0].StorageKey, 12, &contentType)

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
	if posted.Message.Author != "user" || len(posted.Message.Assets) != 1 || posted.Message.Assets[0].FileName != "note.md" {
		t.Fatalf("posted = %#v", posted)
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
	reqUploadBWithA := httptest.NewRequest(http.MethodPost, "/api/threads/"+payloadB.Thread.ID+"/uploads", strings.NewReader(`{"files":[{"file_name":"blocked.txt","size_bytes":1}]}`))
	reqUploadBWithA.Header.Set("authorization", "Bearer "+keyA.Key)
	server.ServeHTTP(uploadBWithA, reqUploadBWithA)
	if uploadBWithA.Code != http.StatusNotFound {
		t.Fatalf("uploadBWithA status=%d body=%s", uploadBWithA.Code, uploadBWithA.Body.String())
	}

	intentA := httptest.NewRecorder()
	reqIntentA := httptest.NewRequest(http.MethodPost, "/api/threads/"+payloadA.Thread.ID+"/uploads", strings.NewReader(`{"files":[{"file_name":"user-a.txt","size_bytes":1}]}`))
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
	if len(intentAPayload.Uploads) != 1 || !strings.HasPrefix(intentAPayload.Uploads[0].StorageKey, "agentbox/"+authA.UserID+"/"+payloadA.Thread.ID+"/"+intentAPayload.Uploads[0].UploadID+"/") {
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

	if err := svc.RevokeAPIKey(t.Context(), authA, "shared"); err != nil {
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

	uploadIntent := request(http.MethodPost, "/api/threads/"+thread.ID+"/uploads", keyB.Key, `{"files":[{"file_name":"team-note.txt","mime_type":"text/plain","size_bytes":4}]}`)
	if uploadIntent.Code != http.StatusCreated {
		t.Fatalf("team upload intent status=%d body=%s", uploadIntent.Code, uploadIntent.Body.String())
	}
	var uploadPayload struct {
		Uploads []struct {
			UploadID   string `json:"upload_id"`
			StorageKey string `json:"storage_key"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(uploadIntent.Body.Bytes(), &uploadPayload); err != nil {
		t.Fatal(err)
	}
	if len(uploadPayload.Uploads) != 1 || uploadPayload.Uploads[0].UploadID == "" || uploadPayload.Uploads[0].StorageKey == "" {
		t.Fatalf("team upload payload=%#v", uploadPayload)
	}
	teamContentType := "text/plain"
	store.PutAssetObject(uploadPayload.Uploads[0].StorageKey, 4, &teamContentType)
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
	if publicPreview.Code != http.StatusTemporaryRedirect || !strings.Contains(publicPreview.Header().Get("Location"), "inline") {
		t.Fatalf("public preview status=%d location=%q body=%s", publicPreview.Code, publicPreview.Header().Get("Location"), publicPreview.Body.String())
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
	if !strings.Contains(viewerScoped.Body.String(), `"download_url"`) || !strings.Contains(viewerScoped.Body.String(), "seed.txt") {
		t.Fatalf("scoped viewer missing signed asset data: %s", viewerScoped.Body.String())
	}
}

func TestHTTPOnboardingIsBrowserOnlyExplicitAndResumable(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	passwordHash, err := authpkg.HashPassword("let-me-in")
	if err != nil {
		t.Fatal(err)
	}
	repo.Users = append(repo.Users, testUser("global", "usr_onboarding", "onboarding@example.com", "Onboarding User", "member", passwordHash))
	server := NewServer(config.Config{
		SessionCookieName: config.DefaultSessionCookieName,
		AppPublicURL:      "https://agentbox.example",
	}, svc)

	login := httptest.NewRecorder()
	server.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"onboarding@example.com","password":"let-me-in"}`)))
	if login.Code != http.StatusOK || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login status=%d body=%s cookies=%#v", login.Code, login.Body.String(), login.Result().Cookies())
	}
	cookie := login.Result().Cookies()[0]

	getState := func() *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/onboarding", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	initial := getState()
	if initial.Code != http.StatusOK || !strings.Contains(initial.Body.String(), `"steps":[]`) || len(repo.APIKeys) != 0 {
		t.Fatalf("initial onboarding status=%d body=%s keys=%#v", initial.Code, initial.Body.String(), repo.APIKeys)
	}

	skipRequest := httptest.NewRequest(http.MethodPost, "/api/onboarding/skip", nil)
	skipRequest.AddCookie(cookie)
	skipResponse := httptest.NewRecorder()
	server.ServeHTTP(skipResponse, skipRequest)
	if skipResponse.Code != http.StatusOK || !strings.Contains(skipResponse.Body.String(), `"dismissed_at"`) {
		t.Fatalf("skip status=%d body=%s", skipResponse.Code, skipResponse.Body.String())
	}

	type connectionPayload struct {
		Connector  string `json:"connector"`
		Credential struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"credential"`
		Onboarding     types.OnboardingState       `json:"onboarding"`
		MCPURL         string                      `json:"mcp_url"`
		ProfileCommand string                      `json:"profile_command"`
		SetupPrompt    string                      `json:"setup_prompt"`
		RaycastSetup   *types.RaycastSetupMaterial `json:"raycast_setup"`
		Instructions   []string                    `json:"instructions"`
	}
	createConnector := func(connector string, rotate bool) (*httptest.ResponseRecorder, connectionPayload) {
		t.Helper()
		body, _ := json.Marshal(map[string]bool{"rotate": rotate})
		request := httptest.NewRequest(http.MethodPost, "/api/onboarding/connectors/"+connector, bytes.NewReader(body))
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		var payload connectionPayload
		if response.Code == http.StatusCreated {
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
		}
		return response, payload
	}

	chatResponse, chat := createConnector("chatgpt", false)
	if chatResponse.Code != http.StatusCreated || chat.Connector != "chatgpt" || chat.Credential.Name != "ChatGPT" || chat.Credential.Key == "" || !strings.Contains(chat.MCPURL, "https://agentbox.example/api/mcp?key=") || len(chat.Instructions) == 0 {
		t.Fatalf("chatgpt status=%d payload=%#v body=%s", chatResponse.Code, chat, chatResponse.Body.String())
	}
	duplicateResponse, _ := createConnector("chatgpt", false)
	if duplicateResponse.Code != http.StatusConflict || !strings.Contains(duplicateResponse.Body.String(), "ONBOARDING_CREDENTIAL_EXISTS") {
		t.Fatalf("duplicate status=%d body=%s", duplicateResponse.Code, duplicateResponse.Body.String())
	}
	rotatedResponse, rotated := createConnector("chatgpt", true)
	if rotatedResponse.Code != http.StatusCreated || rotated.Credential.ID != chat.Credential.ID || rotated.Credential.Key == chat.Credential.Key {
		t.Fatalf("rotation status=%d original=%#v rotated=%#v body=%s", rotatedResponse.Code, chat, rotated, rotatedResponse.Body.String())
	}

	claudeResponse, claude := createConnector("claude", false)
	if claudeResponse.Code != http.StatusCreated || claude.Credential.Name != "Claude" || claude.Credential.Key == rotated.Credential.Key || !strings.Contains(claude.MCPURL, "/api/mcp?key=") {
		t.Fatalf("claude status=%d payload=%#v body=%s", claudeResponse.Code, claude, claudeResponse.Body.String())
	}
	localResponse, local := createConnector("local", false)
	if localResponse.Code != http.StatusCreated || local.Credential.Name != "Local CLI" || !strings.Contains(local.ProfileCommand, "--user-id 'usr_onboarding'") || !strings.Contains(local.SetupPrompt, "npm install -g @amxv/agentbox") || !strings.Contains(local.SetupPrompt, "agentbox list") {
		t.Fatalf("local status=%d payload=%#v body=%s", localResponse.Code, local, localResponse.Body.String())
	}
	raycastResponse, raycast := createConnector("raycast", false)
	raycastStep := raycast.Onboarding.Steps[len(raycast.Onboarding.Steps)-1]
	if raycastResponse.Code != http.StatusCreated || raycast.Credential.Name != "Raycast" || raycastStep.Connector != "raycast" || raycastStep.Credential == nil || strings.Join(raycastStep.Credential.Scopes, ",") != "threads:read,threads:write,assets:read,assets:write" || raycast.RaycastSetup == nil || raycast.RaycastSetup.APIKey != raycast.Credential.Key || raycast.RaycastSetup.BaseURL != "https://agentbox.example" || len(raycast.RaycastSetup.InstallCommands) != 4 || raycast.RaycastSetup.Preferences[0].Name != "baseUrl" || raycast.RaycastSetup.Preferences[1].Name != "apiKey" {
		t.Fatalf("raycast status=%d payload=%#v body=%s", raycastResponse.Code, raycast, raycastResponse.Body.String())
	}

	persisted := getState()
	if persisted.Code != http.StatusOK || strings.Contains(persisted.Body.String(), rotated.Credential.Key) || strings.Contains(persisted.Body.String(), claude.Credential.Key) || strings.Contains(persisted.Body.String(), local.Credential.Key) || strings.Contains(persisted.Body.String(), raycast.Credential.Key) || !strings.Contains(persisted.Body.String(), `"name":"ChatGPT"`) || !strings.Contains(persisted.Body.String(), `"name":"Claude"`) || !strings.Contains(persisted.Body.String(), `"name":"Local CLI"`) || !strings.Contains(persisted.Body.String(), `"name":"Raycast"`) {
		t.Fatalf("persisted onboarding leaked or missed metadata: status=%d body=%s", persisted.Code, persisted.Body.String())
	}

	mcpRequest := httptest.NewRequest(http.MethodGet, "/api/mcp?key="+url.QueryEscape(raycast.Credential.Key), nil)
	mcpResponse := httptest.NewRecorder()
	server.ServeHTTP(mcpResponse, mcpRequest)
	if mcpResponse.Code != http.StatusForbidden || !strings.Contains(mcpResponse.Body.String(), "mcp:use scope is required") {
		t.Fatalf("Raycast credential reached MCP: status=%d body=%s", mcpResponse.Code, mcpResponse.Body.String())
	}

	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/owner/content/threads", nil)
	ownerRequest.Header.Set("authorization", "Bearer "+raycast.Credential.Key)
	ownerResponse := httptest.NewRecorder()
	server.ServeHTTP(ownerResponse, ownerRequest)
	if ownerResponse.Code != http.StatusForbidden || !strings.Contains(ownerResponse.Body.String(), "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("Raycast credential reached owner web content: status=%d body=%s", ownerResponse.Code, ownerResponse.Body.String())
	}

	apiRequest := httptest.NewRequest(http.MethodGet, "/api/onboarding", nil)
	apiRequest.Header.Set("authorization", "Bearer "+claude.Credential.Key)
	apiResponse := httptest.NewRecorder()
	server.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusForbidden || !strings.Contains(apiResponse.Body.String(), "BROWSER_SESSION_REQUIRED") {
		t.Fatalf("API credential accessed onboarding: status=%d body=%s", apiResponse.Code, apiResponse.Body.String())
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
	for _, suffix := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		if _, err := svc.CreateThread(t.Context(), browser, "cursor marker "+suffix); err != nil {
			t.Fatal(err)
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
		cursor := ""
		for pageNumber := 0; pageNumber < 10; pageNumber++ {
			page := requestPage(extraQuery, cursor)
			for _, thread := range page.Threads {
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

func authContext(_ string, actorName string) types.AuthContext {
	return types.AuthContext{
		UserID:      "usr_" + actorName,
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   actorName,
	}
}

func testUser(_ string, userID string, email string, displayName string, _ string, passwordHash string) types.User {
	now := "2026-07-07T00:00:00.000Z"
	return types.User{
		ID:           userID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func dbHashForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
