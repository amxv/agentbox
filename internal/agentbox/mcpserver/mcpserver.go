package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
	"agentbox/internal/agentbox/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	actor types.Actor
	svc   *service.Service
}

func New(actor types.Actor, svc *service.Service) *mcp.Server {
	builder := &Server{actor: actor, svc: svc}
	return builder.build()
}

func NewHTTPHandler(actor types.Actor, svc *service.Service) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server {
			return New(actor, svc)
		},
		&mcp.StreamableHTTPOptions{
			Stateless:      true,
			JSONResponse:   true,
			SessionTimeout: 0,
		},
	)
}

func (s *Server) build() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "agentbox", Version: version.Version}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{},
		GetSessionID: func() string { return "" },
	})
	server.AddTool(&mcp.Tool{
		Name:        "list_threads",
		Title:       "List threads",
		Description: "List recent Agentbox threads.",
		InputSchema: objectSchema(map[string]any{
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		}, nil),
		OutputSchema: objectSchema(map[string]any{
			"threads": map[string]any{"type": "array", "items": map[string]any{}},
		}, []string{"threads"}),
		Annotations: annotations(true, false, false),
	}, s.listThreads)
	server.AddTool(&mcp.Tool{
		Name:        "get_thread",
		Title:       "Get thread",
		Description: "Read an Agentbox thread and its messages.",
		InputSchema: objectSchema(map[string]any{
			"thread_id": map[string]any{"type": "string", "minLength": 1},
		}, []string{"thread_id"}),
		OutputSchema: objectSchema(map[string]any{
			"thread": map[string]any{},
		}, []string{"thread"}),
		Annotations: annotations(true, false, false),
	}, s.getThread)
	server.AddTool(&mcp.Tool{
		Name:        "create_thread",
		Title:       "Create thread",
		Description: "Create a new Agentbox thread.",
		InputSchema: objectSchema(map[string]any{
			"title": map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
		}, []string{"title"}),
		OutputSchema: objectSchema(map[string]any{
			"thread": map[string]any{},
		}, []string{"thread"}),
		Annotations: annotations(false, false, true),
	}, s.createThread)
	server.AddTool(&mcp.Tool{
		Meta:        mcp.Meta{"openai/fileParams": []string{"file"}, "openai/toolInvocation/invoking": "Posting to Agentbox…", "openai/toolInvocation/invoked": "Posted to Agentbox"},
		Name:        "post_message",
		Title:       "Post message",
		Description: "Post a Markdown message to an Agentbox thread. To attach a file from ChatGPT, pass the uploaded conversation file ID, for example file_abc123. Do not pass a local filesystem path or plain filename.",
		InputSchema: objectSchema(map[string]any{
			"thread_id": map[string]any{"type": "string", "minLength": 1},
			"body":      map[string]any{"type": "string"},
			"file": map[string]any{
				"anyOf": []any{
					map[string]any{
						"type": "object",
						"properties": map[string]any{
							"download_url": map[string]any{"type": "string", "format": "uri"},
							"file_id":      map[string]any{"type": "string", "minLength": 1},
							"mime_type":    map[string]any{"type": "string"},
							"file_name":    map[string]any{"type": "string"},
						},
						"required":             []string{"download_url", "file_id"},
						"additionalProperties": true,
					},
					map[string]any{"type": "string"},
				},
			},
		}, []string{"thread_id"}),
		OutputSchema: objectSchema(map[string]any{
			"message": map[string]any{},
		}, []string{"message"}),
		Annotations: annotations(false, false, true),
	}, s.postMessage)
	return server
}

func (s *Server) listThreads(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		Limit *int `json:"limit"`
	}
	if err := decodeArgs(req, &input); err != nil {
		return nil, err
	}
	limit := 0
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit > 100 {
		limit = 100
	}
	threads, err := s.svc.ListThreads(ctx, limit)
	if err != nil {
		return nil, err
	}
	return result("Listed Agentbox threads.", map[string]any{"threads": threads}), nil
}

func (s *Server) getThread(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		ThreadID string `json:"thread_id"`
	}
	if err := decodeArgs(req, &input); err != nil {
		return nil, err
	}
	if err := validate.ThreadID(input.ThreadID); err != nil {
		return nil, err
	}
	thread, err := s.svc.GetThread(ctx, input.ThreadID)
	if err != nil {
		return nil, err
	}
	return result("Fetched Agentbox thread.", map[string]any{"thread": thread}), nil
}

func (s *Server) createThread(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		Title string `json:"title"`
	}
	if err := decodeArgs(req, &input); err != nil {
		return nil, err
	}
	thread, err := s.svc.CreateThread(ctx, s.actor, input.Title)
	if err != nil {
		return nil, err
	}
	return result("Created Agentbox thread.", map[string]any{"thread": thread}), nil
}

func (s *Server) postMessage(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var raw struct {
		ThreadID string          `json:"thread_id"`
		Body     string          `json:"body"`
		File     json.RawMessage `json:"file"`
	}
	if err := decodeArgs(req, &raw); err != nil {
		return nil, err
	}
	var file *assets.ChatGPTFileInput
	if len(raw.File) > 0 && string(raw.File) != "null" {
		parsed, err := parseFileInput(raw.File)
		if err != nil {
			return nil, err
		}
		file = parsed
	}
	message, err := s.svc.PostMessage(ctx, s.actor, service.PostMessageParams{
		ThreadID: raw.ThreadID,
		Body:     raw.Body,
		File:     file,
	})
	if err != nil {
		return nil, err
	}
	return result("Posted message to Agentbox.", map[string]any{"message": message}), nil
}

func parseFileInput(raw json.RawMessage) (*assets.ChatGPTFileInput, error) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return &assets.ChatGPTFileInput{RawString: asString}, nil
	}
	var file types.ChatGPTFileReference
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if err := validate.FileReference(file.DownloadURL, file.FileID); err != nil {
		return nil, err
	}
	return &assets.ChatGPTFileInput{
		DownloadURL: file.DownloadURL,
		FileID:      file.FileID,
		MimeType:    file.MimeType,
		FileName:    file.FileName,
	}, nil
}

func decodeArgs(req *mcp.CallToolRequest, target any) error {
	if len(req.Params.Arguments) == 0 {
		return nil
	}
	if err := json.Unmarshal(req.Params.Arguments, target); err != nil {
		return err
	}
	return nil
}

func result(text string, structured map[string]any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: text}},
		StructuredContent: structured,
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if required != nil {
		schema["required"] = required
	}
	return schema
}

func annotations(readOnly bool, destructive bool, openWorld bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    readOnly,
		DestructiveHint: &destructive,
		OpenWorldHint:   &openWorld,
	}
}
