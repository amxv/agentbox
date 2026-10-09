package httpapi

import (
	"net/http"

	"agentbox/internal/agentbox/types"
)

func (s *Server) threadDisplayPreference(w http.ResponseWriter, r *http.Request, threadID string) {
	authContext, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if authContext.SubjectType != types.AuthSubjectUserSession {
		writeCodedError(w, http.StatusForbidden, "PERMISSION_DENIED", "Display preferences require a signed-in user session.")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	switch r.Method {
	case http.MethodGet:
		preference, err := s.service.GetThreadDisplayPreference(r.Context(), *authContext, threadID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preference": preference})
	case http.MethodPatch:
		var preference types.ThreadDisplayPreference
		if err := parseJSONStrict(r, &preference); err != nil {
			writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		saved, err := s.service.SaveThreadDisplayPreference(r.Context(), *authContext, threadID, preference)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preference": saved})
	default:
		w.Header().Set("Allow", "GET, PATCH")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
