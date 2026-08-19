package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

type Server struct {
	cfg     config.Config
	service *service.Service
	mux     *http.ServeMux
}

func NewServer(cfg config.Config, svc *service.Service) *Server {
	server := &Server{
		cfg:     cfg,
		service: svc,
		mux:     http.NewServeMux(),
	}
	server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.MaintenanceMode && !maintenanceExemptPath(r.URL.Path) && !s.hasMaintenanceBypass(r) && !s.hasOwnerBrowserMaintenanceAccess(r) {
		writeCodedError(w, http.StatusServiceUnavailable, "MAINTENANCE_MODE", "AgentBox is temporarily unavailable for maintenance.")
		return
	}
	s.mux.ServeHTTP(w, r)
}

func maintenanceExemptPath(path string) bool {
	switch path {
	case "/api/health", "/api/admin/owner/setup-token", "/api/auth/owner/setup":
		return true
	default:
		return false
	}
}

func (s *Server) hasMaintenanceBypass(r *http.Request) bool {
	expected := strings.TrimSpace(s.cfg.MaintenanceBypassKey)
	provided := strings.TrimSpace(r.Header.Get("x-agentbox-maintenance-key"))
	if expected == "" || provided == "" || len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func (s *Server) hasOwnerBrowserMaintenanceAccess(r *http.Request) bool {
	secret := s.sessionSecretFromRequest(r)
	if secret == "" {
		return false
	}
	authContext, err := s.service.AuthenticateSession(r.Context(), secret)
	if err != nil || authContext == nil {
		return false
	}
	return authContext.SubjectType == types.AuthSubjectUserSession && authContext.IsOwner && strings.TrimSpace(authContext.SessionID) != ""
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.health)
	s.mux.HandleFunc("/api/auth/login", s.authLogin)
	s.mux.HandleFunc("/api/auth/owner/setup", s.authOwnerSetup)
	s.mux.HandleFunc("/api/auth/invitations/inspect", s.authInvitationInspect)
	s.mux.HandleFunc("/api/auth/invitations/register", s.authInvitationRegister)
	s.mux.HandleFunc("/api/auth/logout", s.authLogout)
	s.mux.HandleFunc("/api/auth/me", s.authMe)
	s.mux.HandleFunc("/api/me", s.authMe)
	s.mux.HandleFunc("/api/me/teams", s.myTeams)
	s.mux.HandleFunc("/api/auth/cli/authorize", s.authCLIAuthorize)
	s.mux.HandleFunc("/api/auth/cli/exchange", s.authCLIExchange)
	s.mux.HandleFunc("/api/admin/owner/setup-token", s.adminOwnerSetupToken)
	s.mux.HandleFunc("/api/admin/uploads/cleanup", s.adminUploadCleanup)
	s.mux.HandleFunc("/api/owner/invitations", s.ownerInvitations)
	s.mux.HandleFunc("/api/owner/invitations/", s.ownerInvitation)
	s.mux.HandleFunc("/api/owner/users", s.ownerUsers)
	s.mux.HandleFunc("/api/owner/users/", s.ownerUserAction)
	s.mux.HandleFunc("/api/owner/credentials", s.ownerCredentials)
	s.mux.HandleFunc("/api/owner/credentials/", s.ownerCredential)
	s.mux.HandleFunc("/api/owner/content/threads", s.ownerContentThreads)
	s.mux.HandleFunc("/api/owner/content/search", s.ownerContentSearch)
	s.mux.HandleFunc("/api/owner/content/threads/", s.ownerContentThread)
	s.mux.HandleFunc("/api/owner/content/assets/", s.ownerContentAsset)
	s.mux.HandleFunc("/api/owner/teams", s.ownerTeams)
	s.mux.HandleFunc("/api/owner/teams/", s.ownerTeam)
	s.mux.HandleFunc("/api/keys", s.keys)
	s.mux.HandleFunc("/api/keys/", s.key)
	s.mux.HandleFunc("/api/raycast-installations", s.raycastInstallations)
	s.mux.HandleFunc("/api/onboarding", s.onboarding)
	s.mux.HandleFunc("/api/onboarding/skip", s.onboardingSkip)
	s.mux.HandleFunc("/api/onboarding/connectors/", s.onboardingConnector)
	s.mux.HandleFunc("/api/threads", s.threads)
	s.mux.HandleFunc("/api/threads/", s.threadSubroutes)
	s.mux.HandleFunc("/api/messages/", s.messageSubroutes)
	s.mux.HandleFunc("/api/public/threads/", s.publicThreadSubroutes)
	s.mux.HandleFunc("/api/assets/", s.assetSubroutes)
	s.mux.Handle("/api/mcp", s.mcpHandler())
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "agentbox",
	})
}

func (s *Server) requestBaseURL(r *http.Request) string {
	if origin, err := s.cfg.TrustedAppPublicURL(); err == nil && origin != "" {
		return origin
	}
	if s.cfg.IsProduction() {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	// Development fallback deliberately ignores Forwarded and X-Forwarded-*
	// headers. Production requires AGENTBOX_APP_PUBLIC_URL, and the local
	// fallback may trust only the host observed by the Go server itself.
	fallback := config.Config{AppPublicURL: scheme + "://" + host}
	origin, err := fallback.TrustedAppPublicURL()
	if err != nil {
		return ""
	}
	return origin
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (*types.AuthContext, bool) {
	var authContext *types.AuthContext
	var err error
	if secret := authSecretFromRequest(r); secret != "" {
		authContext, err = s.service.AuthenticateAPIKey(r.Context(), secret)
	} else {
		authContext, err = s.service.AuthenticateSession(r.Context(), s.sessionSecretFromRequest(r))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if authContext == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return nil, false
	}
	return authContext, true
}

func (s *Server) requireOwnerBrowser(w http.ResponseWriter, r *http.Request) *types.AuthContext {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return nil
	}
	if authContext.SubjectType != types.AuthSubjectUserSession || !authContext.IsOwner {
		writeCodedError(w, http.StatusForbidden, "OWNER_BROWSER_REQUIRED", "Permanent owner browser session is required.")
		return nil
	}
	return authContext
}

func (s *Server) requireOwnerContentContext(w http.ResponseWriter, r *http.Request) (service.OwnerWebContext, bool) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return service.OwnerWebContext{}, false
	}
	ownerContext, err := s.service.ResolveOwnerWebContext(*authContext)
	if err != nil {
		writeServiceError(w, err)
		return service.OwnerWebContext{}, false
	}
	return ownerContext, true
}

func (s *Server) sessionCookieName() string {
	if strings.TrimSpace(s.cfg.SessionCookieName) != "" {
		return strings.TrimSpace(s.cfg.SessionCookieName)
	}
	return config.DefaultSessionCookieName
}

func (s *Server) sessionSecretFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(s.sessionCookieName())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, secret string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    secret,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if err := auth.RequireAdminRequest(r, s.cfg); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "Unauthorized" {
			status = http.StatusUnauthorized
		}
		writeError(w, status, err.Error())
		return false
	}
	return true
}

func authSecretFromRequest(r *http.Request) string {
	if bearer := strings.TrimSpace(r.Header.Get("authorization")); bearer != "" {
		if strings.HasPrefix(strings.ToLower(bearer), "bearer ") {
			if secret := strings.TrimSpace(bearer[len("Bearer "):]); secret != "" {
				return secret
			}
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("key"))
}

func apiKeyResponse(key types.APIKey) map[string]any {
	result := map[string]any{
		"id":           key.ID,
		"user_id":      key.UserID,
		"name":         key.Name,
		"purpose":      key.Purpose,
		"key_masked":   key.KeyMasked,
		"token_prefix": key.TokenPrefix,
		"scopes":       key.Scopes,
		"created_at":   key.CreatedAt,
		"updated_at":   key.UpdatedAt,
		"last_used_at": key.LastUsedAt,
		"revoked_at":   key.RevokedAt,
	}
	if key.Key != "" {
		result["key"] = key.Key
	}
	return result
}

func apiKeyResponses(keys []types.APIKey) []map[string]any {
	result := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		result = append(result, apiKeyResponse(key))
	}
	return result
}

func browserUser(authContext types.AuthContext) bool {
	return authContext.SubjectType == types.AuthSubjectUserSession && authContext.UserID != ""
}

func canManageKeys(authContext types.AuthContext) bool {
	return browserUser(authContext) || hasScope(authContext, "keys:write")
}

func canReadKeys(authContext types.AuthContext) bool {
	return canManageKeys(authContext) || hasScope(authContext, "keys:read")
}

func hasScope(authContext types.AuthContext, scope string) bool {
	for _, candidate := range authContext.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeCodedError(w, status, errorCodeForStatus(status), message)
}

func writeCodedError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{"error": message, "code": code})
}

func writeServiceError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "INVALID_ARGUMENT"
	message := err.Error()
	var coded service.CodedError
	if errors.As(err, &coded) {
		code = coded.Code
		message = coded.Message
		switch coded.Code {
		case "THREAD_NOT_FOUND", "MESSAGE_NOT_FOUND", "ATTACHMENT_NOT_FOUND", "TEAM_NOT_FOUND", "USER_NOT_FOUND", "PUBLIC_LINK_NOT_FOUND", "PUBLIC_ASSET_NOT_FOUND", "CREDENTIAL_NOT_FOUND":
			status = http.StatusNotFound
		case "PERMISSION_DENIED", "BROWSER_SESSION_REQUIRED", "OWNER_BROWSER_REQUIRED":
			status = http.StatusForbidden
		case "OWNER_EMAIL_MISMATCH", "ONBOARDING_CREDENTIAL_EXISTS", "PUBLIC_LINK_EXISTS", "CREDENTIAL_LABEL_CONFLICT", "UPLOAD_FINALIZING":
			status = http.StatusConflict
		case "ATTACHMENT_PURGED", "ATTACHMENT_UNAVAILABLE":
			status = http.StatusGone
		case "TEAM_SLUG_CONFLICT":
			status = http.StatusConflict
		case "RATE_LIMITED", "UPLOAD_QUOTA_EXCEEDED":
			status = http.StatusTooManyRequests
		case "INTERNAL_ERROR":
			status = http.StatusInternalServerError
		default:
			status = http.StatusBadRequest
		}
	} else if errors.Is(err, service.ErrThreadNotFound) {
		status = http.StatusNotFound
		code = "THREAD_NOT_FOUND"
		message = service.ErrThreadNotFound.Error()
	}
	writeJSON(w, status, map[string]any{"error": message, "code": code})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func errorCodeForStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "PERMISSION_DENIED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "THREAD_NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RATE_LIMITED"
	case http.StatusInternalServerError:
		return "INTERNAL_ERROR"
	default:
		return "INVALID_ARGUMENT"
	}
}

func parseJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1_048_576))
	if err := decoder.Decode(target); err != nil {
		return errors.New("Expected a JSON request body.")
	}
	return nil
}

func parseJSONStrict(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1_048_576))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("Expected a JSON request body with only supported fields.")
	}
	return nil
}

func method(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method == expected {
		return true
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
	return false
}

func ownerPageRequest(r *http.Request) (types.PageRequest, error) {
	request := types.PageRequest{Limit: numberQuery(r, "limit", types.DefaultOwnerPageLimit)}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if cursor != "" {
		offset, err := strconv.Atoi(cursor)
		if err != nil || offset < 0 {
			return types.PageRequest{}, errors.New("cursor must be a non-negative continuation offset")
		}
		request.Offset = offset
	}
	return types.NormalizePageRequest(request), nil
}

func numberQuery(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return int(value)
}

func optionalQuery(r *http.Request, name string) *string {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return nil
	}
	return &value
}

func splitFirst(value string) (string, string, bool) {
	value = strings.Trim(value, "/")
	if value == "" {
		return "", "", false
	}
	head, tail, found := strings.Cut(value, "/")
	if !found {
		return head, "", true
	}
	return head, strings.Trim(tail, "/"), true
}

type viewerThread struct {
	types.Thread
	Messages []viewerMessage `json:"messages"`
}

type ownerViewerThread struct {
	types.Thread
	Owner      types.User             `json:"owner"`
	Messages   []viewerMessage        `json:"messages"`
	Visibility types.ThreadVisibility `json:"visibility"`
}

type viewerMessage struct {
	types.Message
	Assets []viewerAsset `json:"assets"`
}

type viewerAsset struct {
	types.Asset
	DownloadPath string `json:"download_path,omitempty"`
	PreviewPath  string `json:"preview_path,omitempty"`
}

type uploadIntentResponse struct {
	UploadID        string            `json:"upload_id"`
	FileName        string            `json:"file_name"`
	MimeType        *string           `json:"mime_type"`
	SizeBytes       int64             `json:"size_bytes"`
	SHA256          string            `json:"sha256"`
	UploadURL       string            `json:"upload_url"`
	ExpiresIn       int               `json:"expires_in"`
	RequiredHeaders map[string]string `json:"required_headers"`
}

func safeUploadIntentResponses(uploads []types.PresignedUpload) []uploadIntentResponse {
	result := make([]uploadIntentResponse, 0, len(uploads))
	for _, upload := range uploads {
		result = append(result, uploadIntentResponse{
			UploadID:        upload.UploadID,
			FileName:        upload.FileName,
			MimeType:        upload.MimeType,
			SizeBytes:       upload.SizeBytes,
			SHA256:          upload.SHA256,
			UploadURL:       upload.UploadURL,
			ExpiresIn:       upload.ExpiresIn,
			RequiredHeaders: upload.RequiredHeaders,
		})
	}
	return result
}

func withViewerAssetPaths(thread *types.ThreadWithMessages) viewerThread {
	result := viewerThread{Thread: thread.Thread, Messages: []viewerMessage{}}
	for _, message := range thread.Messages {
		vm := viewerMessage{Message: message, Assets: []viewerAsset{}}
		for _, asset := range message.Assets {
			if asset.PurgedAt != nil {
				vm.Assets = append(vm.Assets, viewerAsset{Asset: asset})
				continue
			}
			basePath := "/api/assets/" + url.PathEscape(asset.ID)
			viewer := viewerAsset{Asset: asset, DownloadPath: basePath + "/download-url"}
			if isDashboardPreviewableAsset(asset) {
				viewer.PreviewPath = basePath + "/preview-url"
			}
			vm.Assets = append(vm.Assets, viewer)
		}
		result.Messages = append(result.Messages, vm)
	}
	return result
}

func withOwnerContentAssetPaths(thread *types.OwnerContentThreadDetail) ownerViewerThread {
	result := ownerViewerThread{Thread: thread.Thread, Owner: thread.Owner, Messages: []viewerMessage{}, Visibility: thread.Visibility}
	for _, message := range thread.Messages {
		vm := viewerMessage{Message: message, Assets: []viewerAsset{}}
		for _, asset := range message.Assets {
			if asset.PurgedAt != nil {
				vm.Assets = append(vm.Assets, viewerAsset{Asset: asset})
				continue
			}
			basePath := "/api/owner/content/assets/" + url.PathEscape(asset.ID)
			viewer := viewerAsset{Asset: asset, DownloadPath: basePath + "/download"}
			if asset.MimeType != nil && strings.HasPrefix(strings.ToLower(*asset.MimeType), "image/") {
				viewer.PreviewPath = basePath + "/preview"
			}
			vm.Assets = append(vm.Assets, viewer)
		}
		result.Messages = append(result.Messages, vm)
	}
	return result
}

func isDashboardPreviewableAsset(asset types.Asset) bool {
	if asset.MimeType != nil {
		mimeType := strings.ToLower(strings.TrimSpace(*asset.MimeType))
		if strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, messageformat.Markdown) {
			return true
		}
	}
	return messageformat.IsMarkdownPath(asset.FileName)
}

func writeAssetResolution(w http.ResponseWriter, payload map[string]any, urlField string, signedURL string, err error) {
	if err == nil {
		payload["available"] = true
		payload[urlField] = signedURL
		writeJSON(w, http.StatusOK, payload)
		return
	}
	var coded service.CodedError
	if errors.As(err, &coded) && coded.Code == "ATTACHMENT_UNAVAILABLE" {
		payload["available"] = false
		payload["unavailable_reason"] = coded.Message
		writeJSON(w, http.StatusOK, payload)
		return
	}
	writeServiceError(w, err)
}
