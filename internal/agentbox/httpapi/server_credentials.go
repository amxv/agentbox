package httpapi

import (
	"net/http"
	"strings"

	"agentbox/internal/agentbox/service"
)

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
		err := s.service.RevokeAPIKeyByID(r.Context(), *authContext, credentialID)
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
