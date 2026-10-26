package httpapi

import (
	"net/http"
	"net/url"

	"agentbox/internal/agentbox/types"
)

func (s *Server) ownerIssuePasswordReset(w http.ResponseWriter, r *http.Request, authContext types.AuthContext, userID string) {
	if !method(w, r, http.MethodPost) {
		return
	}
	result, err := s.service.IssueUserPasswordReset(r.Context(), authContext, userID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// Use a URL fragment so the bearer token never reaches access logs or referrers.
	resetPath := "/reset-password#token=" + url.QueryEscape(result.Token)
	writeJSON(w, http.StatusCreated, map[string]any{
		"reset_path": resetPath,
		"expires_at": result.ExpiresAt,
	})
}

func (s *Server) authPasswordResetInspect(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Token string `json:"token"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inspection, err := s.service.InspectUserPasswordReset(r.Context(), input.Token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inspection)
}

func (s *Server) authPasswordResetComplete(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := parseJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.service.CompleteUserPasswordReset(r.Context(), input.Token, input.Password); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
