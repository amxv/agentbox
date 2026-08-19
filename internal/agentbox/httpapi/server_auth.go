package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agentbox/internal/agentbox/service"
)

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
