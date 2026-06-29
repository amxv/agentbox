package httpapi

import (
	"encoding/json"
	"net/http"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/service"
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
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "agentbox",
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
