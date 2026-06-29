package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
)

func TestHealth(t *testing.T) {
	svc := service.New(&db.MemoryRepository{}, &assets.FakeStore{})
	server := NewServer(config.Config{}, svc)

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true || payload["service"] != "agentbox" {
		t.Fatalf("payload = %#v", payload)
	}
}
