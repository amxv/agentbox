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

func setThreadVisibilityForTest(ctx context.Context, repository interface {
	ManageThreadVisibility(context.Context, string, string, types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error)
}, userID string, threadID string, desiredTeamIDs []string) (types.ThreadVisibility, error) {
	current, err := repository.ManageThreadVisibility(ctx, userID, threadID, types.ManageThreadVisibilityInput{})
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	desired := map[string]bool{}
	for _, teamID := range desiredTeamIDs {
		desired[teamID] = true
	}
	currentIDs := map[string]bool{}
	input := types.ManageThreadVisibilityInput{}
	for _, team := range current.SharedTeams {
		currentIDs[team.ID] = true
		if !desired[team.ID] {
			input.RemoveTeams = append(input.RemoveTeams, team.ID)
		}
	}
	for _, teamID := range desiredTeamIDs {
		if !currentIDs[teamID] {
			input.AddTeams = append(input.AddTeams, teamID)
		}
	}
	state, err := repository.ManageThreadVisibility(ctx, userID, threadID, input)
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	return types.ThreadVisibility{ThreadID: state.ThreadID, OwnerUserID: state.OwnerUserID, SharedTeams: state.SharedTeams}, nil
}

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
	for _, name := range []string{"list_threads", "search_threads", "get_thread", "create_thread", "post_message", "manage_thread_visibility"} {
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
	visibility := byName["manage_thread_visibility"]
	if visibility.Annotations.ReadOnlyHint || visibility.Annotations.DestructiveHint == nil || !*visibility.Annotations.DestructiveHint {
		t.Fatalf("manage_thread_visibility annotations = %#v", visibility.Annotations)
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
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatal(err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("post_message properties = %#v", schema["properties"])
	}
	fileSchema, ok := properties["file"].(map[string]any)
	if !ok {
		t.Fatalf("post_message file schema = %#v", properties["file"])
	}
	if fileSchema["type"] != "object" || fileSchema["additionalProperties"] != false {
		t.Fatalf("post_message file schema = %#v", fileSchema)
	}
	if _, exists := fileSchema["anyOf"]; exists {
		t.Fatalf("post_message file schema contains a string/object union: %#v", fileSchema)
	}
	fileProperties, ok := fileSchema["properties"].(map[string]any)
	if !ok || len(fileProperties) != 4 {
		t.Fatalf("post_message file properties = %#v", fileSchema["properties"])
	}
	for _, name := range []string{"download_url", "file_id", "mime_type", "file_name"} {
		property, ok := fileProperties[name].(map[string]any)
		if !ok || len(property) != 1 || property["type"] != "string" {
			t.Fatalf("post_message file property %s = %#v", name, fileProperties[name])
		}
	}
	fileRequired, ok := fileSchema["required"].([]any)
	if !ok || len(fileRequired) != 2 || fileRequired[0] != "download_url" || fileRequired[1] != "file_id" {
		t.Fatalf("post_message file required = %#v", fileSchema["required"])
	}
	topRequired, ok := schema["required"].([]any)
	if !ok || len(topRequired) != 1 || topRequired[0] != "thread_id" {
		t.Fatalf("post_message top-level required = %#v", schema["required"])
	}
	description := strings.ToLower(post.Description)
	for _, forbidden := range []string{"file_", "download_url", "sandbox", "filesystem path", "plain filename"} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("post_message description contains transport instruction %q: %s", forbidden, post.Description)
		}
	}
	fileParams, ok := meta["openai/fileParams"].([]any)
	if !ok || len(fileParams) != 1 || fileParams[0] != "file" {
		t.Fatalf("file params meta = %#v", meta["openai/fileParams"])
	}
	createSchemaJSON, err := json.Marshal(byName["create_thread"].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"add_teams", "remove_teams", "public", "regenerate_public_link"} {
		if strings.Contains(string(createSchemaJSON), forbidden) {
			t.Fatalf("create_thread schema contains visibility field %q: %s", forbidden, createSchemaJSON)
		}
	}

	visibilitySchemaJSON, err := json.Marshal(visibility.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"thread_id", "add_teams", "remove_teams", "public", "regenerate_public_link"} {
		if !strings.Contains(string(visibilitySchemaJSON), required) {
			t.Fatalf("manage_thread_visibility schema missing %q: %s", required, visibilitySchemaJSON)
		}
	}
}

func TestParseFileInputRequiresClosedStructuredObject(t *testing.T) {
	valid, err := parseFileInput(json.RawMessage(`{"download_url":"https://files.openai.example/download/token","file_id":"file_abc123","mime_type":"text/markdown","file_name":"handoff.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	if valid.DownloadURL != "https://files.openai.example/download/token" || valid.FileID != "file_abc123" || valid.FileName == nil || *valid.FileName != "handoff.md" {
		t.Fatalf("valid file = %#v", valid)
	}

	for _, raw := range []string{
		`"file_abc123"`,
		`"sandbox:/mnt/data/handoff.md"`,
		`"https://files.openai.example/download/token"`,
		`[{"download_url":"https://files.openai.example/download/token","file_id":"file_abc123"}]`,
		`{"download_url":"https://files.openai.example/download/token","file_id":"file_abc123","extra":"not allowed"}`,
		`{"download_url":"sandbox:/mnt/data/handoff.md","file_id":"file_abc123"}`,
	} {
		if _, err := parseFileInput(json.RawMessage(raw)); err == nil {
			t.Fatalf("parseFileInput(%s) unexpectedly succeeded", raw)
		}
	}
}

func TestPostMessageAcceptsStructuredChatGPTArtifact(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := service.New(repo, store)
	auth := testAuth()
	thread, err := svc.CreateThread(ctx, auth, "Structured artifact")
	if err != nil {
		t.Fatal(err)
	}

	server := New(auth, svc)
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

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "post_message",
		Arguments: map[string]any{
			"thread_id": thread.ID,
			"body":      "attached from ChatGPT",
			"file": map[string]any{
				"download_url": "https://files.openai.example/download/token",
				"file_id":      "file_abc123",
				"mime_type":    "text/markdown",
				"file_name":    "handoff.md",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("post_message result = %#v", result)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Message types.Message `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Message.Assets) != 1 || payload.Message.Assets[0].FileName != "handoff.md" || payload.Message.Assets[0].SizeBytes != int64(len("fake-chatgpt-file")) {
		t.Fatalf("message = %#v", payload.Message)
	}
	if len(store.Uploads) != 1 || store.Uploads[0].FileName != "handoff.md" || store.Uploads[0].SizeBytes != int64(len("fake-chatgpt-file")) {
		t.Fatalf("uploads = %#v message assets = %#v", store.Uploads, payload.Message.Assets)
	}
}

func TestStreamableHTTPAllowsForwardedProductionHost(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	handler := NewHTTPHandler(testAuth(), svc, "https://agentbox.example")

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
	auth := testAuth()
	repo.Users = append(repo.Users, types.User{ID: auth.UserID, Email: "mcp@example.com", DisplayName: auth.UserDisplayName})
	handler := NewHTTPHandler(auth, svc, "https://agentbox.example")
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
			ID                       string                        `json:"id"`
			CreatedBy                string                        `json:"created_by"`
			CreatedByUserDisplayName *string                       `json:"created_by_user_display_name"`
			CreatedByActorName       *string                       `json:"created_by_actor_name"`
			VisibilitySummary        types.ThreadVisibilitySummary `json:"visibility_summary"`
		} `json:"thread"`
		Message struct {
			ThreadID                 string  `json:"thread_id"`
			Body                     string  `json:"body"`
			BodyContentType          *string `json:"body_content_type"`
			CreatedByUserDisplayName *string `json:"created_by_user_display_name"`
			CreatedByActorName       *string `json:"created_by_actor_name"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Thread.ID == "" || payload.Thread.CreatedBy != "tester" || payload.Thread.CreatedByUserDisplayName == nil || *payload.Thread.CreatedByUserDisplayName != "Test User" || payload.Thread.CreatedByActorName == nil || *payload.Thread.CreatedByActorName != "tester" || !payload.Thread.VisibilitySummary.Private {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.Message.ThreadID != payload.Thread.ID || payload.Message.Body != "Please run the narrow checks." || payload.Message.BodyContentType == nil || *payload.Message.BodyContentType != "text/plain" || payload.Message.CreatedByUserDisplayName == nil || *payload.Message.CreatedByUserDisplayName != "Test User" || payload.Message.CreatedByActorName == nil || *payload.Message.CreatedByActorName != "tester" {
		t.Fatalf("payload message = %#v", payload.Message)
	}

	oldTeam, err := repo.CreateTeam(ctx, "mcp-old", "MCP Old")
	if err != nil {
		t.Fatal(err)
	}
	newTeam, err := repo.CreateTeam(ctx, "mcp-new", "MCP New")
	if err != nil {
		t.Fatal(err)
	}
	for _, teamID := range []string{oldTeam.ID, newTeam.ID} {
		if _, err := repo.AddTeamMember(ctx, teamID, auth.UserID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := setThreadVisibilityForTest(ctx, repo, auth.UserID, payload.Thread.ID, []string{oldTeam.ID}); err != nil {
		t.Fatal(err)
	}
	managed, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "manage_thread_visibility",
		Arguments: map[string]any{
			"thread_id":    payload.Thread.ID,
			"add_teams":    []string{"mcp-new", "mcp-new"},
			"remove_teams": []string{oldTeam.ID},
			"public":       true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if managed.IsError {
		t.Fatalf("manage_thread_visibility failed: %#v", managed)
	}
	managedRaw, err := json.Marshal(managed.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var managedPayload struct {
		Visibility types.ManagedThreadVisibility `json:"visibility"`
	}
	if err := json.Unmarshal(managedRaw, &managedPayload); err != nil {
		t.Fatal(err)
	}
	if len(managedPayload.Visibility.SharedTeams) != 1 || managedPayload.Visibility.SharedTeams[0].ID != newTeam.ID || !managedPayload.Visibility.Public || !strings.HasPrefix(managedPayload.Visibility.PublicURL, "https://agentbox.example/share/agpub_") {
		t.Fatalf("managed visibility = %#v", managedPayload.Visibility)
	}
	if len(managedPayload.Visibility.AvailableTeams) != 2 {
		t.Fatalf("available teams = %#v", managedPayload.Visibility.AvailableTeams)
	}
	readVisibility, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "manage_thread_visibility",
		Arguments: map[string]any{"thread_id": payload.Thread.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	readRaw, _ := json.Marshal(readVisibility.StructuredContent)
	if !strings.Contains(string(readRaw), managedPayload.Visibility.PublicURL) {
		t.Fatalf("visibility read did not redisplay public URL: %s", readRaw)
	}

	listed, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_threads", Arguments: map[string]any{"limit": 10}})
	if err != nil {
		t.Fatal(err)
	}
	listedRaw, _ := json.Marshal(listed.StructuredContent)
	if !strings.Contains(string(listedRaw), `"visibility_summary"`) || !strings.Contains(string(listedRaw), `"public":true`) || !strings.Contains(string(listedRaw), `"created_by_user_display_name":"Test User"`) || !strings.Contains(string(listedRaw), `"created_by_actor_name":"tester"`) {
		t.Fatalf("MCP list metadata=%s", listedRaw)
	}
	gotThread, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_thread", Arguments: map[string]any{"thread_id": payload.Thread.ID}})
	if err != nil {
		t.Fatal(err)
	}
	gotThreadRaw, _ := json.Marshal(gotThread.StructuredContent)
	if !strings.Contains(string(gotThreadRaw), `"visibility_summary"`) || !strings.Contains(string(gotThreadRaw), `"created_by_user_display_name":"Test User"`) || !strings.Contains(string(gotThreadRaw), `"created_by_actor_name":"tester"`) {
		t.Fatalf("MCP get metadata=%s", gotThreadRaw)
	}
	searched, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_threads", Arguments: map[string]any{"query": "MCP thread", "limit": 10}})
	if err != nil {
		t.Fatal(err)
	}
	searchedRaw, _ := json.Marshal(searched.StructuredContent)
	if !strings.Contains(string(searchedRaw), `"visibility_summary"`) || !strings.Contains(string(searchedRaw), `"public":true`) || !strings.Contains(string(searchedRaw), `"created_by_user_display_name":"Test User"`) || !strings.Contains(string(searchedRaw), `"created_by_actor_name":"tester"`) {
		t.Fatalf("MCP search metadata=%s", searchedRaw)
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

func TestMCPToolsUseUserAuthContext(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})
	authA := types.AuthContext{UserID: "usr_a", UserDisplayName: "User A", SubjectType: types.AuthSubjectAPIKey, ActorName: "agent-a", KeyID: "key_a"}
	authB := types.AuthContext{UserID: "usr_b", UserDisplayName: "User B", SubjectType: types.AuthSubjectAPIKey, ActorName: "agent-b", KeyID: "key_b"}
	threadA, err := svc.CreateThread(ctx, authA, "User A thread")
	if err != nil {
		t.Fatal(err)
	}
	threadB, err := svc.CreateThread(ctx, authB, "User B thread")
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
		t.Fatalf("user A list payload = %#v; user B thread = %s", payload, threadB.ID)
	}

	crossUser, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_thread",
		Arguments: map[string]any{"thread_id": threadB.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !crossUser.IsError {
		t.Fatalf("expected cross-user get_thread to fail, got %#v", crossUser)
	}
	crossVisibility, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "manage_thread_visibility",
		Arguments: map[string]any{"thread_id": threadB.ID, "public": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !crossVisibility.IsError {
		t.Fatalf("expected cross-user visibility mutation to fail, got %#v", crossVisibility)
	}
}

func testAuth() types.AuthContext {
	return types.AuthContext{
		UserID:          "usr_test",
		UserDisplayName: "Test User",
		SubjectType:     types.AuthSubjectAPIKey,
		ActorName:       "tester",
		KeyID:           "key_test",
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
