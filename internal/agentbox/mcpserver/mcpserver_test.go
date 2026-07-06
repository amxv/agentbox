package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsExposeMetadataAndAnnotations(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	server := New(testAuth(), svc)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"list_threads", "search_threads", "get_thread", "create_thread", "post_message"} {
		if byName[name] == nil {
			t.Fatalf("missing tool %s in %#v", name, byName)
		}
	}
	if !byName["list_threads"].Annotations.ReadOnlyHint {
		t.Fatalf("list_threads annotations = %#v", byName["list_threads"].Annotations)
	}
	if !byName["search_threads"].Annotations.ReadOnlyHint {
		t.Fatalf("search_threads annotations = %#v", byName["search_threads"].Annotations)
	}
	post := byName["post_message"]
	if post.Annotations.ReadOnlyHint || post.Annotations.OpenWorldHint == nil || !*post.Annotations.OpenWorldHint {
		t.Fatalf("post_message annotations = %#v", post.Annotations)
	}
	meta := post.Meta.GetMeta()
	if got := meta["openai/toolInvocation/invoked"]; got != "Posted to Agentbox" {
		t.Fatalf("post_message meta = %#v", meta)
	}
	schemaJSON, err := json.Marshal(post.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schemaJSON), "body_content_type") || !strings.Contains(string(schemaJSON), "text/markdown") {
		t.Fatalf("post_message schema = %s", schemaJSON)
	}
	fileParams, ok := meta["openai/fileParams"].([]any)
	if !ok || len(fileParams) != 1 || fileParams[0] != "file" {
		t.Fatalf("file params meta = %#v", meta["openai/fileParams"])
	}
}

func TestStreamableHTTPAllowsForwardedProductionHost(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	handler := NewHTTPHandler(testAuth(), svc)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0.0.0"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(body))
	req.Host = "agentbox-black.vercel.app"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code == http.StatusForbidden {
		t.Fatalf("unexpected host protection rejection: %s", res.Body.String())
	}
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", res.Code, res.Body.String())
	}
}

func TestStreamableHTTPCallTool(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	handler := NewHTTPHandler(testAuth(), svc)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_thread",
		Arguments: map[string]any{"title": "MCP thread", "initial_message": "Please run the narrow checks.", "body_content_type": "text/plain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content = %#v", res.Content)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	var fallback map[string]any
	if err := json.Unmarshal([]byte(text), &fallback); err != nil {
		t.Fatalf("content text is not JSON: %v text=%s", err, text)
	}
	if fallback["thread"] == nil || fallback["message"] == nil {
		t.Fatalf("content fallback = %#v", fallback)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Thread struct {
			ID        string `json:"id"`
			CreatedBy string `json:"created_by"`
		} `json:"thread"`
		Message struct {
			ThreadID        string  `json:"thread_id"`
			Body            string  `json:"body"`
			BodyContentType *string `json:"body_content_type"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Thread.ID == "" || payload.Thread.CreatedBy != "tester" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.Message.ThreadID != payload.Thread.ID || payload.Message.Body != "Please run the narrow checks." || payload.Message.BodyContentType == nil || *payload.Message.BodyContentType != "text/plain" {
		t.Fatalf("payload message = %#v", payload.Message)
	}

	search, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_threads",
		Arguments: map[string]any{"query": "narrow", "limit": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONContentHasKey(t, search, "threads")

	post, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "post_message",
		Arguments: map[string]any{"thread_id": "thr_missing", "body": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !post.IsError {
		t.Fatalf("expected MCP tool error, got %#v", post)
	}
	text = post.Content[0].(*mcp.TextContent).Text
	var errPayload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &errPayload); err != nil {
		t.Fatalf("error content text is not JSON: %v text=%s", err, text)
	}
	if errPayload.Error.Code != "THREAD_NOT_FOUND" || strings.Contains(text, "SQLSTATE") || strings.Contains(text, "constraint") {
		t.Fatalf("error payload = %#v text=%s", errPayload, text)
	}
}

func TestMCPToolsUseTenantAuthContext(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	authA := types.AuthContext{TenantID: "ten_a", SubjectType: types.AuthSubjectAPIKey, ActorName: "tenant-a", KeyID: "key_a"}
	authB := types.AuthContext{TenantID: "ten_b", SubjectType: types.AuthSubjectAPIKey, ActorName: "tenant-b", KeyID: "key_b"}
	threadA, err := svc.CreateThread(ctx, authA, "Tenant A thread")
	if err != nil {
		t.Fatal(err)
	}
	threadB, err := svc.CreateThread(ctx, authB, "Tenant B thread")
	if err != nil {
		t.Fatal(err)
	}

	server := New(authA, svc)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	listed, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_threads",
		Arguments: map[string]any{"limit": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(listed.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Threads []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Threads) != 1 || payload.Threads[0].ID != threadA.ID {
		t.Fatalf("tenant A list payload = %#v; tenant B thread = %s", payload, threadB.ID)
	}

	crossTenant, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_thread",
		Arguments: map[string]any{"thread_id": threadB.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !crossTenant.IsError {
		t.Fatalf("expected cross-tenant get_thread to fail, got %#v", crossTenant)
	}
}

func testAuth() types.AuthContext {
	return types.AuthContext{
		TenantID:    types.DefaultTenantID,
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   "tester",
		KeyID:       "key_test",
	}
}

func assertJSONContentHasKey(t *testing.T, res *mcp.CallToolResult, key string) {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("missing content")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("content text is not JSON: %v text=%s", err, text)
	}
	if _, ok := payload[key]; !ok {
		t.Fatalf("content JSON missing %s: %#v", key, payload)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var structured map[string]any
	if err := json.Unmarshal(raw, &structured); err != nil {
		t.Fatal(err)
	}
	if _, ok := structured[key]; !ok {
		t.Fatalf("structured content missing %s: %#v", key, structured)
	}
}
