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
	s.mux.HandleFunc("/api/threads", s.threads)
	s.mux.HandleFunc("/api/threads/", s.threadSubroutes)
	s.mux.HandleFunc("/api/assets/", s.assetSubroutes)
	s.mux.HandleFunc("/api/viewer/threads", s.viewerThreads)
	s.mux.HandleFunc("/api/viewer/threads/", s.viewerThread)
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

func (s *Server) threads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.requireActor(w, r); !ok {
			return
		}
		limit := numberQuery(r, "limit", 50)
		threads, err := s.service.ListThreads(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
	case http.MethodPost:
		actor, ok := s.requireActor(w, r)
		if !ok {
			return
		}
		var input struct {
			Title string `json:"title"`
		}
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		thread, err := s.service.CreateThread(r.Context(), *actor, input.Title)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
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
	if tail == "messages" {
		s.postMessage(w, r, threadID)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request, threadID string) {
	if !method(w, r, http.MethodGet) {
		return
	}
	if _, ok := s.requireActor(w, r); !ok {
		return
	}
	thread, err := s.service.GetThread(r.Context(), threadID)
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

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request, threadID string) {
	if !method(w, r, http.MethodPost) {
		return
	}
	actor, ok := s.requireActor(w, r)
	if !ok {
		return
	}
	contentType := r.Header.Get("content-type")
	if strings.Contains(contentType, "multipart/form-data") {
		s.postMessageMultipart(w, r, actor, threadID)
		return
	}

	var input struct {
		Body *string                     `json:"body"`
		File *types.ChatGPTFileReference `json:"file"`
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
	message, err := s.service.PostMessage(r.Context(), *actor, service.PostMessageParams{
		ThreadID: threadID,
		Body:     body,
		File:     file,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": message})
}

func (s *Server) postMessageMultipart(w http.ResponseWriter, r *http.Request, actor *types.Actor, threadID string) {
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
	message, err := s.service.PostMessageWithAsset(r.Context(), *actor, service.PostMessageWithAssetParams{
		ThreadID: threadID,
		Body:     body,
		Bytes:    bytes,
		FileName: fileName,
		MimeType: mimeType,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	if _, ok := s.requireActor(w, r); !ok {
		return
	}
	asset, err := s.service.GetAsset(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if asset == nil {
		writeError(w, http.StatusNotFound, "Asset not found.")
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
	if err := auth.RequireAdminRequest(r, s.cfg); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "Unauthorized" {
			status = http.StatusUnauthorized
		}
		writeError(w, status, err.Error())
		return
	}
	limit := numberQuery(r, "limit", 100)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	threads, err := s.service.ListThreads(r.Context(), limit)
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
	if err := auth.RequireAdminRequest(r, s.cfg); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "Unauthorized" {
			status = http.StatusUnauthorized
		}
		writeError(w, status, err.Error())
		return
	}
	threadID := strings.TrimPrefix(r.URL.Path, "/api/viewer/threads/")
	if threadID == "" || strings.Contains(threadID, "/") {
		http.NotFound(w, r)
		return
	}
	thread, err := s.service.GetThread(r.Context(), threadID)
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
		actor, err := auth.AuthenticateRequest(r, s.cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if actor == nil {
			writeError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		mcpserver.NewHTTPHandler(*actor, s.service).ServeHTTP(w, r)
	})
}

func (s *Server) requireActor(w http.ResponseWriter, r *http.Request) (*types.Actor, bool) {
	actor, err := auth.AuthenticateRequest(r, s.cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return nil, false
	}
	return actor, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
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
