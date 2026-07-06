package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
	"agentbox/internal/agentbox/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	auth types.AuthContext
	svc  *service.Service
}

func New(auth types.AuthContext, svc *service.Service) *mcp.Server {
	builder := &Server{auth: auth, svc: svc}
	return builder.build()
}

func NewHTTPHandler(auth types.AuthContext, svc *service.Service) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server {
			return New(auth, svc)
		},
		&mcp.StreamableHTTPOptions{
			Stateless:                  true,
			JSONResponse:               true,
			SessionTimeout:             0,
			DisableLocalhostProtection: true,
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
		Name:        "search_threads",
		Title:       "Search threads",
		Description: "Search Agentbox threads by keyword across titles and message bodies.",
		InputSchema: objectSchema(map[string]any{
			"query":         map[string]any{"type": "string", "minLength": 1},
			"limit":         map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			"created_by":    map[string]any{"type": "string", "minLength": 1},
			"updated_after": map[string]any{"type": "string", "format": "date-time"},
		}, []string{"query"}),
		OutputSchema: objectSchema(map[string]any{
			"threads": map[string]any{"type": "array", "items": map[string]any{}},
		}, []string{"threads"}),
		Annotations: annotations(true, false, false),
	}, s.searchThreads)
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
		Description: "Create a new Agentbox thread. Optionally include initial_message to create the first message in the same call.",
		InputSchema: objectSchema(map[string]any{
			"title":             map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
			"initial_message":   map[string]any{"type": "string"},
			"body_content_type": map[string]any{"type": "string", "enum": []string{"auto", "text/plain", "text/markdown"}},
		}, []string{"title"}),
		OutputSchema: objectSchema(map[string]any{
			"thread":  map[string]any{},
			"message": map[string]any{},
		}, []string{"thread"}),
		Annotations: annotations(false, false, true),
	}, s.createThread)
	server.AddTool(&mcp.Tool{
		Meta:        mcp.Meta{"openai/fileParams": []string{"file"}, "openai/toolInvocation/invoking": "Posting to Agentbox…", "openai/toolInvocation/invoked": "Posted to Agentbox"},
		Name:        "post_message",
		Title:       "Post message",
		Description: "Post a message to an Agentbox thread. Messages default to auto-detected Markdown/plain rendering; set body_content_type to text/markdown or text/plain when you know the format. To attach a file from ChatGPT, pass the uploaded conversation file ID, for example file_abc123. Do not pass a local filesystem path or plain filename.",
		InputSchema: objectSchema(map[string]any{
			"thread_id":         map[string]any{"type": "string", "minLength": 1},
			"body":              map[string]any{"type": "string"},
			"body_content_type": map[string]any{"type": "string", "enum": []string{"auto", "text/plain", "text/markdown"}},
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
		return errorResult(err), nil
	}
	limit := 0
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit > 100 {
		limit = 100
	}
	threads, err := s.svc.ListThreads(ctx, s.auth, limit)
	if err != nil {
		return errorResult(err), nil
	}
	return result("Listed Agentbox threads.", map[string]any{"threads": threads}), nil
}

func (s *Server) searchThreads(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		Query        string  `json:"query"`
		Limit        *int    `json:"limit"`
		CreatedBy    *string `json:"created_by"`
		UpdatedAfter *string `json:"updated_after"`
	}
	if err := decodeArgs(req, &input); err != nil {
		return errorResult(err), nil
	}
	limit := 0
	if input.Limit != nil {
		limit = *input.Limit
	}
	threads, err := s.svc.SearchThreads(ctx, s.auth, types.SearchThreadParams{
		Query:        input.Query,
		Limit:        limit,
		CreatedBy:    input.CreatedBy,
		UpdatedAfter: input.UpdatedAfter,
	})
	if err != nil {
		return errorResult(err), nil
	}
	return result("Searched Agentbox threads.", map[string]any{"threads": threads}), nil
}

func (s *Server) getThread(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		ThreadID string `json:"thread_id"`
	}
	if err := decodeArgs(req, &input); err != nil {
		return errorResult(err), nil
	}
	if err := validate.ThreadID(input.ThreadID); err != nil {
		return errorResult(err), nil
	}
	thread, err := s.svc.GetThread(ctx, s.auth, input.ThreadID)
	if err != nil {
		return errorResult(err), nil
	}
	return result("Fetched Agentbox thread.", map[string]any{"thread": thread}), nil
}

func (s *Server) createThread(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		Title           string  `json:"title"`
		InitialMessage  *string `json:"initial_message"`
		BodyContentType *string `json:"body_content_type"`
	}
	if err := decodeArgs(req, &input); err != nil {
		return errorResult(err), nil
	}
	if input.InitialMessage != nil {
		thread, message, err := s.svc.CreateThreadWithMessage(ctx, s.auth, input.Title, *input.InitialMessage, input.BodyContentType)
		if err != nil {
			return errorResult(err), nil
		}
		return result("Created Agentbox thread with initial message.", map[string]any{"thread": thread, "message": message}), nil
	}
	thread, err := s.svc.CreateThread(ctx, s.auth, input.Title)
	if err != nil {
		return errorResult(err), nil
	}
	return result("Created Agentbox thread.", map[string]any{"thread": thread}), nil
}

func (s *Server) postMessage(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var raw struct {
		ThreadID        string          `json:"thread_id"`
		Body            string          `json:"body"`
		BodyContentType *string         `json:"body_content_type"`
		File            json.RawMessage `json:"file"`
	}
	if err := decodeArgs(req, &raw); err != nil {
		return errorResult(err), nil
	}
	var file *assets.ChatGPTFileInput
	if len(raw.File) > 0 && string(raw.File) != "null" {
		parsed, err := parseFileInput(raw.File)
		if err != nil {
			return errorResult(err), nil
		}
		file = parsed
	}
	message, err := s.svc.PostMessage(ctx, s.auth, service.PostMessageParams{
		ThreadID:        raw.ThreadID,
		Body:            raw.Body,
		BodyContentType: raw.BodyContentType,
		File:            file,
	})
	if err != nil {
		return errorResult(err), nil
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
	payload := jsonText(structured)
	return &mcp.CallToolResult{
		Meta:              mcp.Meta{"agentbox/status": text},
		Content:           []mcp.Content{&mcp.TextContent{Text: payload}},
		StructuredContent: structured,
	}
}

func errorResult(err error) *mcp.CallToolResult {
	payload := errorPayload(err)
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: jsonText(payload)}},
		StructuredContent: payload,
		IsError:           true,
	}
}

func jsonText(value map[string]any) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		return `{"error":{"code":"INTERNAL_ERROR","message":"Failed to encode Agentbox MCP result."}}`
	}
	return string(bytes)
}

func errorPayload(err error) map[string]any {
	code := "INTERNAL_ERROR"
	message := "Agentbox could not complete the tool call."
	var coded service.CodedError
	if errors.As(err, &coded) {
		code = coded.Code
		message = coded.Message
	} else if errors.Is(err, service.ErrThreadNotFound) {
		code = "THREAD_NOT_FOUND"
		message = service.ErrThreadNotFound.Error()
	} else if errors.Is(err, messageformat.ErrInvalidContentType) {
		code = "INVALID_ARGUMENT"
		message = err.Error()
	} else if isInvalidArgument(err) {
		code = "INVALID_ARGUMENT"
		message = err.Error()
	}
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
}

func isInvalidArgument(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "too_small") ||
		strings.Contains(message, "too_big") ||
		strings.Contains(message, "invalid_format") ||
		strings.Contains(message, "Too small: expected string") ||
		message == "download_url and file_id are required" ||
		message == "body_content_type must be text/plain, text/markdown, or auto" ||
		strings.HasPrefix(message, "File was received as a plain string.")
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
