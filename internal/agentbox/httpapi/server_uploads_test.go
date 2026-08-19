package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

func TestUploadCleanupEndpointIsDeploymentSecretOnlyBoundedAndIdempotent(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	user := types.User{ID: "usr_http_upload_cleanup", Email: "cleanup@example.invalid", DisplayName: "Cleanup"}
	repo.Users = append(repo.Users, user)
	auth := types.AuthContext{
		UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession,
		SessionID: "sess_http_upload_cleanup", ActorName: "Web dashboard",
	}
	thread, err := svc.CreateThread(t.Context(), auth, "HTTP upload cleanup")
	if err != nil {
		t.Fatal(err)
	}
	contents := []byte("abandoned")
	sum := sha256.Sum256(contents)
	digest := hex.EncodeToString(sum[:])
	uploads, err := svc.CreatePresignedUploads(t.Context(), auth, thread.ID, []types.UploadIntentFile{{
		FileName: "abandoned.bin", SizeBytes: int64(len(contents)), SHA256: digest,
	}})
	if err != nil || len(uploads) != 1 {
		t.Fatalf("uploads=%#v err=%v", uploads, err)
	}
	store.PutAssetObjectWithSHA(uploads[0].StorageKey, int64(len(contents)), nil, digest)
	store.PutAssetObject("agentbox/unrelated/keep.bin", 99, nil)
	repo.UploadCleanup[0].NotBefore = time.Now().UTC().Add(-time.Minute)

	server := NewServer(config.Config{MaintenanceBypassKey: "operator-maintenance-secret"}, svc)

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/admin/uploads/cleanup?limit=1", nil))
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Body.String(), `"code":"UNAUTHORIZED"`) {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPost, "/api/admin/uploads/cleanup?limit=101", nil)
	invalidRequest.Header.Set("x-agentbox-maintenance-key", "operator-maintenance-secret")
	server.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"code":"INVALID_ARGUMENT"`) {
		t.Fatalf("invalid limit status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	cleaned := httptest.NewRecorder()
	cleanupRequest := httptest.NewRequest(http.MethodPost, "/api/admin/uploads/cleanup?limit=1", nil)
	cleanupRequest.Header.Set("x-agentbox-maintenance-key", "operator-maintenance-secret")
	server.ServeHTTP(cleaned, cleanupRequest)
	if cleaned.Code != http.StatusOK || !strings.Contains(cleaned.Body.String(), `"cleaned":1`) || !strings.Contains(cleaned.Body.String(), `"failed":0`) {
		t.Fatalf("cleanup status=%d body=%s", cleaned.Code, cleaned.Body.String())
	}
	if _, err := store.HeadAssetObject(t.Context(), uploads[0].StorageKey); !errors.Is(err, assets.ErrObjectNotFound) {
		t.Fatalf("cleanup did not remove exact staging key: %v", err)
	}
	if _, err := store.HeadAssetObject(t.Context(), "agentbox/unrelated/keep.bin"); err != nil {
		t.Fatalf("cleanup removed unrelated key: %v", err)
	}

	repeated := httptest.NewRecorder()
	repeatRequest := httptest.NewRequest(http.MethodPost, "/api/admin/uploads/cleanup?limit=1", nil)
	repeatRequest.Header.Set("x-agentbox-maintenance-key", "operator-maintenance-secret")
	server.ServeHTTP(repeated, repeatRequest)
	if repeated.Code != http.StatusOK || !strings.Contains(repeated.Body.String(), `"attempted":0`) {
		t.Fatalf("repeat cleanup status=%d body=%s", repeated.Code, repeated.Body.String())
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
	server.ServeHTTP(intent, httptest.NewRequest(http.MethodPost, "/api/threads/"+created.Thread.ID+"/uploads?key=user-key", strings.NewReader(`{"files":[{"file_name":"note.md","mime_type":"text/markdown","size_bytes":12,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)))
	if intent.Code != http.StatusCreated {
		t.Fatalf("intent status = %d body=%s", intent.Code, intent.Body.String())
	}
	var intentPayload struct {
		Uploads []struct {
			UploadID        string            `json:"upload_id"`
			UploadURL       string            `json:"upload_url"`
			RequiredHeaders map[string]string `json:"required_headers"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(intent.Body.Bytes(), &intentPayload); err != nil {
		t.Fatal(err)
	}
	if len(intentPayload.Uploads) != 1 || intentPayload.Uploads[0].UploadID == "" || intentPayload.Uploads[0].UploadURL == "" || intentPayload.Uploads[0].RequiredHeaders["content-type"] != "text/markdown" || strings.Contains(intent.Body.String(), "storage_key") {
		t.Fatalf("intent payload = %#v", intentPayload)
	}
	if len(repo.Pending) != 1 || repo.Pending[0].ID != intentPayload.Uploads[0].UploadID || !strings.HasPrefix(repo.Pending[0].StorageKey, "agentbox/staging/usr_user/"+created.Thread.ID+"/"+intentPayload.Uploads[0].UploadID+"/") {
		t.Fatalf("pending upload = %#v", repo.Pending)
	}
	contentType := "text/markdown"
	store.PutAssetObjectWithSHA(repo.Pending[0].StorageKey, 12, &contentType, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

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
