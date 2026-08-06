package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/mcpserver"
	"agentbox/internal/agentbox/messageformat"
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
	if s.cfg.MaintenanceMode && !maintenanceExemptPath(r.URL.Path) && !s.hasMaintenanceBypass(r) && !s.hasOwnerBrowserMaintenanceAccess(r) {
		writeCodedError(w, http.StatusServiceUnavailable, "MAINTENANCE_MODE", "AgentBox is temporarily unavailable for the user/team cutover.")
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
	s.mux.HandleFunc("/api/public/threads/", s.publicThreadSubroutes)
	s.mux.HandleFunc("/api/assets/", s.assetSubroutes)
	s.mux.Handle("/api/mcp", s.mcpHandler())
}

func (s *Server) ownerInvitations(w http.ResponseWriter, r *http.Request) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		pageRequest, err := ownerPageRequest(r)
		if err != nil {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		page, err := s.service.ListSignupInvitationsPage(r.Context(), *authContext, pageRequest)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	case http.MethodPost:
		var input struct {
			ExpiresInMinutes int      `json:"expires_in_minutes"`
			TeamIDs          []string `json:"team_ids"`
		}
		if r.Body != nil && r.ContentLength != 0 {
			if err := parseJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if input.ExpiresInMinutes < 0 {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "expires_in_minutes must not be negative.")
			return
		}
		if len(input.TeamIDs) > types.MaxOwnerPageLimit {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "at most 100 initial teams may be selected")
			return
		}
		result, err := s.service.CreateSignupInvitation(r.Context(), *authContext, time.Duration(input.ExpiresInMinutes)*time.Minute, input.TeamIDs...)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		signupPath := "/signup?token=" + url.QueryEscape(result.Token)
		signupURL := signupPath
		if baseURL := s.requestBaseURL(r); baseURL != "" {
			signupURL = baseURL + signupPath
		}
		writeJSON(w, http.StatusCreated, map[string]any{"invitation": result.Invitation, "token": result.Token, "signup_url": signupURL})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) adminUploadCleanup(w http.ResponseWriter, r *http.Request) {
	if !s.hasMaintenanceBypass(r) {
		writeCodedError(w, http.StatusUnauthorized, "UNAUTHORIZED", "A valid maintenance key is required.")
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "limit must be between 1 and 100.")
			return
		}
		limit = parsed
	}
	result, err := s.service.CleanupPendingUploads(r.Context(), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) ownerInvitation(w http.ResponseWriter, r *http.Request) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return
	}
	invitationID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/owner/invitations/"), "/")
	if invitationID == "" || strings.Contains(invitationID, "/") {
		http.NotFound(w, r)
		return
	}
	if !method(w, r, http.MethodDelete) {
		return
	}
	if err := s.service.RevokeSignupInvitation(r.Context(), *authContext, invitationID); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": invitationID})
}

func (s *Server) ownerUsers(w http.ResponseWriter, r *http.Request) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	pageRequest, err := ownerPageRequest(r)
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	page, err := s.service.ListUsersPage(r.Context(), *authContext, pageRequest)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) ownerCredentials(w http.ResponseWriter, r *http.Request) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	pageRequest, err := ownerPageRequest(r)
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	page, err := s.service.ListOwnerAPIKeysPage(r.Context(), *authContext, pageRequest)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) ownerCredential(w http.ResponseWriter, r *http.Request) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return
	}
	if !method(w, r, http.MethodDelete) {
		return
	}
	credentialID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/owner/credentials/"), "/")
	if credentialID == "" || strings.Contains(credentialID, "/") {
		http.NotFound(w, r)
		return
	}
	if err := s.service.RevokeOwnerAPIKey(r.Context(), *authContext, credentialID); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": credentialID})
}

func (s *Server) ownerContentThreads(w http.ResponseWriter, r *http.Request) {
	ownerContext, ok := s.requireOwnerContentContext(w, r)
	if !ok {
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	pageRequest, err := ownerPageRequest(r)
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	page, err := s.service.ListOwnerContentThreadsPage(r.Context(), ownerContext, types.OwnerContentListParams{
		Limit: pageRequest.Limit, Offset: pageRequest.Offset,
		UserID: strings.TrimSpace(r.URL.Query().Get("user_id")), TeamRef: strings.TrimSpace(r.URL.Query().Get("team")),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) ownerContentSearch(w http.ResponseWriter, r *http.Request) {
	ownerContext, ok := s.requireOwnerContentContext(w, r)
	if !ok {
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	pageRequest, err := ownerPageRequest(r)
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	page, err := s.service.SearchOwnerContentThreadsPage(r.Context(), ownerContext, types.OwnerContentSearchParams{
		Query: strings.TrimSpace(r.URL.Query().Get("query")), Limit: pageRequest.Limit, Offset: pageRequest.Offset,
		UserID: strings.TrimSpace(r.URL.Query().Get("user_id")), TeamRef: strings.TrimSpace(r.URL.Query().Get("team")),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) ownerContentThread(w http.ResponseWriter, r *http.Request) {
	ownerContext, ok := s.requireOwnerContentContext(w, r)
	if !ok {
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	threadID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/owner/content/threads/"), "/")
	if threadID == "" || strings.Contains(threadID, "/") {
		http.NotFound(w, r)
		return
	}
	thread, err := s.service.GetOwnerContentThread(r.Context(), ownerContext, threadID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	viewer := withOwnerContentAssetPaths(thread)
	writeJSON(w, http.StatusOK, map[string]any{"thread": viewer})
}

func (s *Server) ownerContentAsset(w http.ResponseWriter, r *http.Request) {
	ownerContext, ok := s.requireOwnerContentContext(w, r)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/owner/content/assets/")
	assetID, tail, pathOK := splitFirst(rest)
	if !pathOK || (tail != "download" && tail != "preview") {
		http.NotFound(w, r)
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	safeExpires := validate.ClampSignedURLExpiry(numberQuery(r, "expires_in", 300))
	urlField := "download_url"
	signedURL := ""
	var err error
	if tail == "preview" {
		urlField = "preview_url"
		signedURL, err = s.service.SignedOwnerContentAssetPreviewURL(r.Context(), ownerContext, assetID, safeExpires)
	} else {
		signedURL, err = s.service.SignedOwnerContentAssetDownloadURL(r.Context(), ownerContext, assetID, safeExpires)
	}
	payload := map[string]any{
		"asset_id":   assetID,
		"expires_in": safeExpires,
	}
	writeAssetResolution(w, payload, urlField, signedURL, err)
}

func (s *Server) ownerTeams(w http.ResponseWriter, r *http.Request) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		pageRequest, err := ownerPageRequest(r)
		if err != nil {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		page, err := s.service.ListOwnerTeamsPage(r.Context(), *authContext, pageRequest)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	case http.MethodPost:
		var input struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		team, err := s.service.CreateTeam(r.Context(), *authContext, input.Slug, input.Name)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"team": team})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) ownerTeam(w http.ResponseWriter, r *http.Request) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/owner/teams/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 1 && parts[0] != "" {
		if !method(w, r, http.MethodPatch) {
			return
		}
		var input struct {
			Name string `json:"name"`
		}
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		team, err := s.service.RenameTeam(r.Context(), *authContext, parts[0], input.Name)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"team": team})
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "members" {
		if !method(w, r, http.MethodGet) {
			return
		}
		pageRequest, err := ownerPageRequest(r)
		if err != nil {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		page, err := s.service.ListOwnerTeamMembersPage(r.Context(), *authContext, parts[0], pageRequest)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
		return
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] == "members" && parts[2] != "" {
		switch r.Method {
		case http.MethodPut:
			membership, err := s.service.AddTeamMember(r.Context(), *authContext, parts[0], parts[2])
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"membership": membership})
		case http.MethodDelete:
			if err := s.service.RemoveTeamMember(r.Context(), *authContext, parts[0], parts[2]); err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"removed": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	http.NotFound(w, r)
}

func (s *Server) myTeams(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	teams, err := s.service.ListMyTeams(r.Context(), *authContext)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

func (s *Server) ownerUserAction(w http.ResponseWriter, r *http.Request) {
	authContext := s.requireOwnerBrowser(w, r)
	if authContext == nil {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/owner/users/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && parts[1] == "teams" {
		pageRequest, err := ownerPageRequest(r)
		if err != nil {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		page, err := s.service.ListOwnerUserTeamsPage(r.Context(), *authContext, parts[0], pageRequest)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	disabled := false
	switch parts[1] {
	case "disable":
		disabled = true
	case "enable":
		disabled = false
	case "purge-attachments":
		var input struct {
			Limit int `json:"limit"`
		}
		if r.Body != nil && r.ContentLength != 0 {
			if err := parseJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		result, err := s.service.PurgeUserAttachments(r.Context(), *authContext, parts[0], input.Limit)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"purge": result})
		return
	default:
		http.NotFound(w, r)
		return
	}
	user, err := s.service.SetUserDisabled(r.Context(), *authContext, parts[0], disabled)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) authInvitationInspect(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inspection, err := s.service.InspectSignupInvitation(r.Context(), input.Token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inspection)
}

func (s *Server) authInvitationRegister(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Token       string `json:"token"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	authContext, sessionSecret, user, err := s.service.RegisterWithSignupInvitation(r.Context(), input.Token, input.Email, input.DisplayName, input.Password)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	s.setSessionCookie(w, sessionSecret)
	writeJSON(w, http.StatusCreated, map[string]any{
		"auth":     authContext,
		"user":     user,
		"redirect": "/onboarding",
	})
}

func (s *Server) adminOwnerSetupToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		ExpiresInMinutes int `json:"expires_in_minutes"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if input.ExpiresInMinutes < 0 {
		writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "expires_in_minutes must not be negative.")
		return
	}
	ttl := time.Duration(input.ExpiresInMinutes) * time.Minute
	result, err := s.service.IssueOwnerSetupToken(r.Context(), ttl)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	setupPath := "/owner/setup?token=" + url.QueryEscape(result.Token)
	setupURL := setupPath
	if baseURL := s.requestBaseURL(r); baseURL != "" {
		setupURL = baseURL + setupPath
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      result.Token,
		"purpose":    result.Purpose,
		"expires_at": result.ExpiresAt,
		"setup_url":  setupURL,
	})
}

func (s *Server) authOwnerSetup(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Token       string `json:"token"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	authContext, sessionSecret, owner, err := s.service.CompleteOwnerSetup(r.Context(), input.Token, input.Email, input.DisplayName, input.Password)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	s.setSessionCookie(w, sessionSecret)
	writeJSON(w, http.StatusOK, map[string]any{
		"auth":  authContext,
		"owner": owner,
	})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	authContext, secret, err := s.service.Login(r.Context(), "", input.Email, input.Password)
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

func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !canReadKeys(*authContext) {
			writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "Browser session or keys:read scope is required.")
			return
		}
		pageRequest, err := ownerPageRequest(r)
		if err != nil {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		page, err := s.service.ListAPIKeysPage(r.Context(), *authContext, pageRequest)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		credentials := apiKeyResponses(page.Credentials)
		writeJSON(w, http.StatusOK, map[string]any{"credentials": credentials, "keys": credentials, "page": page.Page})
	case http.MethodPost:
		if !canManageKeys(*authContext) {
			writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "Browser session or keys:write scope is required.")
			return
		}
		var input struct {
			Name    string   `json:"name"`
			Purpose string   `json:"purpose"`
			Scopes  []string `json:"scopes"`
		}
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.EqualFold(strings.TrimSpace(input.Purpose), "raycast") {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Use the dedicated Raycast installation flow so purpose and scopes are fixed safely.")
			return
		}
		scopes := input.Scopes
		if len(scopes) == 0 {
			scopes = service.ConnectorAPIKeyScopes(input.Purpose)
		}
		key, err := s.service.CreateAPIKeyWithPurposeAndScopes(r.Context(), *authContext, input.Name, input.Purpose, scopes)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"key": apiKeyResponse(key),
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) raycastInstallations(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !canManageKeys(*authContext) {
		writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "Browser session or keys:write scope is required.")
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Label string `json:"label"`
	}
	if err := parseJSONStrict(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.service.CreateRaycastInstallation(r.Context(), *authContext, input.Label, s.requestBaseURL(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"credential":    apiKeyResponse(result.Credential),
		"raycast_setup": result.Setup,
	})
}

func (s *Server) onboarding(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	state, err := s.service.GetOnboardingState(r.Context(), *authContext)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"onboarding": state})
}

func (s *Server) onboardingSkip(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	state, err := s.service.DismissOnboarding(r.Context(), *authContext)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"onboarding": state})
}

func (s *Server) onboardingConnector(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	connector := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/onboarding/connectors/"), "/")
	if connector == "" || strings.Contains(connector, "/") {
		http.NotFound(w, r)
		return
	}
	var input struct {
		Rotate bool `json:"rotate"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := parseJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	result, err := s.service.CreateOnboardingConnection(r.Context(), *authContext, connector, s.requestBaseURL(r), input.Rotate)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"connector":       result.Connector,
		"credential":      apiKeyResponse(result.Credential),
		"onboarding":      result.State,
		"mcp_url":         result.MCPURL,
		"profile_command": result.ProfileCommand,
		"setup_prompt":    result.SetupPrompt,
		"raycast_setup":   result.RaycastSetup,
		"instructions":    result.Instructions,
	})
}

func (s *Server) key(w http.ResponseWriter, r *http.Request) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !canManageKeys(*authContext) {
		writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "Browser session or keys:write scope is required.")
		return
	}
	remainder := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/keys/"), "/")
	parts := strings.Split(remainder, "/")
	if remainder == "" || len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
		http.NotFound(w, r)
		return
	}
	credentialID := parts[0]
	if len(parts) == 2 {
		if parts[1] != "setup" || !method(w, r, http.MethodGet) {
			return
		}
		setup, err := s.service.RaycastInstallationSetup(r.Context(), *authContext, credentialID, s.requestBaseURL(r))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"raycast_setup": setup})
		return
	}
	switch r.Method {
	case http.MethodDelete:
		var err error
		if strings.HasPrefix(credentialID, "key_") {
			err = s.service.RevokeAPIKeyByID(r.Context(), *authContext, credentialID)
		} else {
			// Compatibility for pre-inventory clients. Maintained clients use
			// stable credential IDs so similarly named historical rows remain
			// individually addressable.
			err = s.service.RevokeAPIKey(r.Context(), *authContext, credentialID)
		}
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": credentialID})
	case http.MethodPatch:
		rotated, setup, err := s.service.RotateAPIKeyByID(r.Context(), *authContext, credentialID, s.requestBaseURL(r))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"credential": apiKeyResponse(rotated), "raycast_setup": setup})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
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
		case "THREAD_NOT_FOUND", "MESSAGE_NOT_FOUND", "ATTACHMENT_NOT_FOUND", "TENANT_NOT_FOUND", "TEAM_NOT_FOUND", "USER_NOT_FOUND", "PUBLIC_LINK_NOT_FOUND", "PUBLIC_ASSET_NOT_FOUND", "CREDENTIAL_NOT_FOUND":
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
