package mcpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
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
	server := New(types.Actor{Name: "tester", KeyName: "test"}, svc)
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
	for _, name := range []string{"list_threads", "get_thread", "create_thread", "post_message"} {
		if byName[name] == nil {
			t.Fatalf("missing tool %s in %#v", name, byName)
		}
	}
	if !byName["list_threads"].Annotations.ReadOnlyHint {
		t.Fatalf("list_threads annotations = %#v", byName["list_threads"].Annotations)
	}
	post := byName["post_message"]
	if post.Annotations.ReadOnlyHint || post.Annotations.OpenWorldHint == nil || !*post.Annotations.OpenWorldHint {
		t.Fatalf("post_message annotations = %#v", post.Annotations)
	}
	meta := post.Meta.GetMeta()
	if got := meta["openai/toolInvocation/invoked"]; got != "Posted to Agentbox" {
		t.Fatalf("post_message meta = %#v", meta)
	}
	fileParams, ok := meta["openai/fileParams"].([]any)
	if !ok || len(fileParams) != 1 || fileParams[0] != "file" {
		t.Fatalf("file params meta = %#v", meta["openai/fileParams"])
	}
}

func TestStreamableHTTPCallTool(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	handler := NewHTTPHandler(types.Actor{Name: "tester", KeyName: "test"}, svc)
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
		Arguments: map[string]any{"title": "MCP thread"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 || res.Content[0].(*mcp.TextContent).Text != "Created Agentbox thread." {
		t.Fatalf("content = %#v", res.Content)
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
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Thread.ID == "" || payload.Thread.CreatedBy != "tester" {
		t.Fatalf("payload = %#v", payload)
	}
}
