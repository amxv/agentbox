# Claude.ai MCP Structured Results Research

Date: 2026-07-01

## Executive Summary

Agentbox's MCP tools are spec-shaped but not currently self-sufficient for Claude.ai custom connectors. The server advertises `outputSchema` and returns useful data in `structuredContent`, but the model-visible `content` field is only a short confirmation string such as `Fetched Agentbox thread.` or `Listed Agentbox threads.`.

That works in clients that expose `structuredContent` to the model or app runtime, and it can appear to work well in ChatGPT. It fails in Claude.ai custom remote MCP connectors because Claude's documented MCP result block exposes `content`, and field reports show Claude.ai/custom connector behavior often ignores or does not model-surface `structuredContent`. The practical fix is to make `content[0].text` contain the same payload as JSON while preserving `structuredContent` and `outputSchema`.

Recommended Agentbox change:

```json
{
  "structuredContent": {
    "thread": { "...": "..." }
  },
  "content": [
    {
      "type": "text",
      "text": "{\"thread\":{\"...\":\"...\"}}"
    }
  ]
}
```

For human readability, Agentbox can use a two-block response, but the JSON block must be present and obvious:

```json
{
  "content": [
    { "type": "text", "text": "Fetched Agentbox thread." },
    { "type": "text", "text": "{\"thread\":{\"...\":\"...\"}}" }
  ],
  "structuredContent": {
    "thread": { "...": "..." }
  }
}
```

The first form is safer for clients that only read the first text block.

## What Agentbox Does Today

Source inspected: `internal/agentbox/mcpserver/mcpserver.go`.

Agentbox registers four MCP tools:

- `list_threads`
- `get_thread`
- `create_thread`
- `post_message`

Each tool declares `OutputSchema`:

- `list_threads` declares `{ "threads": [...] }`
- `get_thread` declares `{ "thread": ... }`
- `create_thread` declares `{ "thread": ... }`
- `post_message` declares `{ "message": ... }`

The handlers then call a shared helper:

```go
func result(text string, structured map[string]any) *mcp.CallToolResult {
    return &mcp.CallToolResult{
        Content:           []mcp.Content{&mcp.TextContent{Text: text}},
        StructuredContent: structured,
    }
}
```

The actual tool responses therefore look like:

```json
{
  "content": [{ "type": "text", "text": "Fetched Agentbox thread." }],
  "structuredContent": {
    "thread": {
      "id": "...",
      "messages": [...]
    }
  }
}
```

This is the proximate cause. Claude.ai sees only the confirmation text when it does not pass `structuredContent` into model context.

Relevant source references:

- Tool output schemas: `internal/agentbox/mcpserver/mcpserver.go:52`, `:64`, `:76`, `:107`
- Confirmation-only calls: `internal/agentbox/mcpserver/mcpserver.go:133`, `:150`, `:164`, `:194`
- Shared result helper: `internal/agentbox/mcpserver/mcpserver.go:227`

## MCP Spec Expectations

The current MCP tools spec says:

- Tool definitions may include `outputSchema`.
- Structured data goes in `structuredContent`.
- If `outputSchema` is present, the server must return structured results conforming to it.
- For backward compatibility, a tool returning structured content should also return serialized JSON in a `TextContent` block.

The latest MCP spec's canonical structured-output example returns both:

```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"temperature\": 22.5, \"conditions\": \"Partly cloudy\", \"humidity\": 65}"
    }
  ],
  "structuredContent": {
    "temperature": 22.5,
    "conditions": "Partly cloudy",
    "humidity": 65
  }
}
```

Agentbox currently satisfies the native `structuredContent` half but not the compatibility `content` half.

Sources:

- MCP 2025-11-25 Tools spec: https://modelcontextprotocol.io/specification/latest/server/tools
- MCP 2025-06-18 Tools spec: https://modelcontextprotocol.io/specification/2025-06-18/server/tools

## Claude.ai Behavior

Official Claude docs for custom remote MCP connectors confirm that Claude.ai can connect to third-party remote MCP servers, but they do not document tool-result `structuredContent` as a model-visible field in Claude.ai chat.

The Claude API MCP connector docs are more concrete. They show that when Claude uses an MCP tool, the response includes an `mcp_tool_result` block shaped as:

```json
{
  "type": "mcp_tool_result",
  "tool_use_id": "...",
  "is_error": false,
  "content": [
    { "type": "text", "text": "Hello" }
  ]
}
```

Notably, that documented `mcp_tool_result` response shape includes `content` but not `structuredContent`.

This does not prove the backend never reads `structuredContent` internally, but it strongly supports the observed symptom: Claude.ai/custom connectors may not surface `structuredContent` to the model. If `content` is only "Fetched Agentbox thread.", Claude cannot reason over the thread body, IDs, messages, or attachment references.

Field evidence matches this:

- An OpenClaw issue from March-May 2026 reports the same pattern: MCP tools returned useful payloads in `structuredContent` with only count/status text in `content`; Cursor and Claude.ai only saw `content`, making the tools unusable.
- The landed fix in that project was to make text `content` self-sufficient while preserving `structuredContent`.

Sources:

- Claude remote MCP custom connector docs: https://claude.com/docs/connectors/custom/remote-mcp
- Claude Platform MCP connector docs: https://platform.claude.com/docs/en/agents-and-tools/mcp-connector
- OpenClaw issue: https://github.com/openclaw/openclaw/issues/57461

## ChatGPT Behavior

ChatGPT appears more tolerant in two ways:

- ChatGPT Apps SDK documents `structuredContent` as visible to the model and component, with `_meta` reserved for widget-only hidden data.
- ChatGPT's widget runtime exposes `window.openai.toolOutput` as the returned `structuredContent`.

However, OpenAI's own MCP docs still recommend duplicating the same value as JSON text in `content` for compatibility. The data-only MCP guide explicitly says `search` and `fetch` should return the object as `structuredContent` and include the same value as a JSON-encoded string in `content`.

So ChatGPT compatibility does not argue against changing Agentbox. It argues for keeping `structuredContent` while adding JSON text in `content`.

Sources:

- OpenAI MCP server guide for ChatGPT/API integrations: https://developers.openai.com/api/docs/mcp
- OpenAI Apps SDK MCP server docs: https://developers.openai.com/apps-sdk/build/mcp-server
- OpenAI Apps SDK reference: https://developers.openai.com/apps-sdk/reference

## Likely Root Cause

The same Agentbox MCP response is interpreted differently by different MCP hosts:

| Host/client | Likely behavior | Effect with current Agentbox |
| --- | --- | --- |
| ChatGPT Apps / Apps SDK | Can use `structuredContent`; may expose it to model/widget | Works or mostly works |
| Claude.ai custom remote connector | Model-visible result appears to be `content`-centric | Claude sees only confirmation strings |
| Generic MCP clients | Many read `content`; some validate/use `structuredContent` | Mixed; content-only clients fail |

This is not an authentication, transport, or tool discovery issue. Claude is successfully invoking the tools. The problem is response payload placement.

## Recommended Agentbox Changes

### 1. Make `content` self-sufficient

Change the shared MCP result helper so `content[0].text` is `json.Marshal(structured)` instead of only the English confirmation string.

Current:

```json
{
  "content": [{ "type": "text", "text": "Listed Agentbox threads." }],
  "structuredContent": { "threads": [...] }
}
```

Recommended:

```json
{
  "content": [{ "type": "text", "text": "{\"threads\":[...]}" }],
  "structuredContent": { "threads": [...] }
}
```

This is the most compatible option because clients that read only the first content block still get parseable data.

### 2. Preserve `structuredContent`

Do not remove `structuredContent`. It is the native structured-output field, validates against `outputSchema`, helps ChatGPT/widgets, and future-proofs Agentbox for clients that consume structured results correctly.

### 3. Preserve `outputSchema`, but make it more specific later

Agentbox currently uses very loose object schemas like:

```go
"thread": map[string]any{}
```

That probably validates broadly, but it gives clients little guidance. A follow-up could define real schemas for `Thread`, `Message`, and `Asset`:

- `id`
- `title`
- `created_at`
- `updated_at`
- `created_by`
- `messages`
- `assets`
- `download_url` / `preview_url` where applicable

This is not required to fix Claude.ai. It would improve schema-aware clients.

### 4. Avoid relying on annotations for output visibility

Tool annotations such as `readOnlyHint`, `destructiveHint`, and `openWorldHint` influence approval and trust UX. They do not solve the output visibility problem. Agentbox already uses annotations; the missing piece is JSON text in `content`.

### 5. Add a compatibility test

Add a focused test that calls each MCP tool and asserts:

- `StructuredContent` contains the expected top-level key.
- First `TextContent.Text` is valid JSON.
- The decoded JSON has the same top-level key and value shape as `StructuredContent`.

Example assertion shape:

```go
text := res.Content[0].(*mcp.TextContent).Text
var fallback map[string]any
if err := json.Unmarshal([]byte(text), &fallback); err != nil {
    t.Fatalf("content text is not JSON: %v", err)
}
if _, ok := fallback["thread"]; !ok {
    t.Fatalf("content JSON missing thread")
}
```

## Compatibility Considerations

### Token and payload size

Duplicating structured data into `content` increases model-visible tokens. For Agentbox's use case, that is the point: Claude must see thread/message data to use it. If thread payloads become large, implement bounded output at the service/tool layer rather than hiding the payload only in `structuredContent`.

Possible future controls:

- `get_thread` input options for `limit_messages`, `include_assets`, or `max_body_chars`.
- Keep attachment binary data out of JSON; use asset metadata and signed download URLs only.
- For very large exports, return a summary plus a downloadable resource link.

### Human-readable status text

If Agentbox wants to keep confirmation strings, append them after the JSON block rather than before it, or include a `_summary` field inside the JSON. First-block JSON is safer because many clients only inspect the first text item.

### ChatGPT file metadata

The existing `openai/fileParams` metadata for `post_message` should remain. It is orthogonal to this issue.

## Proposed Implementation Shape

The minimal code change would be in the shared helper:

```go
func result(text string, structured map[string]any) *mcp.CallToolResult {
    bytes, err := json.Marshal(structured)
    fallback := text
    if err == nil {
        fallback = string(bytes)
    }
    return &mcp.CallToolResult{
        Content:           []mcp.Content{&mcp.TextContent{Text: fallback}},
        StructuredContent: structured,
    }
}
```

If preserving the confirmation is important for ChatGPT's UI, consider adding it as a second text block or as metadata, but the first text block should remain JSON.

## Bottom Line

Agentbox should treat `structuredContent` as the native machine-readable channel and `content[0].text` as the cross-client compatibility channel. Today, `content` is not self-sufficient, so Claude.ai receives only plain confirmations. Mirroring the structured payload into `content` as JSON is the spec-recommended, ChatGPT-compatible, Claude-compatible fix.
