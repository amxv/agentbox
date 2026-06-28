# Agentbox Go Port Implementation Plan

Date: 2026-06-29

## State of Current System

Agentbox is currently a TypeScript/Next.js project with three major surfaces:

- A Vercel-hosted Next.js API and web UI.
- A Streamable HTTP MCP server exposed at `/api/mcp`.
- A bundled Node CLI exposed as `agentbox`.

Current source evidence:

- `package.json` defines Next.js 16, React 19, TypeScript, `@modelcontextprotocol/sdk`, AWS S3 SDK, `postgres`, `commander`, and `mime-types`; it builds the web app with `next build` and the CLI with `tsup src/cli/index.ts` (`package.json`).
- `next.config.ts` has no custom configuration.
- `migrations/0001_init.sql` creates `threads`, `messages`, and `assets`, with indexes on `threads.updated_at`, `messages(thread_id, created_at)`, and `assets.message_id`.
- `src/core/types.ts` defines the JSON contract for `Thread`, `Message`, `Asset`, `ThreadWithMessages`, `Actor`, and `ChatGPTFileReference`.
- `src/core/schemas.ts` enforces `title` length, `post_message` defaults, and ChatGPT file reference shape.
- `src/core/auth.ts` parses `AGENTBOX_API_KEYS` as JSON or comma-separated `name:key:author`, authenticates using the URL query parameter `key`, uses timing-safe comparison, permits local dev without keys outside production, and validates optional `AGENTBOX_ALLOWED_ORIGINS`.
- `src/core/admin.ts` parses `AGENTBOX_ADMIN_KEYS` or legacy `AGENTBOX_ADMIN_KEY`, accepts `x-agentbox-admin-key` or bearer auth for the viewer API, and permits local dev without admin keys outside production.
- `src/core/db.ts` lazily opens a PostgreSQL client from `DATABASE_URL`, caps pool size with `AGENTBOX_DB_POOL_SIZE` defaulting to `3`, lazily ensures schema, generates `thr_`, `msg_`, and `asset_` IDs with UUIDs, normalizes timestamps to ISO strings, and stores one optional asset per posted message.
- `src/core/assets.ts` stores attachments in Cloudflare R2 through the S3 API, requires `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, and `R2_BUCKET`, optionally emits public URLs from `R2_PUBLIC_BASE_URL`, sanitizes filenames, infers MIME types, enforces `AGENTBOX_MAX_FILE_SIZE_BYTES` defaulting to 25 MiB, downloads ChatGPT file references, and generates signed download URLs.
- `src/core/handlers.ts` is the shared service layer for list/create/get/post thread operations and file upload flow.
- `src/core/mcp.ts` registers MCP tools `list_threads`, `get_thread`, `create_thread`, and `post_message`; `post_message` has OpenAI file parameter metadata and returns both text content and structured content.
- `app/api/mcp/route.ts` creates a new MCP server per authenticated request and serves GET, POST, and DELETE through `WebStandardStreamableHTTPServerTransport` with `enableJsonResponse: true` and no session ID generator.
- `app/api/health/route.ts` returns `{ ok: true, service: "agentbox" }`.
- `app/api/threads/route.ts` implements authenticated `GET /api/threads?limit=N` and `POST /api/threads`.
- `app/api/threads/[threadId]/route.ts` implements authenticated `GET /api/threads/{threadId}`.
- `app/api/threads/[threadId]/messages/route.ts` implements authenticated `POST /api/threads/{threadId}/messages` for JSON and multipart form data.
- `app/api/assets/[assetId]/download-url/route.ts` implements authenticated signed URL creation with `expires_in` clamped to 60..3600 seconds.
- `app/api/viewer/threads/route.ts` implements admin-authenticated viewer thread listing with limit clamped to 1..200.
- `app/api/viewer/threads/[threadId]/route.ts` implements admin-authenticated viewer thread detail and adds signed `download_url` and image `preview_url` fields, using 900 second expiry for images and 300 seconds for other assets.
- `app/components/viewer-profiles.ts`, `app/threads/inbox-view.tsx`, and `app/threads/[threadId]/thread-view.tsx` implement a browser-local admin-key viewer profile system and read-only inbox UI.
- `src/core/profiles.ts` implements CLI profile storage at `AGENTBOX_CONFIG_DIR` or OS-specific defaults, supports `AGENTBOX_PROFILES`, `AGENTBOX_PROFILE`, `AGENTBOX_BASE_URL` / `AGENTBOX_URL`, and `AGENTBOX_API_KEY`, writes `profiles.json` with mode `0600`, masks API keys, and sanitizes URLs.
- `src/cli/index.ts` implements CLI commands `profiles`, `profiles add`, `profiles remove`, `profiles use`, `profiles show`, `doctor`, `list`, `create`, `get`, `download`, and `post`.
- `src/scripts/migrate.ts` runs `ensureSchema()` and closes the DB.

Current Vercel Go runtime evidence:

- Vercel Go runtime docs: https://vercel.com/docs/functions/runtimes/go
- Vercel runtimes docs: https://vercel.com/docs/functions/runtimes
- Vercel project configuration docs: https://vercel.com/docs/project-configuration/vercel-json
- Vercel function limits docs: https://vercel.com/docs/functions/limitations

Important Vercel constraints from those docs:

- The Go runtime is beta and available on all plans.
- The Go framework preset detects a root `go.mod` and one of `main.go`, `cmd/api/main.go`, or `cmd/server/main.go`; the server must listen on `PORT`.
- Running a Go server requires the project framework preset to be `go`.
- `go.mod` must be at the project root; Vercel reads the Go version from `go.mod`, or from `toolchain` if present.
- Dependencies belong in `go.mod`; commit `go.sum` when it exists.
- `GO_BUILD_FLAGS` can customize default `go build`; the default strips debug info with `-ldflags "-s -w"`.
- `buildCommand` can replace the default build, and a custom output can be copied to `$VERCEL_OUTPUT_FILE`.
- Go serverless functions are also possible from `.go` files in `/api`, each exporting an `http.HandlerFunc`.
- To deploy a Go server alongside a frontend such as Next.js within the same project, Vercel says to use Services.
- Vercel Functions run in a single region by default, `iad1`, and region selection should be near the database.
- Function bundle size is capped at 250 MB uncompressed.
- Function request or response payload size is capped at 4.5 MB; this conflicts with the current API's 25 MiB direct multipart and ChatGPT download/upload expectation if files pass through Vercel functions.
- Runtime filesystem is read-only except `/tmp`, which is suitable only for scratch space.

Current Go MCP SDK evidence:

- Official Go SDK repository: https://github.com/modelcontextprotocol/go-sdk
- Go package docs: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
- The SDK is the official Go MCP SDK, exposes `mcp.NewServer`, generic `mcp.AddTool`, `ToolAnnotations`, `TextContent`, and `NewStreamableHTTPHandler`.
- `NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions)` serves Streamable HTTP MCP sessions and is the best fit for parity with the current TypeScript Streamable HTTP route.

## State of Ideal System

The target system should be a split-runtime Agentbox with the same externally visible behavior:

- A Go backend owns the HTTP API, MCP server, database access, R2 attachment handling, migrations, and CLI.
- The web dashboard remains Next.js. Do not port the landing page, inbox dashboard, thread detail page, or browser-local viewer profile UI to Go unless a later task explicitly changes that scope.
- API response JSON, IDs, timestamp shapes, error status codes, query parameters, environment variables, and CLI output remain backward compatible.
- The MCP endpoint remains `/api/mcp?key=...`, supports GET/POST/DELETE, validates origin when configured, authenticates with the same actor model, exposes the same four tools, preserves OpenAI file parameter metadata where supported by the Go SDK, and returns structured content compatible with current clients.
- PostgreSQL schema remains compatible with the existing `threads`, `messages`, and `assets` tables so existing deployments can be rolled forward without data migration.
- R2 attachment storage keeps the same storage key format, signed download semantics, public URL behavior, filename sanitization, MIME inference, and size checks, while addressing Vercel's 4.5 MB request/response limit.
- CLI profiles continue to read and write the same config file format and path rules so users do not have to reconfigure.
- CLI commands preserve current names, flags, output shape, JSON mode, exit-code behavior, stdin handling, file body handling, multipart asset upload behavior where supported, and download behavior.
- Deployment target: Go backend deployed as a Vercel Go Service and Next.js dashboard deployed as the frontend service. Use Vercel Services or equivalent rewrites/routing so the dashboard can call the Go API under the same public product URL.

Recommendation: implement the Go backend and CLI while keeping the Next.js dashboard. During migration, retain the existing TypeScript API only as a parity oracle, then remove or disable the TypeScript API routes after the Next.js dashboard is wired to the Go backend.

## Architecture Mapping

Recommended Go layout:

```text
go.mod
go.sum
cmd/
  api/
    main.go              # Vercel Go entrypoint, listens on PORT
  agentbox/
    main.go              # CLI entrypoint
internal/
  agentbox/
    auth/                # API key and admin key parsing/auth
    config/              # env parsing, runtime config
    db/                  # PostgreSQL repository and migrations
    assets/              # R2/S3 upload, download URL, MIME helpers
    service/             # thread/message use cases, shared by HTTP and MCP
    httpapi/             # net/http handlers, request/response helpers
    mcpserver/           # MCP server construction and tool handlers
    profiles/            # CLI profile store
    cli/                 # cobra/urfave/flag command tree
    version/             # version string shared by server and CLI
migrations/
  0001_init.sql          # keep existing SQL, later add migration metadata if needed
app/
  ...                    # Next.js dashboard remains the frontend surface
```

Dependency recommendations:

- Router: start with `net/http` plus method checks, or `github.com/go-chi/chi/v5` if route grouping becomes noisy. The Vercel Go docs explicitly support standard `net/http` and frameworks such as `chi`.
- PostgreSQL: use `github.com/jackc/pgx/v5/pgxpool`; it supports context-aware queries and explicit pool settings.
- Migrations: use `github.com/jackc/tern/v2/migrate` or `github.com/golang-migrate/migrate/v4`. For lowest risk, first preserve the current idempotent `ensureSchema` SQL, then introduce versioned migrations in a follow-up.
- R2/S3: use `github.com/aws/aws-sdk-go-v2/service/s3` plus `s3.NewPresignClient`.
- MIME: use `mime.TypeByExtension` with a supplemental extension map if parity gaps appear versus `mime-types`.
- CLI: use `github.com/spf13/cobra` for command parity, global `--profile`, global or per-command `--json`, and testable command execution.
- MCP: use `github.com/modelcontextprotocol/go-sdk/mcp` with `mcp.NewServer`, generic `mcp.AddTool`, `mcp.ToolAnnotations`, and `mcp.NewStreamableHTTPHandler`.

## API Endpoint Parity

Implement these Go routes and keep the response envelopes unchanged:

| Current route | Go handler | Auth | Behavior |
| --- | --- | --- | --- |
| `GET /api/health` | `httpapi.Health` | none | Return `{ "ok": true, "service": "agentbox" }`. |
| `GET /api/threads?limit=N` | `httpapi.ListThreads` | URL `key` actor | Current route uses `Number(limit ?? "50")` without clamping. Preserve for exact parity initially, then optionally clamp in a breaking-change phase. |
| `POST /api/threads` | `httpapi.CreateThread` | URL `key` actor | Validate title length 1..200. Return status 201 and `{ "thread": ... }`. |
| `GET /api/threads/{threadId}` | `httpapi.GetThread` | URL `key` actor | Return `{ "thread": ... }`; 404 only when service returns "Thread not found.". |
| `POST /api/threads/{threadId}/messages` JSON | `httpapi.PostMessageJSON` | URL `key` actor | Accept `{ "body": string?, "file": ChatGPTFileReference? }`; default body to empty string; create optional remote-file asset. |
| `POST /api/threads/{threadId}/messages` multipart | `httpapi.PostMessageMultipart` | URL `key` actor | Accept `body` and `asset`; upload local asset bytes; return status 201. |
| `GET /api/assets/{assetId}/download-url?expires_in=N` | `httpapi.AssetDownloadURL` | URL `key` actor | Return signed R2 URL; clamp expiry to 60..3600, default 300; 404 when asset is missing. |
| `GET /api/viewer/threads?limit=N` | `httpapi.ViewerListThreads` | admin header or bearer | Clamp limit to 1..200, default 100. |
| `GET /api/viewer/threads/{threadId}` | `httpapi.ViewerGetThread` | admin header or bearer | Return full thread with signed asset URLs; image previews use 900 seconds, other files 300 seconds. |
| `GET/POST/DELETE /api/mcp` | `mcpserver.Handler` | URL `key` actor plus optional origin validation | Serve Streamable HTTP MCP with the same tools and stateless request behavior. |

HTTP compatibility details:

- Keep query-key auth for API and MCP because current CLI and ChatGPT setup depend on `?key=...`.
- Keep admin viewer auth in headers because the current browser UI avoids putting admin keys in URLs.
- Keep JSON field names in snake_case because the current TypeScript types and CLI output use those names.
- Keep ISO timestamp strings from PostgreSQL timestamps.
- Keep current error envelopes as `{ "error": "..." }`.
- Keep `content-type: application/json` on JSON responses.
- Add request body limits in Go explicitly. Use a separate limit for JSON and multipart. The plan must resolve Vercel's 4.5 MB limit before claiming 25 MiB uploads work on Vercel.

## MCP Server Behavior

Port `src/core/mcp.ts` to `internal/agentbox/mcpserver`:

- Build `New(actor Actor, svc *service.Service) *mcp.Server`.
- Use implementation name `agentbox` and version `0.1.0` until a shared version package is added.
- Register tools:
  - `list_threads`: input `{ limit?: number }`, max 100, read-only annotation.
  - `get_thread`: input `{ thread_id: string }`, read-only annotation.
  - `create_thread`: input `{ title: string }`, write annotation.
  - `post_message`: input `{ thread_id: string, body?: string, file?: ... }`, write annotation, OpenAI file parameter metadata if the Go SDK supports custom `_meta` fields on tools.
- Return `CallToolResult` with text content matching current messages:
  - "Listed Agentbox threads."
  - "Fetched Agentbox thread."
  - "Created Agentbox thread."
  - "Posted message to Agentbox."
- Return structured content equivalent to current `{ threads: ... }`, `{ thread: ... }`, and `{ message: ... }`.
- Preserve tool annotations: read-only false/true, destructive false, open-world false/true where expressible.
- For `post_message.file`, support an object `{ download_url, file_id, mime_type?, file_name? }`. The current MCP schema description says a string `file_...` can be passed, but `uploadChatGPTFile` only accepts URL strings and otherwise errors. Preserve this behavior or fix it explicitly with a migration note.
- Use `mcp.NewStreamableHTTPHandler` in the HTTP server. If the SDK defaults diverge from current `enableJsonResponse: true` behavior, add compatibility tests and configure options or wrap responses.
- Add MCP tests against the Go SDK client using `mcp.StreamableClientTransport` and against raw JSON-RPC payloads for ChatGPT compatibility.

## CLI Command Parity

Port `src/cli/index.ts` into `cmd/agentbox` plus `internal/agentbox/cli`.

Global behavior:

- Command name: `agentbox`.
- Version: `0.1.0` until release versioning changes.
- Global `-p, --profile <name>`.
- Preserve `--json` on commands that currently support it.
- Parse responses and print server errors using the current `error` field.
- Set non-zero exit code on errors.

Profile commands:

- `agentbox profiles [--json]`
- `agentbox profiles add <name> --base-url <url> --api-key <key> [--activate] [--json]`
- `agentbox profiles remove <name> [--json]`
- `agentbox profiles use <name> [--json]`
- `agentbox profiles show [name] [--json]`

Profile storage parity:

- `AGENTBOX_CONFIG_DIR` overrides config directory.
- Windows: `%APPDATA%/agentbox`.
- macOS: `$HOME/Library/Application Support/agentbox`.
- Linux/Unix: `$XDG_CONFIG_HOME/agentbox`, else `$HOME/.config/agentbox`.
- File name: `profiles.json`.
- File mode: `0600`.
- Payload shape:

```json
{
  "active_profile": "default",
  "profiles": {
    "default": {
      "base_url": "https://example.com",
      "api_key": "secret"
    }
  }
}
```

- Read compatibility: accept `active_profile` or legacy `current_profile`; accept `base_url` or `baseUrl`; accept `api_key` or `apiKey`; accept profile array or object.
- Env profile precedence: `AGENTBOX_PROFILES` JSON wins over stored profiles; `AGENTBOX_PROFILE` selects a profile; legacy `AGENTBOX_BASE_URL` or `AGENTBOX_URL` with `AGENTBOX_API_KEY` is fallback.

Operational commands:

- `agentbox doctor [--json]`: check profile resolution, base URL, masked API key, `/api/health`, authenticated `/api/threads?limit=10`, first signed download URL if recent assets exist, and sanitized MCP URL.
- `agentbox list [-n, --limit <number>] [--json]`: call `/api/threads?limit=N`, default 50, print `id updated_at title`.
- `agentbox create <title> [--json]`: call `POST /api/threads`, print `id title`.
- `agentbox get <thread-id> [--json]`: call `GET /api/threads/{id}`, print markdown-ish thread view exactly enough to preserve user workflows.
- `agentbox download <thread-id> [-o, --output <dir>] [--json]`: fetch thread, request each signed download URL, write files to `agentbox-downloads/{threadId}` by default as `{asset.id}-{asset.file_name}`.
- `agentbox post <thread-id> [message] [-f, --file <path>] [-a, --asset <path>] [--json]`: use message argument, then `--file`, then stdin if non-TTY and body is empty; use JSON when no asset and multipart when asset is present.

## Config, Auth, and Key Handling

Server env parity:

- `DATABASE_URL`: required for DB.
- `AGENTBOX_DB_POOL_SIZE`: default `3`.
- `AGENTBOX_API_KEYS`: JSON array or comma-separated `name:key:author`.
- `AGENTBOX_ALLOWED_ORIGINS`: optional comma-separated exact origins for MCP.
- `AGENTBOX_ADMIN_KEYS`: JSON array or comma-separated `name:key`.
- `AGENTBOX_ADMIN_KEY`: legacy single admin key fallback.
- `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`: required for asset upload/download.
- `R2_PUBLIC_BASE_URL`: optional public URL base.
- `AGENTBOX_MAX_FILE_SIZE_BYTES`: default current value for non-Vercel deployments, but see Vercel payload risk below.
- `NODE_ENV` equivalent: introduce `AGENTBOX_ENV=production` or use `VERCEL_ENV=production` to decide whether local-dev no-key fallback is allowed. Do not depend on Node-specific `NODE_ENV` in Go.

Auth implementation:

- Use `crypto/subtle.ConstantTimeCompare` after checking equal lengths.
- Keep exact local-dev fallback semantics for development, but make production detection explicit and tested.
- Parse JSON config strictly enough to fail loudly on invalid JSON, matching current `JSON.parse` behavior.
- Keep actors as `{ name, keyName }`, where `name` is the configured author and `keyName` is the configured key name.
- Never log raw API keys, admin keys, signed URLs with credentials, or profile secrets.

## Attachment, R2 Download, and Upload Behavior

Current behavior to preserve:

- Filename sanitization regex equivalent to `[^a-zA-Z0-9._-]+` -> `-`, trim leading/trailing `-`, max 150 chars, fallback `file.bin`.
- Storage key shape: `agentbox/{threadId}/{messageHint}/{uuid}-{sanitizedFileName}`.
- Public URL shape: trim trailing slash from `R2_PUBLIC_BASE_URL` and append `/{storageKey}`.
- Upload content type: inferred MIME type or `application/octet-stream`.
- Signed download disposition: attachment with ASCII fallback filename and RFC 5987 `filename*` UTF-8 encoding.
- ChatGPT file upload flow: fetch `download_url`, check `content-length` before reading when present, check actual bytes after reading, use `file_name` or `{file_id}.bin`, use `file_id` as message hint.

Vercel payload problem:

- The current API accepts up to 25 MiB and proxies upload/download bytes through the server, but Vercel Functions cap request and response payloads at 4.5 MB.
- A Go port deployed on Vercel cannot reliably preserve 25 MiB multipart uploads through `/api/threads/{id}/messages`.

Recommended resolution:

1. Preserve the current multipart endpoint for non-Vercel/self-hosted use, but set the default Vercel upload cap to <= 4.5 MB unless a direct-upload flow is implemented.
2. Add a direct upload flow before raising Vercel file limits:
   - `POST /api/assets/upload-url` to create a presigned R2 PUT URL and a pending asset token.
   - CLI uploads directly to R2, then calls `POST /api/threads/{id}/messages` with finalized asset metadata.
   - MCP ChatGPT files are still server-fetched from `download_url`; if files exceed Vercel limits, stream from ChatGPT response directly into R2 without buffering full content into memory or response. Verify the Vercel runtime permits the needed outbound streaming duration.
3. Keep old multipart command behavior in the CLI initially, then add a feature flag or automatic direct-upload path when the server advertises support.

## DB and Migrations Strategy

Initial Go repository should use the existing schema as canonical:

- Keep `threads`, `messages`, and `assets` table names and columns.
- Keep ID prefixes and UUID generation.
- Keep existing indexes.
- Keep `updated_at` update when posting a message.
- Keep asset insert in the same DB transaction as message insert.

Implementation plan:

- Phase 1: Port `ensureSchema` as idempotent SQL executed by `agentbox migrate` and optionally at server startup for compatibility.
- Phase 2: Add a migration metadata table and versioned migration runner. Use the existing `migrations/0001_init.sql` as migration 1 without changing SQL.
- Phase 3: Disable lazy schema creation in production by default after migration tooling is proven, while retaining a dev flag such as `AGENTBOX_AUTO_MIGRATE=true`.

Connection strategy:

- Use `pgxpool` with max conns from `AGENTBOX_DB_POOL_SIZE`.
- Set context timeouts on DB operations.
- On Vercel, keep the pool small because many function/server instances can multiply connections.
- Consider a managed pooler if `DATABASE_URL` points to Neon, Supabase, or another hosted Postgres with connection caps.

## Vercel Deployment and Runtime Constraints

Preferred Go server deployment:

- Add root `go.mod` with explicit `go` directive and optional `toolchain`.
- Use `cmd/api/main.go` because Vercel Go preset detects it.
- Server must read `PORT`, default to `3000` locally, and call `http.ListenAndServe`.
- Set Vercel framework preset to `go`.
- Add `vercel.json` only if needed:

```json
{
  "$schema": "https://openapi.vercel.sh/vercel.json",
  "buildCommand": "go build -o server ./cmd/api",
  "regions": ["iad1"]
}
```

- Choose `regions` near the PostgreSQL database. If the DB is not in `iad1`, set the region accordingly.
- Keep binary and bundled files under 250 MB uncompressed.
- Do not rely on persistent local disk. Use only `/tmp` for scratch work and R2/Postgres for durable state.
- Do not assume 25 MiB request bodies on Vercel; implement direct upload for larger files.

Deployment shape:

### Target: Go Backend Service Plus Next.js Dashboard

- Go backend is a Vercel Go Service.
- Next.js remains the dashboard/frontend service.
- The Next.js dashboard should call the Go backend for:
  - `/api/viewer/threads`
  - `/api/viewer/threads/{threadId}`
  - any future dashboard API calls
- ChatGPT and CLI clients should call the Go backend for:
  - `/api/mcp`
  - `/api/threads`
  - `/api/threads/{threadId}`
  - `/api/threads/{threadId}/messages`
  - `/api/assets/{assetId}/download-url`
- Pros: preserves the current dashboard UX, keeps frontend iteration in Next.js, and ports backend/CLI behavior to Go without a UI rewrite.
- Cons: requires clear Vercel service routing, local dev orchestration for two servers, and CORS/same-origin decisions between the dashboard and Go API.

### Alternative: Separate Public Origins

- Go backend and Next.js dashboard can run on separate URLs if Vercel Services or same-origin rewrites are blocked.
- Pros: simplest deployment fallback.
- Cons: the dashboard must be configured with a backend base URL, CORS must be enabled intentionally, and user-facing setup has more moving parts.

### Avoid: Go Serverless Function Files Under `/api`

- Each API route is a separate Go handler file.
- Pros: close to current route-by-route structure.
- Cons: duplicates routing and shared setup, worse fit for MCP Streamable HTTP session handling, and less ergonomic for a CLI-oriented Go module.

Recommendation: implement the target split-runtime architecture: Go API/MCP/CLI backend plus Next.js dashboard. Avoid a pure Go dashboard rewrite and avoid Go serverless function files unless Vercel's Go server beta blocks the single-server backend.

## Plan Phases

### Phase 1: Freeze Behavior and Prepare Parity Harness

Files to read before starting:

- `src/core/types.ts`
- `src/core/schemas.ts`
- `src/core/auth.ts`
- `src/core/admin.ts`
- `src/core/db.ts`
- `src/core/assets.ts`
- `src/core/handlers.ts`
- `src/core/mcp.ts`
- `app/api/*/route.ts` files
- `src/core/profiles.ts`
- `src/cli/index.ts`
- `migrations/0001_init.sql`

What to do:

- Add black-box parity tests or fixtures against the current TypeScript implementation before replacing API behavior.
- Cover:
  - health
  - thread create/list/get
  - message post without asset
  - message post with multipart asset below Vercel limit
  - signed download URL response shape
  - viewer list/get with admin key
  - MCP list/create/get/post tool calls
  - CLI profile read/write compatibility
- Capture current error behavior for invalid keys, missing thread, invalid title, invalid file reference, and missing R2 config.
- Decide how tests will run locally with both servers during the migration.

Validation strategy:

- Run TypeScript `typecheck` and existing lint.
- Run the parity tests against the current implementation.
- Store expected JSON fixtures with stable fields normalized where needed.

Risks / fallbacks:

- If current code has no test harness, write black-box tests in Go or Node that hit a running local server and can later be pointed at the Go server.
- If R2 is unavailable locally, use an S3-compatible test double such as MinIO or abstract asset storage behind an interface with an in-memory fake.

### Phase 2: Build Go Backend Core

Files to read before starting:

- `src/core/types.ts`
- `src/core/schemas.ts`
- `src/core/auth.ts`
- `src/core/admin.ts`
- `src/core/db.ts`
- `src/core/assets.ts`
- `src/core/handlers.ts`
- `src/scripts/migrate.ts`
- `migrations/0001_init.sql`
- Vercel Go runtime docs at https://vercel.com/docs/functions/runtimes/go
- Vercel function limits docs at https://vercel.com/docs/functions/limitations

What to do:

- Create root `go.mod` with explicit Go version.
- Add `cmd/api/main.go` with a minimal `PORT` listener and `/api/health`.
- Add `internal/agentbox/{config,service,db,assets,auth,httpapi}` packages.
- Define Go structs with JSON tags matching current snake_case output.
- Define input validation functions for create thread, post message, file reference, and limits.
- Add version package with `0.1.0`.
- Port API key parsing and admin key parsing exactly, including JSON and comma-separated formats.
- Implement timing-safe key comparison.
- Implement production/local-dev fallback based on explicit Go env rules.
- Implement `pgxpool` repository methods:
  - `EnsureSchema`
  - `ListThreads(limit int)`
  - `CreateThread(title, author string)`
  - `GetThread(threadID string)`
  - `GetAsset(assetID string)`
  - `PostMessage(threadID, author, body string, asset *NewAsset)`
- Implement service methods that mirror `src/core/handlers.ts`.
- Implement filename sanitizer, MIME inference, storage key builder, public URL builder, R2 client, upload, and presigned download URL creation.
- Use AWS SDK for Go v2 S3 client with endpoint `https://{R2_ACCOUNT_ID}.r2.cloudflarestorage.com` and region `auto`.
- Implement `UploadChatGPTFile` with content-length and actual byte checks.
- Implement streaming-to-R2 for remote ChatGPT downloads if possible, to reduce memory pressure.
- Add an `AssetStore` interface and fake implementation for tests.
- Decide and document Vercel upload behavior:
  - cap multipart to 4.5 MB on Vercel, or
  - implement direct upload before production cutover.
- Add `agentbox migrate` CLI subcommand or separate `cmd/migrate` only if the final CLI command surface is approved. Otherwise keep migration as server-internal until rollout.

Validation strategy:

- `go test ./...`
- `go vet ./...`
- Local `go run ./cmd/api` and `curl /api/health`.
- Unit tests for key parsing, config parsing, validators, sanitizer, MIME fallback, public URL, and signed URL expiry clamp.
- Repository integration tests against test Postgres.
- Transaction test proving message plus asset insert and `threads.updated_at` update are atomic.
- Integration test with MinIO or R2 dev bucket.
- Memory test for remote download path if streaming is implemented.

Risks / fallbacks:

- If Vercel Go server preset behavior changes, switch to root `main.go` or `cmd/server/main.go`, both documented as detected entrypoints.
- Lazy schema creation can hide migration problems. Keep it during early parity, then make production migration explicit later.
- MIME inference may not exactly match `mime-types`; add table-driven tests for common file extensions used by Agentbox.
- Vercel payload cap is the largest functional gap. Do not ship claims of 25 MiB Vercel support until direct upload is implemented.

### Phase 3: Port HTTP API and MCP Server

Files to read before starting:

- `app/api/health/route.ts`
- `app/api/threads/route.ts`
- `app/api/threads/[threadId]/route.ts`
- `app/api/threads/[threadId]/messages/route.ts`
- `app/api/assets/[assetId]/download-url/route.ts`
- `app/api/viewer/threads/route.ts`
- `app/api/viewer/threads/[threadId]/route.ts`
- `app/api/mcp/route.ts`
- `src/core/mcp.ts`
- `src/core/http.ts`
- Official Go MCP SDK docs at https://github.com/modelcontextprotocol/go-sdk and https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp

What to do:

- Implement route registration under `/api/...`.
- Implement JSON helpers and error envelope.
- Implement multipart parsing with explicit size limits.
- Implement viewer handlers with admin auth and image preview URL logic.
- Preserve current status codes:
  - 201 for created thread/message.
  - 401 for API auth failure.
  - 401 for viewer unauthorized.
  - 403 for forbidden origin on MCP.
  - 404 for missing thread/asset where current code does.
  - 400 for validation/post failures.
  - 500 for list/get/signed URL internal failures.
- Add `internal/agentbox/mcpserver`.
- Register four tools with the same names, titles, descriptions, input schemas, output schemas, annotations, and response text.
- Wire tool handlers to the Go service layer.
- Add request-level actor injection so each authenticated request creates or retrieves a server with the correct actor.
- Wrap `mcp.NewStreamableHTTPHandler` in auth/origin middleware for `/api/mcp`.
- Investigate and implement custom tool metadata for `openai/fileParams`, `openai/toolInvocation/invoking`, and `openai/toolInvocation/invoked`. If the SDK does not support arbitrary `_meta`, document a compatibility limitation and consider a small protocol-level patch or raw schema route.

Validation strategy:

- Run black-box parity tests from Phase 1 against the Go server.
- Compare JSON response keys and status codes.
- Use MCP inspector or Go SDK client tests against local `/api/mcp`.
- Verify ChatGPT can list tools and call `post_message` with a file reference.
- Verify GET, POST, and DELETE do not regress current client expectations.

Risks / fallbacks:

- Current `GET /api/threads` does not clamp limit. If Go validates more strictly, clients could observe behavior changes. Preserve first, harden later.
- The Go MCP SDK may handle Streamable HTTP sessions differently from the TypeScript SDK's `sessionIdGenerator: undefined` stateless behavior. If parity fails, either configure stateless options or implement a small adapter around the SDK transport.
- OpenAI-specific MCP metadata may not map cleanly. Treat this as a release blocker for ChatGPT file upload parity.

### Phase 4: Port CLI and Rewire Next.js Dashboard

Files to read before starting:

- `src/cli/index.ts`
- `src/core/profiles.ts`
- `app/page.tsx`
- `app/components/inbox-button.tsx`
- `app/components/viewer-profiles.ts`
- `app/threads/page.tsx`
- `app/threads/inbox-view.tsx`
- `app/threads/[threadId]/page.tsx`
- `app/threads/[threadId]/thread-view.tsx`
- Vercel Go runtime docs section about using Services with Next.js.

What to do:

- Implement `cmd/agentbox`.
- Port profile store, environment precedence, URL sanitization, and secret masking.
- Port all command names, flags, output text, and JSON output.
- Implement HTTP client with query-key injection.
- Implement stdin and file body handling.
- Implement multipart upload and direct upload support if Phase 2 added direct upload.
- Build cross-platform binaries.
- Keep the web dashboard in Next.js.
- Remove backend business logic from the Next.js runtime after the Go API is ready.
- Replace Next.js API route implementations with one of these thin routing strategies:
  - same-origin rewrites to the Go backend service, preferred for browser simplicity;
  - route handlers that proxy to the Go backend only when rewrites are not enough;
  - explicit dashboard backend base URL only as a fallback for separate-origin deployments.
- Keep the landing page, inbox page, thread detail page, and viewer profile UI in Next.js.
- Preserve localStorage keys:
  - `agentbox_admin_key`
  - `agentbox_viewer_profiles_v1`
  - `agentbox_active_viewer_profile_id`
- Preserve viewer API calls and admin header behavior.

Validation strategy:

- Unit tests for profile parser and writer using temp dirs.
- CLI golden-output tests for common text outputs.
- CLI integration tests against the Go server.
- Verify an existing TypeScript-created `profiles.json` can be read by the Go CLI and a Go-written file can be read by the TypeScript CLI during transition.
- Browser smoke test for saving viewer profile, listing threads, opening thread details, image preview, and attachment open link.
- Confirm no admin key appears in URLs.
- Confirm dashboard network calls reach the Go backend and not the old TypeScript handlers.

Risks / fallbacks:

- Cobra may format help text differently from Commander. Preserve command behavior and machine-readable output first; exact help text is lower priority unless users rely on it.
- Same-origin rewrites are preferable. If unavailable, use a thin Next.js proxy so browser code and localStorage behavior remain unchanged.

### Phase 5: Vercel Rollout, Cleanup, and Distribution

Files to read before starting:

- `package.json`
- `next.config.ts`
- `go.mod`
- `cmd/api/main.go`
- `vercel.json` if added
- `scripts/fix-cli-shebang.mjs`
- `src/cli/index.ts`
- `src/core/*`
- `app/api/*`
- `app/threads/*` only to verify dashboard calls are routed to the Go backend; do not remove dashboard pages.
- Vercel docs:
  - https://vercel.com/docs/functions/runtimes/go
  - https://vercel.com/docs/project-configuration/vercel-json
  - https://vercel.com/docs/functions/limitations

What to do:

- Configure the Go backend service with Vercel framework preset `go`.
- Keep the Next.js dashboard as a separate frontend service.
- Ensure `cmd/api/main.go` listens on `PORT`.
- Configure same-origin routing so existing public paths resolve correctly:
  - API/MCP paths route to the Go backend.
  - dashboard/page paths route to Next.js.
- Add `vercel.json` or service routing config with region and optional build command only when necessary.
- Configure backend environment variables in the Go service and dashboard environment variables in the Next.js service.
- Run migrations before production traffic.
- Deploy preview, point CLI profile and dashboard preview at the Go backend preview, run full parity suite.
- Cut over production MCP URL and CLI base URL.
- Keep the old TypeScript API deployment available for rollback until the Go backend proves stable.
- After parity and cutover, remove or archive TypeScript API/CLI/backend code while keeping the Next.js dashboard code.
- Decide distribution path for the Go CLI:
  - GitHub Releases with OS/arch binaries.
  - Homebrew tap.
  - npm wrapper package that downloads the Go binary, if npm distribution must be preserved.
- Update build/release scripts.
- Preserve `migrations/` and license.

Validation strategy:

- `agentbox doctor` against preview and production.
- MCP inspector or ChatGPT custom MCP server validation against preview.
- Thread create/post/get/download from CLI.
- Viewer admin profile smoke test.
- Next.js dashboard smoke test against the Go backend.
- Verify Vercel logs for timeout, payload, connection, and R2 errors.
- Fresh checkout build.
- Fresh install CLI on macOS/Linux, plus Windows if supported.
- Production smoke test.

Risks / fallbacks:

- If Vercel Go beta has a blocker, deploy the Go backend elsewhere temporarily while keeping Vercel for the Next.js dashboard.
- If Postgres connection churn appears, lower pool size and add a pooler.
- Existing npm users expect `npm install -g agentbox`. Keep a wrapper package or publish a clear migration release.

## Cross-Provider Requirements

The port should avoid hard-coding Vercel where possible:

- Keep HTTP server portable by using `net/http` and `PORT`.
- Keep DB as plain PostgreSQL over `DATABASE_URL`.
- Keep object storage behind an S3-compatible interface so Cloudflare R2 remains default but MinIO/AWS S3 can be used in tests or self-hosting.
- Make payload limits configurable by deployment target:
  - Vercel: <= 4.5 MB direct function request/response unless direct upload is enabled.
  - Self-hosted: current 25 MiB default can remain.
- Keep filesystem assumptions limited to temp files and CLI downloads.
- Keep auth based on environment variables and request headers/query parameters, not Vercel-specific identity.
- Keep migrations executable outside Vercel.

## Risks and Decisions Needed

1. Vercel upload limit: current 25 MiB attachment behavior conflicts with Vercel's 4.5 MB function payload cap. Recommended decision: implement direct R2 uploads for CLI/local assets and streaming remote-file uploads before production cutover.
2. UI scope: decided. The web dashboard remains Next.js; only the API/MCP/backend and CLI move to Go.
3. MCP metadata parity: OpenAI-specific `_meta` fields must be verified with the Go MCP SDK. Recommended decision: treat ChatGPT file parameter metadata as a release blocker.
4. Migration policy: lazy `ensureSchema` is convenient but weak for production. Recommended decision: preserve initially, then require explicit migrations for production.
5. CLI distribution: npm is current implied distribution through `package.json` bin. Recommended decision: ship Go binaries through GitHub Releases and optionally maintain a small npm wrapper for continuity.

## Recommended Sequence

1. Freeze current behavior with parity tests and fixtures against the TypeScript implementation.
2. Build the Go backend core: module, config/auth, DB, migrations, service layer, R2 assets, and upload strategy.
3. Port the Go HTTP API and MCP server, then validate API and MCP parity.
4. Port the CLI and rewire the Next.js dashboard to call the Go backend.
5. Roll out on Vercel, run production-readiness checks, cut over traffic, and remove/archive TypeScript API/CLI/backend code after rollback risk is acceptable.
