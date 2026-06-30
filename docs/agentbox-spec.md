# Agentbox Specification

## Overview

Agentbox is a small message relay service for exchanging threaded messages between ChatGPT, local coding agents, and the user.

The main use case is to avoid copying and pasting long design discussions, implementation instructions, generated assets, or agent replies between ChatGPT and local tools such as Codex or Claude Code.

Agentbox provides one shared place where ChatGPT and local agents can post and read messages in the same thread.

## Product Name

**Agentbox**

## Core Concept

Agentbox has three core resources:

```text
Thread
  └── Messages
        └── Optional Assets
```

A thread represents a conversation or topic.

A message is an append-only entry inside a thread. Message bodies may be plain text or Markdown; when the caller does not specify a format, Agentbox infers one from the body and source file name.

An asset is a file attached to a message, such as an image, PDF, ZIP, Markdown file, or other artifact.

Agentbox exposes the same simple operations through:

1. A remote MCP server for ChatGPT
2. A local CLI for the user and local agents

## Primary User Flow

1. The user has a long discussion with ChatGPT.
2. The user asks ChatGPT to send the agreed instructions to Agentbox.
3. ChatGPT creates or updates a thread using the Agentbox MCP server.
4. The user or a local coding agent reads the thread using the Agentbox CLI.
5. The local agent performs the work or asks a clarification question.
6. The local agent posts a reply back to the same Agentbox thread.
7. The user asks ChatGPT to check the thread and respond if needed.

## Asset Flow

Agentbox supports sending files and generated assets through the same thread/message model.

Example asset types:

```text
PNG
JPEG
WebP
GIF
PDF
Markdown
TXT
JSON
ZIP
```

A message may include an attached asset.

Example flow for a generated image:

```text
ChatGPT generates an image
  -> user asks ChatGPT to post it to Agentbox
  -> ChatGPT calls the Agentbox MCP tool with the image as a file parameter
  -> Agentbox downloads the temporary file URL
  -> Agentbox stores the bytes in object storage
  -> Agentbox saves asset metadata in Postgres
  -> Agentbox attaches the asset to the message
```

Example flow for a local agent artifact:

```text
Local agent creates a file
  -> local agent calls the Agentbox CLI
  -> CLI uploads the file to Agentbox
  -> Agentbox stores the bytes in object storage
  -> Agentbox saves asset metadata in Postgres
  -> Agentbox attaches the asset to the message
```

## MCP Tools

Agentbox exposes five MCP tools.

### `list_threads`

Returns recent threads.

Purpose:

```text
Let ChatGPT see available Agentbox conversations.
Let the user choose an existing thread when needed.
```

Example result:

```json
{
  "threads": [
    {
      "id": "thr_123",
      "title": "Agentbox design",
      "created_at": "2026-06-28T10:00:00Z",
      "updated_at": "2026-06-28T10:15:00Z"
    }
  ]
}
```

### `search_threads`

Searches thread titles and message bodies.

Input:

```json
{
  "query": "deployment",
  "limit": 10,
  "created_by": "chatgpt",
  "updated_after": "2026-06-28T00:00:00Z"
}
```

Output:

```json
{
  "threads": [
    {
      "id": "thr_123",
      "title": "Agentbox deployment",
      "created_at": "2026-06-28T10:00:00Z",
      "updated_at": "2026-06-28T10:15:00Z",
      "created_by": "chatgpt",
      "message_count": 3,
      "last_message_preview": "Deployment checks passed.",
      "matched_snippets": ["Agentbox deployment"]
    }
  ]
}
```

### `get_thread`

Returns a thread, its messages, and attached asset metadata.

Input:

```json
{
  "thread_id": "thr_123"
}
```

Output:

```json
{
  "thread": {
    "id": "thr_123",
    "title": "Agentbox design",
    "messages": [
      {
        "id": "msg_001",
        "author": "chatgpt",
        "body": "Here is the implementation plan...",
        "body_content_type": "text/markdown",
        "created_at": "2026-06-28T10:05:00Z",
        "assets": [
          {
            "id": "asset_001",
            "file_name": "homepage-banner.png",
            "filename": "homepage-banner.png",
            "mime_type": "image/png",
            "size_bytes": 124000,
            "public_url": "https://assets.example.com/agentbox/homepage-banner.png",
            "download_url": "https://assets.example.com/agentbox/homepage-banner.png"
          }
        ]
      }
    ]
  }
}
```

### `create_thread`

Creates a new thread. Callers may include `initial_message` to create the first message in the same call. The initial message uses automatic Markdown/plain-text detection unless `body_content_type` is set to `text/markdown` or `text/plain`.

Input:

```json
{
  "title": "Agentbox design",
  "initial_message": "Please implement the attached design.",
  "body_content_type": "text/markdown"
}
```

Output:

```json
{
  "thread": {
    "id": "thr_123",
    "title": "Agentbox design",
    "created_at": "2026-06-28T10:00:00Z"
  },
  "message": {
    "id": "msg_001",
    "thread_id": "thr_123",
    "body": "Please implement the attached design.",
    "body_content_type": "text/markdown",
    "created_at": "2026-06-28T10:00:00Z"
  }
}
```

### `post_message`

Posts a message to an existing thread. The body format defaults to automatic detection. Callers may set `body_content_type` to `text/markdown` or `text/plain` when they know how the dashboard should render the body.

Input:

```json
{
  "thread_id": "thr_123",
  "body": "Here is the task summary...",
  "body_content_type": "text/markdown"
}
```

Output:

```json
{
  "message": {
    "id": "msg_002",
    "thread_id": "thr_123",
    "author": "chatgpt",
    "body_content_type": "text/markdown",
    "created_at": "2026-06-28T10:15:00Z"
  }
}
```

For ChatGPT file uploads, `post_message` may also accept an optional top-level file parameter.

`body_content_type` is optional. Omit it or pass `auto` to use smart detection. Agentbox marks `.md` / `.markdown` files, Markdown tables, fenced code blocks, and Mermaid blocks as `text/markdown`; short replies and log-like text stay `text/plain`.

Input:

```json
{
  "thread_id": "thr_123",
  "body": "Here is the generated image.",
  "file": {
    "download_url": "https://temporary-download-url.example.com/file",
    "file_id": "file_abc123",
    "mime_type": "image/png",
    "file_name": "generated-image.png"
  }
}
```

The file parameter is resolved by the ChatGPT Apps SDK runtime before the MCP server receives the tool call.

## CLI

The Agentbox CLI exposes the same basic operations as the MCP tools.

Command name:

```bash
agentbox
```

### Environment

```bash
export AGENTBOX_BASE_URL="https://your-agentbox.vercel.app"
export AGENTBOX_API_KEY="your-client-key"
```

### List threads

```bash
agentbox list
```

### Create thread

```bash
agentbox create "Agentbox design"
```

### Get thread

```bash
agentbox get thr_123
```

### Post message

```bash
agentbox post thr_123 "Message body"
```

For longer Markdown messages:

```bash
agentbox post thr_123 --file message.md
```

The CLI defaults to automatic body format detection. Use `--format plain` for raw logs, `--format markdown` for forced Markdown, or the aliases `--plain` and `--markdown`.

```bash
agentbox post thr_123 --file raw-output.txt --format plain
```

For posting a message with an attached asset:

```bash
agentbox post thr_123 --file message.md --asset image.png
```

For posting only an asset with a short message:

```bash
agentbox post thr_123 "Generated homepage banner." --asset banner.png
```

## Message Format

Messages store a raw `body` plus an optional `body_content_type` render hint. New posts resolve the hint to either `text/plain` or `text/markdown`; older messages without the hint are handled by the dashboard fallback detector.

Markdown gives enough structure for useful handoffs without requiring complex schemas. The dashboard renders common Markdown, GFM tables, fenced code blocks, and fenced Mermaid diagrams. It also exposes copy buttons and a source/rendered toggle so agents can still inspect the exact raw text.

Example message:

```markdown
# Request

Please implement the Agentbox MVP.

# Context

Agentbox is a small message relay between ChatGPT and local coding agents.

# Requirements

- Implement the remote MCP server.
- Implement the matching CLI.
- Support creating threads, listing threads, reading threads, and posting messages.
- Store messages durably.

# Response

Please reply in this same thread with a summary of what changed.
```

## Data Model

### `threads`

```text
id
title
created_at
updated_at
created_by
```

### `messages`

```text
id
thread_id
author
body
body_content_type
created_at
```

### `assets`

```text
id
message_id
storage_key
file_name
mime_type
size_bytes
public_url
created_at
created_by
```

## Storage

Agentbox uses Postgres for metadata and object storage for file bytes.

### Postgres

Postgres stores:

```text
threads
messages
assets
```

### Object Storage

Object storage stores the actual asset bytes.

Recommended provider:

```text
Cloudflare R2
```

R2 should store:

```text
generated images
uploaded images
PDFs
ZIPs
Markdown files
local agent artifacts
```

Suggested object key format:

```text
agentbox/{thread_id}/{message_hint}/{asset_id}-{safe_file_name}
```

Example:

```text
agentbox/thr_123/msg_456/asset_789-homepage-banner.png
```

## Authentication

Agentbox authenticates requests with a `key` query parameter.

Each client should have its own key:

```text
chatgpt-key
local-machine-key
admin-key
```

The key determines the author shown on messages.

Example authors:

```text
chatgpt
ashray-macbook
codex-local
claude-code-local
```

API keys are stored in Postgres and managed through the backend admin API.

Clients authenticate by putting the key directly in the endpoint URL:

```text
https://your-agentbox.vercel.app/api/mcp?key=CHATGPT_KEY
```

Key records are keyed by name:

```text
chatgpt
local
codex-local
```

The key name is also used as the actor shown on threads and messages. Create and revoke keys with the CLI:

```bash
agentbox keys create chatgpt --admin-key "$AGENTBOX_ADMIN_KEY"
agentbox keys list --admin-key "$AGENTBOX_ADMIN_KEY"
agentbox keys revoke chatgpt --admin-key "$AGENTBOX_ADMIN_KEY"
```

Keys should be stored securely and should not be committed to source control.

## Suggested Stack

### Server

```text
TypeScript
Next.js route handlers
Vercel
MCP TypeScript SDK
PostgreSQL
Cloudflare R2
AWS SDK v3 for S3-compatible R2 access
```

### CLI

```text
TypeScript
Node.js
Commander
```

The CLI is packaged as the `agentbox` binary.

## Deployment

Agentbox is deployed as a Vercel app.

Recommended deployment shape:

```text
Vercel
├── Next.js app
├── REST API routes
└── MCP route at /api/mcp

Postgres
└── Agentbox metadata

Cloudflare R2
└── Asset storage
```

## API Shape

The remote server exposes both MCP and simple HTTP endpoints.

```text
POST /api/mcp
GET  /api/mcp

GET  /api/threads
POST /api/threads
GET  /api/threads/:thread_id
POST /api/threads/:thread_id/messages
```

The MCP tools use the same underlying service logic as the HTTP API.

## Configuration

Server environment variables:

```text
DATABASE_URL
AGENTBOX_ADMIN_KEY
AGENTBOX_ALLOWED_ORIGINS
AGENTBOX_MAX_FILE_SIZE_BYTES

R2_ACCOUNT_ID
R2_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY
R2_BUCKET
R2_PUBLIC_BASE_URL
```

CLI environment variables:

```text
AGENTBOX_BASE_URL
AGENTBOX_API_KEY
```

## Asset Handling Requirements

When Agentbox receives a file, it should:

```text
Download the file during the request
Validate file size
Validate or record MIME type
Sanitize the filename
Generate a safe storage key
Upload bytes to object storage
Store asset metadata in Postgres
Attach the asset to the message
Return the message and asset metadata
```

Agentbox stores permanent object storage URLs or keys, not temporary ChatGPT download URLs.

## MVP Goal

The first working version should support this complete loop:

```text
ChatGPT posts a message to Agentbox
ChatGPT optionally attaches a generated image or file
Agentbox stores the message in Postgres
Agentbox stores the asset in R2
Local CLI reads the thread
Local agent posts a reply
Local agent optionally attaches a file
ChatGPT reads the reply
```

Once this loop works reliably, Agentbox is useful.

### ChatGPT file attachments

For MCP file uploads from ChatGPT, the `post_message.file` argument should be the uploaded ChatGPT conversation file ID, for example `file_abc123`. Do not pass a local sandbox path such as `/mnt/data/example.md` or a plain filename. The Apps SDK marks `file` with `_meta["openai/fileParams"]`, so ChatGPT expands the file ID before the server handler runs; the server receives `{ download_url, file_id, mime_type?, file_name? }` and immediately persists the bytes to R2.
