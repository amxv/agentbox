package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/mcpserver"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
)

func (s *Server) threads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		authContext, ok := s.requireAuth(w, r)
		if !ok {
			return
		}
		limit := numberQuery(r, "limit", 50)
		filter := strings.TrimSpace(r.URL.Query().Get("filter"))
		teamRef := strings.TrimSpace(r.URL.Query().Get("team"))
		cursor, err := types.DecodeThreadPageCursor(r.URL.Query().Get("cursor"))
		if err != nil {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "cursor is invalid.")
			return
		}
		if query := strings.TrimSpace(r.URL.Query().Get("query")); query != "" {
			createdBy := optionalQuery(r, "created_by")
			updatedAfter := optionalQuery(r, "updated_after")
			page, err := s.service.SearchThreadsPage(r.Context(), *authContext, types.SearchThreadParams{
				Query:        query,
				Limit:        limit,
				CreatedBy:    createdBy,
				UpdatedAfter: updatedAfter,
				Filter:       filter,
				TeamRef:      teamRef,
				Cursor:       cursor,
			})
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, page)
			return
		}
		page, err := s.service.ListThreadsPage(r.Context(), *authContext, types.ThreadListParams{
			Limit:   limit,
			Filter:  filter,
			TeamRef: teamRef,
			Cursor:  cursor,
		})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
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
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"thread": thread})
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
	if tail == "view" {
		s.threadView(w, r, threadID)
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
	if tail == "visibility" {
		s.threadVisibility(w, r, threadID)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) threadVisibility(w http.ResponseWriter, r *http.Request, threadID string) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		visibility, err := s.service.ManageThreadVisibility(r.Context(), *authContext, threadID, s.requestBaseURL(r), types.ManageThreadVisibilityInput{})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"visibility": visibility})
	case http.MethodPatch:
		var input types.ManageThreadVisibilityInput
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		visibility, err := s.service.ManageThreadVisibility(r.Context(), *authContext, threadID, s.requestBaseURL(r), input)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"visibility": visibility})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) publicThreadSubroutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/public/threads/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 1 && parts[0] != "" {
		if !method(w, r, http.MethodGet) {
			return
		}
		thread, err := s.service.GetPublicThread(r.Context(), parts[0])
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
		return
	}
	if len(parts) == 4 && parts[0] != "" && parts[1] == "assets" && parts[2] != "" {
		if !method(w, r, http.MethodGet) {
			return
		}
		switch parts[3] {
		case "download":
			downloadURL, err := s.service.PublicAssetDownloadURL(r.Context(), parts[0], parts[2])
			writeAssetResolution(w, map[string]any{"asset_id": parts[2]}, "download_url", downloadURL, err)
			return
		case "preview":
			previewURL, err := s.service.PublicAssetPreviewURL(r.Context(), parts[0], parts[2])
			writeAssetResolution(w, map[string]any{"asset_id": parts[2]}, "preview_url", previewURL, err)
			return
		}
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
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func (s *Server) messageSubroutes(w http.ResponseWriter, r *http.Request) {
	messageID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/messages/"), "/")
	if messageID == "" || strings.Contains(messageID, "/") {
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
	message, err := s.service.GetMessage(r.Context(), *authContext, messageID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": message})
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
	writeJSON(w, http.StatusCreated, map[string]any{"uploads": safeUploadIntentResponses(uploads)})
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
		UploadedAssets  []types.UploadedAssetReference `json:"uploaded_assets"`
		UploadIDs       []string                       `json:"upload_ids"`
	}
	if err := parseJSONStrict(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body := ""
	if input.Body != nil {
		body = *input.Body
	}
	for _, uploadID := range input.UploadIDs {
		input.UploadedAssets = append(input.UploadedAssets, types.UploadedAssetReference{UploadID: uploadID})
	}
	message, err := s.service.PostMessage(r.Context(), *authContext, service.PostMessageParams{
		ThreadID:        threadID,
		Body:            body,
		BodyContentType: input.BodyContentType,
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
	if !ok || (tail != "download-url" && tail != "download" && tail != "preview-url" && tail != "preview") {
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
		writeServiceError(w, err)
		return
	}
	if asset == nil {
		writeCodedError(w, http.StatusNotFound, "ATTACHMENT_NOT_FOUND", "Asset not found.")
		return
	}
	expires := numberQuery(r, "expires_in", 300)
	safeExpires := validate.ClampSignedURLExpiry(expires)
	urlField := "download_url"
	signedURL := ""
	if tail == "preview-url" || tail == "preview" {
		urlField = "preview_url"
		signedURL, err = s.service.SignedAssetPreviewURL(r.Context(), *authContext, asset.ID, safeExpires)
	} else {
		signedURL, err = s.service.SignedAssetDownloadURL(r.Context(), *authContext, asset.ID, safeExpires)
	}
	payload := map[string]any{
		"asset_id":   asset.ID,
		"file_name":  asset.FileName,
		"mime_type":  asset.MimeType,
		"size_bytes": asset.SizeBytes,
		"expires_in": safeExpires,
	}
	writeAssetResolution(w, payload, urlField, signedURL, err)
}

func (s *Server) threadView(w http.ResponseWriter, r *http.Request, threadID string) {
	if !method(w, r, http.MethodGet) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if authContext.SubjectType == types.AuthSubjectAPIKey && len(authContext.Scopes) > 0 && !hasScope(*authContext, "assets:read") {
		writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "assets:read scope is required.")
		return
	}
	thread, err := s.service.GetThread(r.Context(), *authContext, threadID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	viewer := withViewerAssetPaths(thread)
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
		if len(authContext.Scopes) > 0 && !hasScope(*authContext, "mcp:use") {
			writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "mcp:use scope is required.")
			return
		}
		mcpserver.NewHTTPHandler(*authContext, s.service, s.requestBaseURL(r)).ServeHTTP(w, r)
	})
}
