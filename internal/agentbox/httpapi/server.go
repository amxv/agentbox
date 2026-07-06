package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/mcpserver"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
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
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.health)
	s.mux.HandleFunc("/api/auth/login", s.authLogin)
	s.mux.HandleFunc("/api/auth/logout", s.authLogout)
	s.mux.HandleFunc("/api/auth/me", s.authMe)
	s.mux.HandleFunc("/api/auth/cli/authorize", s.authCLIAuthorize)
	s.mux.HandleFunc("/api/auth/cli/exchange", s.authCLIExchange)
	s.mux.HandleFunc("/api/admin/tenants", s.adminTenants)
	s.mux.HandleFunc("/api/admin/tenants/", s.adminTenantSubroutes)
	s.mux.HandleFunc("/api/admin/keys", s.adminKeys)
	s.mux.HandleFunc("/api/admin/keys/", s.adminKey)
	s.mux.HandleFunc("/api/keys", s.keys)
	s.mux.HandleFunc("/api/keys/", s.key)
	s.mux.HandleFunc("/api/threads", s.threads)
	s.mux.HandleFunc("/api/threads/", s.threadSubroutes)
	s.mux.HandleFunc("/api/assets/", s.assetSubroutes)
	s.mux.HandleFunc("/api/viewer/threads", s.viewerThreads)
	s.mux.HandleFunc("/api/viewer/threads/", s.viewerThread)
	s.mux.Handle("/api/mcp", s.mcpHandler())
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TenantID string `json:"tenant_id"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	authContext, secret, err := s.service.Login(r.Context(), input.TenantID, input.Email, input.Password)
	if err != nil {
		status := http.StatusInternalServerError
		message := err.Error()
		if errors.Is(err, service.ErrInvalidLogin) || strings.Contains(err.Error(), "Multiple users") {
			status = http.StatusUnauthorized
			message = service.ErrInvalidLogin.Error()
		}
		writeError(w, status, message)
		return
	}
	s.setSessionCookie(w, secret)
	writeJSON(w, http.StatusOK, map[string]any{"auth": authContext})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	if secret := s.sessionSecretFromRequest(r); secret != "" {
		if err := s.service.LogoutSession(r.Context(), secret); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auth": authContext})
}

func (s *Server) authCLIAuthorize(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var input struct {
		State       string `json:"state"`
		RedirectURI string `json:"redirect_uri"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.service.AuthorizeCLILogin(r.Context(), *authContext, input.State, input.RedirectURI)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":         result.Code,
		"redirect_uri": result.RedirectURI,
	})
}

func (s *Server) authCLIExchange(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Code        string `json:"code"`
		State       string `json:"state"`
		RedirectURI string `json:"redirect_uri"`
		KeyName     string `json:"key_name"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.service.ExchangeCLILogin(r.Context(), input.Code, input.State, input.RedirectURI, input.KeyName)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"api_key":   apiKeyResponse(result.APIKey),
		"key":       apiKeyResponse(result.APIKey),
		"tenant":    result.Tenant,
		"user":      result.User,
		"auth_type": result.AuthType,
	})
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

func (s *Server) threads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		authContext, ok := s.requireAuth(w, r)
		if !ok {
			return
		}
		limit := numberQuery(r, "limit", 50)
		if query := strings.TrimSpace(r.URL.Query().Get("query")); query != "" {
			createdBy := optionalQuery(r, "created_by")
			updatedAfter := optionalQuery(r, "updated_after")
			threads, err := s.service.SearchThreads(r.Context(), *authContext, types.SearchThreadParams{
				Query:        query,
				Limit:        limit,
				CreatedBy:    createdBy,
				UpdatedAfter: updatedAfter,
			})
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
			return
		}
		threads, err := s.service.ListThreads(r.Context(), *authContext, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
	case http.MethodPost:
		authContext, ok := s.requireAuth(w, r)
		if !ok {
			return
		}
		var input struct {
			Title           string  `json:"title"`
			InitialMessage  *string `json:"initial_message"`
			BodyContentType *string `json:"body_content_type"`
		}
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.InitialMessage != nil {
			thread, message, err := s.service.CreateThreadWithMessage(r.Context(), *authContext, input.Title, *input.InitialMessage, input.BodyContentType)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"thread": thread, "message": message})
			return
		}
		thread, err := s.service.CreateThread(r.Context(), *authContext, input.Title)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"thread": thread})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) adminTenants(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		TenantSlug string `json:"tenant_slug"`
		TenantName string `json:"tenant_name"`
		UserEmail  string `json:"user_email"`
		UserName   string `json:"user_name"`
		Password   string `json:"password"`
		CreateKey  bool   `json:"create_key"`
		KeyName    string `json:"key_name"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.service.ProvisionTenant(r.Context(), service.ProvisionTenantParams{
		TenantSlug: input.TenantSlug,
		TenantName: input.TenantName,
		UserEmail:  input.UserEmail,
		UserName:   input.UserName,
		Password:   input.Password,
		CreateKey:  input.CreateKey,
		KeyName:    input.KeyName,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, provisionTenantResponse(result))
}

func (s *Server) adminTenantSubroutes(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/tenants/")
	tenantID, tail, ok := splitFirst(rest)
	if !ok || tenantID == "" {
		http.NotFound(w, r)
		return
	}
	switch tail {
	case "users":
		s.adminTenantUsers(w, r, tenantID)
	case "keys":
		s.adminTenantKeys(w, r, tenantID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) adminTenantUsers(w http.ResponseWriter, r *http.Request, tenantID string) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Email       string `json:"email"`
		UserEmail   string `json:"user_email"`
		DisplayName string `json:"display_name"`
		UserName    string `json:"user_name"`
		Password    string `json:"password"`
		Role        string `json:"role"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	email := firstNonEmpty(input.Email, input.UserEmail)
	displayName := firstNonEmpty(input.DisplayName, input.UserName)
	user, setupToken, err := s.service.ProvisionUser(r.Context(), service.ProvisionUserParams{
		TenantIDOrSlug: tenantID,
		Email:          email,
		DisplayName:    displayName,
		Password:       input.Password,
		Role:           input.Role,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	response := map[string]any{"user": user}
	if setupToken != "" {
		response["setup_token"] = setupToken
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) adminTenantKeys(w http.ResponseWriter, r *http.Request, tenantID string) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key, err := s.service.ProvisionTenantAPIKey(r.Context(), tenantID, input.Name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"key": apiKeyResponse(key)})
}

func (s *Server) adminKeys(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := s.service.ListAPIKeys(r.Context(), adminAuthContext())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		key, err := s.service.CreateAPIKey(r.Context(), adminAuthContext(), input.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"key": apiKeyResponse(key),
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) adminKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/keys/"), "/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if err := s.service.RevokeAPIKey(r.Context(), adminAuthContext(), name); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": name})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !tenantAdmin(*authContext) {
		writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "Tenant admin role is required.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := s.service.ListAPIKeys(r.Context(), *authContext)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		key, err := s.service.CreateAPIKey(r.Context(), *authContext, input.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"key": apiKeyResponse(key),
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) key(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !tenantAdmin(*authContext) {
		writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "Tenant admin role is required.")
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/keys/"), "/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if err := s.service.RevokeAPIKey(r.Context(), *authContext, name); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": name})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) threadSubroutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/threads/")
	threadID, tail, ok := splitFirst(rest)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "" {
		s.getThread(w, r, threadID)
		return
	}
	if tail == "messages" {
		s.postMessage(w, r, threadID)
		return
	}
	if tail == "uploads" {
		s.createUploadIntents(w, r, threadID)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request, threadID string) {
	if !method(w, r, http.MethodGet) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	thread, err := s.service.GetThread(r.Context(), *authContext, threadID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrThreadNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func (s *Server) createUploadIntents(w http.ResponseWriter, r *http.Request, threadID string) {
	if !method(w, r, http.MethodPost) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var input struct {
		Files []types.UploadIntentFile `json:"files"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uploads, err := s.service.CreatePresignedUploads(r.Context(), *authContext, threadID, input.Files)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"uploads": uploads})
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request, threadID string) {
	if !method(w, r, http.MethodPost) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	contentType := r.Header.Get("content-type")
	if strings.Contains(contentType, "multipart/form-data") {
		s.postMessageMultipart(w, r, authContext, threadID)
		return
	}

	var input struct {
		Body            *string                        `json:"body"`
		BodyContentType *string                        `json:"body_content_type"`
		File            *types.ChatGPTFileReference    `json:"file"`
		UploadedAssets  []types.UploadedAssetReference `json:"uploaded_assets"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body := ""
	if input.Body != nil {
		body = *input.Body
	}
	var file *assets.ChatGPTFileInput
	if input.File != nil {
		if err := validate.FileReference(input.File.DownloadURL, input.File.FileID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		file = &assets.ChatGPTFileInput{
			DownloadURL: input.File.DownloadURL,
			FileID:      input.File.FileID,
			MimeType:    input.File.MimeType,
			FileName:    input.File.FileName,
		}
	}
	message, err := s.service.PostMessage(r.Context(), *authContext, service.PostMessageParams{
		ThreadID:        threadID,
		Body:            body,
		BodyContentType: input.BodyContentType,
		File:            file,
		UploadedAssets:  input.UploadedAssets,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": message})
}

func (s *Server) postMessageMultipart(w http.ResponseWriter, r *http.Request, authContext *types.AuthContext, threadID string) {
	limit := s.cfg.MultipartLimitBytes
	if limit <= 0 {
		limit = config.DefaultMaxFileSizeBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+1_048_576)
	if err := r.ParseMultipartForm(limit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body := r.FormValue("body")
	var bodyContentType *string
	if value := r.FormValue("body_content_type"); value != "" {
		bodyContentType = &value
	}
	var bytes []byte
	var fileName string
	var mimeType *string
	file, header, err := r.FormFile("asset")
	if err == nil {
		defer file.Close()
		bytes, err = io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if int64(len(bytes)) > limit {
			writeError(w, http.StatusBadRequest, "File is too large. Max size is "+strconv.FormatInt(limit, 10)+" bytes.")
			return
		}
		fileName = header.Filename
		if header.Header.Get("content-type") != "" {
			contentType := header.Header.Get("content-type")
			mediaType, _, err := mime.ParseMediaType(contentType)
			if err == nil && mediaType != "" {
				mimeType = &contentType
			}
		}
	} else if !errors.Is(err, http.ErrMissingFile) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	message, err := s.service.PostMessageWithAsset(r.Context(), *authContext, service.PostMessageWithAssetParams{
		ThreadID:        threadID,
		Body:            body,
		BodyContentType: bodyContentType,
		Bytes:           bytes,
		FileName:        fileName,
		MimeType:        mimeType,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": message})
}

func (s *Server) assetSubroutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/assets/")
	assetID, tail, ok := splitFirst(rest)
	if !ok || tail != "download-url" {
		http.NotFound(w, r)
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	asset, err := s.service.GetAsset(r.Context(), *authContext, assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if asset == nil {
		writeCodedError(w, http.StatusNotFound, "ATTACHMENT_NOT_FOUND", "Asset not found.")
		return
	}
	expires := numberQuery(r, "expires_in", 300)
	safeExpires := validate.ClampSignedURLExpiry(expires)
	downloadURL, err := s.service.SignedAssetDownloadURL(r.Context(), *asset, safeExpires)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asset_id":     asset.ID,
		"file_name":    asset.FileName,
		"mime_type":    asset.MimeType,
		"size_bytes":   asset.SizeBytes,
		"expires_in":   safeExpires,
		"download_url": downloadURL,
	})
}

func (s *Server) viewerThreads(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	limit := numberQuery(r, "limit", 100)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	threads, err := s.service.ListThreads(r.Context(), *authContext, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
}

func (s *Server) viewerThread(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	threadID := strings.TrimPrefix(r.URL.Path, "/api/viewer/threads/")
	if threadID == "" || strings.Contains(threadID, "/") {
		http.NotFound(w, r)
		return
	}
	thread, err := s.service.GetThread(r.Context(), *authContext, threadID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrThreadNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	viewer, err := withViewerAssetURLs(r, s.service, thread)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": viewer})
}

func (s *Server) mcpHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !auth.ValidateOrigin(r, s.cfg) {
			writeError(w, http.StatusForbidden, "Forbidden origin")
			return
		}
		authContext, err := s.service.AuthenticateAPIKey(r.Context(), authSecretFromRequest(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if authContext == nil {
			writeError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		mcpserver.NewHTTPHandler(*authContext, s.service).ServeHTTP(w, r)
	})
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

func adminAuthContext() types.AuthContext {
	return types.AuthContext{
		TenantID:    types.DefaultTenantID,
		SubjectType: types.AuthSubjectAdmin,
		ActorName:   "admin",
		Role:        "admin",
	}
}

func provisionTenantResponse(result service.ProvisionTenantResult) map[string]any {
	response := map[string]any{
		"tenant": result.Tenant,
		"user":   result.User,
	}
	if result.SetupToken != "" {
		response["setup_token"] = result.SetupToken
	}
	if result.APIKey != nil {
		response["api_key"] = apiKeyResponse(*result.APIKey)
		response["key"] = apiKeyResponse(*result.APIKey)
	}
	return response
}

func apiKeyResponse(key types.APIKey) map[string]any {
	return map[string]any{
		"id":         key.ID,
		"tenant_id":  key.TenantID,
		"name":       key.Name,
		"key":        key.Key,
		"key_masked": key.KeyMasked,
		"created_at": key.CreatedAt,
		"updated_at": key.UpdatedAt,
	}
}

func tenantAdmin(authContext types.AuthContext) bool {
	return authContext.SubjectType == types.AuthSubjectUserSession && authContext.Role == "admin"
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
		case "THREAD_NOT_FOUND", "MESSAGE_NOT_FOUND", "ATTACHMENT_NOT_FOUND", "TENANT_NOT_FOUND":
			status = http.StatusNotFound
		case "PERMISSION_DENIED":
			status = http.StatusForbidden
		case "RATE_LIMITED":
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

func method(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method == expected {
		return true
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
	return false
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

type viewerMessage struct {
	types.Message
	Assets []viewerAsset `json:"assets"`
}

type viewerAsset struct {
	types.Asset
	DownloadURL string  `json:"download_url"`
	PreviewURL  *string `json:"preview_url"`
}

func withViewerAssetURLs(r *http.Request, svc *service.Service, thread *types.ThreadWithMessages) (viewerThread, error) {
	result := viewerThread{Thread: thread.Thread, Messages: []viewerMessage{}}
	for _, message := range thread.Messages {
		vm := viewerMessage{Message: message, Assets: []viewerAsset{}}
		for _, asset := range message.Assets {
			expires := 300
			isImage := asset.MimeType != nil && strings.HasPrefix(*asset.MimeType, "image/")
			if isImage {
				expires = 900
			}
			downloadURL, err := svc.SignedAssetDownloadURL(r.Context(), asset, expires)
			if err != nil {
				return viewerThread{}, err
			}
			var previewURL *string
			if isImage {
				previewURL = &downloadURL
			}
			vm.Assets = append(vm.Assets, viewerAsset{
				Asset:       asset,
				DownloadURL: downloadURL,
				PreviewURL:  previewURL,
			})
		}
		result.Messages = append(result.Messages, vm)
	}
	return result, nil
}
