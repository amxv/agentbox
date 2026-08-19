package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

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
	store.HeadFailures = map[string]error{"assets\x00" + message.Assets[0].StorageKey: errors.New("thread detail must not inspect storage")}
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
	if memberView.Code != http.StatusOK || !strings.Contains(memberView.Body.String(), "secret.txt") || !strings.Contains(memberView.Body.String(), "download_path") || strings.Contains(memberView.Body.String(), "download_url") {
		t.Fatalf("member normal view status=%d body=%s", memberView.Code, memberView.Body.String())
	}
	removedViewer := request(http.MethodGet, "/api/viewer/threads/"+privateThread.ID, memberCookie, "", "")
	if removedViewer.Code != http.StatusNotFound {
		t.Fatalf("removed viewer route status=%d body=%s", removedViewer.Code, removedViewer.Body.String())
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
	if ownerDetail.Code != http.StatusOK || !strings.Contains(ownerDetail.Body.String(), message.ID) || !strings.Contains(ownerDetail.Body.String(), `"owner":{"id":"`+member.ID+`"`) || !strings.Contains(ownerDetail.Body.String(), "download_path") || strings.Contains(ownerDetail.Body.String(), "download_url") || strings.Contains(ownerDetail.Body.String(), "storage_key") {
		t.Fatalf("owner content detail status=%d body=%s", ownerDetail.Code, ownerDetail.Body.String())
	}
	delete(store.HeadFailures, "assets\x00"+message.Assets[0].StorageKey)
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
