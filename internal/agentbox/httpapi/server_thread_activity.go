package httpapi

import (
	"net/http"
)

func (s *Server) threadActivity(w http.ResponseWriter, r *http.Request, threadID string) {
	if !method(w, r, http.MethodGet) {
		return
	}
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	updatedAt, err := s.service.GetThreadActivity(r.Context(), *authContext, threadID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"updated_at": updatedAt})
}
