package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	authpkg "agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

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
	duplicateA := httptest.NewRecorder()
	duplicateARequest := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"name":"CHATGPT","purpose":"chatgpt"}`))
	duplicateARequest.AddCookie(cookieA)
	server.ServeHTTP(duplicateA, duplicateARequest)
	if duplicateA.Code != http.StatusConflict || !strings.Contains(duplicateA.Body.String(), `"code":"CREDENTIAL_LABEL_CONFLICT"`) {
		t.Fatalf("duplicate create status=%d body=%s", duplicateA.Code, duplicateA.Body.String())
	}
	beforeRotationAuth := httptest.NewRecorder()
	beforeRotationRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	beforeRotationRequest.Header.Set("authorization", "Bearer "+firstA.Key)
	server.ServeHTTP(beforeRotationAuth, beforeRotationRequest)
	if beforeRotationAuth.Code != http.StatusOK {
		t.Fatalf("duplicate create invalidated original secret: status=%d body=%s", beforeRotationAuth.Code, beforeRotationAuth.Body.String())
	}

	rotateA := httptest.NewRecorder()
	rotateARequest := httptest.NewRequest(http.MethodPatch, "/api/keys/"+firstA.ID, nil)
	rotateARequest.AddCookie(cookieA)
	server.ServeHTTP(rotateA, rotateARequest)
	if rotateA.Code != http.StatusOK {
		t.Fatalf("rotate credential status=%d body=%s", rotateA.Code, rotateA.Body.String())
	}
	var rotatedPayload struct {
		Credential struct {
			ID      string `json:"id"`
			UserID  string `json:"user_id"`
			Name    string `json:"name"`
			Purpose string `json:"purpose"`
			Secret  string `json:"key"`
		} `json:"credential"`
	}
	if err := json.Unmarshal(rotateA.Body.Bytes(), &rotatedPayload); err != nil {
		t.Fatal(err)
	}
	rotatedA := types.APIKey{ID: rotatedPayload.Credential.ID, UserID: rotatedPayload.Credential.UserID, Name: rotatedPayload.Credential.Name, Purpose: rotatedPayload.Credential.Purpose, Key: rotatedPayload.Credential.Secret}
	if firstA.ID != rotatedA.ID || firstA.Key == rotatedA.Key || rotatedA.Key == "" {
		t.Fatalf("stable-ID rotation did not replace only the secret: first=%#v rotated=%#v", firstA, rotatedA)
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
	revokeARequest := httptest.NewRequest(http.MethodDelete, "/api/keys/"+firstA.ID, nil)
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
