package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
)

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
