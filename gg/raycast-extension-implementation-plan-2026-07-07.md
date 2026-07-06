# Raycast Extension Implementation Plan

Date: 2026-07-07

## State of Current System

Agentbox currently exposes a compact, HTTP-first product surface backed by a Go service and mirrored by both the Go CLI and the Next.js dashboard.

The canonical backend is `internal/agentbox/httpapi/server.go`. It registers:

- `GET /api/health`
- `GET /api/threads?key=...&limit=...`
- `GET /api/threads?key=...&query=...&limit=...&created_by=...&updated_after=...`
- `POST /api/threads?key=...` with `title`, optional `initial_message`, and optional `body_content_type`
- `GET /api/threads/{threadId}?key=...`
- `POST /api/threads/{threadId}/messages?key=...` as JSON or multipart form data
- `POST /api/threads/{threadId}/uploads?key=...` for presigned direct file uploads
- `GET /api/assets/{assetId}/download-url?key=...&expires_in=...`
- admin-only key and viewer endpoints
- `GET/POST/DELETE /api/mcp?key=...` for the MCP server

Authentication for normal API usage is query-string based: `?key=<api key>`. `requireActor` resolves the API key through `service.AuthenticateAPIKey`, and the actor name becomes `created_by` or message `author`. Admin routes use `x-agentbox-admin-key` or bearer auth, but daily Raycast use should avoid requiring an admin key after initial setup.

The service layer in `internal/agentbox/service/service.go` owns validation and behavior:

- thread titles are validated before creation
- messages auto-resolve `body_content_type` through `messageformat.Resolve`
- search supports query, limit, created-by, and RFC3339 `updated_after`
- attachments can be posted as multipart files or through the presigned upload flow
- signed download URLs are clamped by `validate.ClampSignedURLExpiry`

The data types in `internal/agentbox/types/types.go` are simple JSON structs. A Raycast extension can model them directly without code generation:

- `Thread`: `id`, `title`, `created_at`, `updated_at`, `created_by`
- `Message`: `id`, `thread_id`, `author`, `body`, `body_content_type`, `created_at`, `assets`
- `SearchThreadResult`: `id`, `title`, `updated_at`, `created_by`, `message_count`, `last_message_preview`, `matched_snippets`
- `Asset`: `id`, `file_name`, `mime_type`, `size_bytes`, optional `download_url`

The CLI in `internal/agentbox/cli/cli.go` is the strongest behavior reference for daily workflows. It implements `doctor`, `mcp-url`, `list`, `search`, `create`, `get`, `download`, and `post` over the same HTTP API. It stores profiles through `internal/agentbox/profiles/profiles.go`, with macOS default path `~/Library/Application Support/agentbox/profiles.json` unless `AGENTBOX_CONFIG_DIR` is set. Existing profile resolution also supports `AGENTBOX_PROFILES`, `AGENTBOX_PROFILE`, `AGENTBOX_BASE_URL`, `AGENTBOX_URL`, and `AGENTBOX_API_KEY`.

The dashboard is not the canonical API, but it confirms product behavior:

- `app/api/_proxy/proxy.ts` forwards Next API routes to the Go backend through `AGENTBOX_BACKEND_URL`.
- `app/components/agentbox-write.ts` creates a dashboard actor key named `user`, creates threads, obtains presigned upload URLs, uploads files with `PUT`, and posts messages referencing `uploaded_assets`.
- `app/threads/inbox-view.tsx` exposes create/list/detail workflows around an admin-key-authenticated viewer surface.

Existing tests cover the integration surface the Raycast extension should rely on:

- `internal/agentbox/httpapi/server_test.go` covers health, thread creation, initial messages, JSON posting, multipart attachment posting, signed download URLs, search, admin key behavior, and viewer routes.
- `internal/agentbox/cli/cli_test.go` covers profile setup, MCP URL generation, list/search/create/get/post/download behavior, JSON outputs, and multipart asset posting.
- `internal/agentbox/service/service_test.go` covers service-level thread, message, search, and missing-thread behavior.

Current root package/build shape:

- `package.json` is a private Next.js plus Go CLI package using Bun for repo scripts.
- The repo has no Raycast extension package yet.
- Root TypeScript config is Next-oriented and includes all `**/*.ts` and `**/*.tsx`, so a Raycast extension placed inside the repo can accidentally be typechecked by the root unless isolated or excluded.
- Store-ready Raycast extensions should use npm and `package-lock.json`, which is different from the root `bun.lock` workflow.

## Raycast API/Docs Findings

Official Raycast documentation sources checked with `webctx`:

- Getting Started: https://developers.raycast.com/basics/getting-started
- Manifest: https://developers.raycast.com/information/manifest
- File Structure: https://developers.raycast.com/information/file-structure
- Developer CLI: https://developers.raycast.com/information/developer-tools/cli
- Prepare for Store: https://developers.raycast.com/basics/prepare-an-extension-for-store
- Preferences API: https://developers.raycast.com/api-reference/preferences
- List UI: https://developers.raycast.com/api-reference/user-interface/list
- Form UI: https://developers.raycast.com/api-reference/user-interface/form
- Toast API: https://developers.raycast.com/api-reference/feedback/toast
- Clipboard API: https://developers.raycast.com/api-reference/clipboard

Relevant findings:

- Raycast extensions are normally Node/TypeScript packages with React UI and a `package.json` manifest. They are not arbitrary native AppKit apps.
- Current development prerequisites are Raycast 1.26.0+, Node.js 22.14+, npm 7+, React, and TypeScript.
- Extension commands are declared in `package.json`. A command name maps directly to `src/<command>.ts` or `src/<command>.tsx`.
- Command modes are `view`, `no-view`, and `menu-bar`. `view` is appropriate for thread lists, search, details, and forms. `no-view` is appropriate for one-shot commands such as copying an MCP URL or opening the dashboard.
- Raycast preferences are the correct place for configuration such as Agentbox base URL and API key. Required preferences block command use until filled. Password preferences are supported.
- Store review expects `author` to be a Raycast username, `license` to be MIT, latest Raycast API, npm dependencies with `package-lock.json`, local `npm run build`, and `npm run lint`.
- User preference is to use Apache-2.0 licensing where possible. Current Raycast CLI manifest validation requires the extension manifest `license` field to be `MIT`, so keep the Raycast package manifest MIT for build/lint/store compatibility and track Apache-2.0 as a release/legal follow-up rather than bypassing Raycast validation.
- If platform-specific APIs are used, the manifest should restrict `platforms`. This feature is intentionally macOS-native in experience and can set `platforms: ["macOS"]`.
- Raycast extensions can use built-in native-feeling UI components: `List`, `List.Item.Detail`, `Form`, `ActionPanel`, `Action`, `Toast`, `Clipboard`, and file pickers. These are realistic native macOS affordances inside Raycast.
- Useful affordances for Agentbox:
  - `List` with loading state, custom async search, detail pane, sections, accessories, and actions.
  - `Form` with drafts for create/post workflows, `TextArea` with Markdown support, and `FilePicker` for attachments.
  - `showToast` for long-running post/upload operations and failures.
  - `Clipboard.copy`, `Clipboard.paste`, and `Action.CopyToClipboard` for IDs, thread URLs, and MCP URLs.
  - `Action.OpenInBrowser` for dashboard links and signed attachment URLs.

## State of Ideal System

The repo contains a macOS-targeted Raycast extension package that Agentbox users can install locally during development and eventually publish to the Raycast Store or a private Raycast team store.

The extension should feel like a daily Agentbox command center:

- It uses Raycast extension preferences for:
  - `baseUrl`, defaulting to the production dashboard URL or left blank if maintainers prefer explicit setup
  - `apiKey` as a required password preference
  - optional dashboard path preference only if needed later
- It does not require users to install or call the Go CLI for normal operation.
- It calls the existing Agentbox HTTP API directly and keeps the CLI as a behavior reference, not a runtime dependency.
- It offers a first-class `Search Threads` command that can also show recent threads when the search box is empty.
- It offers `Create Thread` and `Post Message` commands that support Markdown/plain auto behavior through the backend, drafts, and optional local file attachment upload.
- It offers fast actions from any thread result:
  - open thread in dashboard
  - copy thread ID
  - copy thread URL
  - copy MCP URL
  - post a reply
  - download/open attachment through a signed URL
- It handles unauthorized, missing configuration, network failures, and missing threads with clear Raycast toasts or empty states and an action to open extension preferences.
- It is packaged and validated independently enough that root Next.js typechecking does not fight Raycast-generated types.

Recommended first daily-use scope:

- Build a single extension package under `raycast/agentbox` or `extensions/raycast-agentbox`.
- Commands:
  - `search-threads`: primary daily command; recent threads by default, server-side search when text is entered, detail pane for last preview/snippets, actions for open/copy/post/get.
  - `create-thread`: form with title, optional initial message, Markdown-enabled textarea, and optional file picker only if implemented through create-then-upload-then-post fallback.
  - `post-message`: form with thread ID argument or picker-backed thread selection, body textarea, optional file picker, and success toast.
  - `copy-mcp-url`: no-view command that copies a concealed-sensitive MCP URL or visible URL depending on chosen UX; prefer not to put the API key into normal clipboard history unless the user explicitly invokes it.
  - `doctor`: view or no-view command that checks preferences, `/api/health`, authenticated `/api/threads?limit=1`, and MCP URL construction.

Defer from first scope:

- Raycast AI tools. They are promising because Agentbox already has MCP-like tool definitions, but they add Pro/user expectations and separate manifest/tool review concerns.
- Menu bar polling. It is useful for unread/recent activity later, but Agentbox currently has no unread state, assignment state, or notification model.
- Admin key management. Admin APIs exist, but storing/administering root keys in Raycast broadens risk. Keep daily usage actor-key based.
- Full offline profile import/write integration with the Go CLI profile store. Reading `~/Library/Application Support/agentbox/profiles.json` can be a nice fallback, but Raycast preferences should be the primary supported setup path.
- Backend API changes. The current endpoints are enough for useful daily workflows.
- Local binary bundling. Store docs caution against opaque/heavy binaries, and direct HTTP avoids that risk.

## Plan Phases

### Phase 1: Extension Package Scaffold and Isolation

Files to read before starting:

- `package.json`
- `tsconfig.json`
- `eslint.config.mjs`
- `npm/agentbox/package.json`
- `deploy/vercel/dashboard/vercel.json`
- `deploy/vercel/backend/vercel.json`
- Raycast docs: Manifest, File Structure, Developer CLI, Prepare for Store

What to do:

- Create a dedicated Raycast package directory, recommended `raycast/agentbox`.
- Add a Raycast-specific `package.json` manifest with:
  - `name`: `agentbox`
  - `title`: `Agentbox`
  - `description`: short daily workflow description
  - `icon`: `assets/icon.png`
  - `author`: placeholder that must be replaced with the maintainer Raycast username before public store submission
  - `license`: `MIT` in the Raycast manifest because Raycast CLI validation currently requires it; keep the Apache-2.0 preference documented as a release/legal follow-up
  - `platforms`: `["macOS"]`
  - categories such as `Productivity` and/or `Developer Tools`
  - extension preferences for `baseUrl` and `apiKey`
  - scripts: `dev`, `build`, `lint`, and optional `publish` using Raycast CLI conventions
- Use npm in the Raycast package and commit `package-lock.json`.
- Add local Raycast `tsconfig.json`, `eslint.config.js`, `.prettierrc`, `assets/icon.png`, and `src/`.
- Keep the package isolated from root Next typechecking. Preferred options:
  - Put package under a path excluded by root `tsconfig.json` and root ESLint if root checks recurse into it.
  - Or update root config intentionally so root checks do not include generated `raycast-env.d.ts` or Raycast-only source files.
- Do not add a runtime dependency on the Go CLI or compiled binaries.

Validation strategy:

- From the Raycast package directory, run `npm install`.
- Run `npm run build` and `npm run lint`.
- From repo root, run `bun run typecheck` and `bun run lint` to catch accidental cross-package config conflicts.
- Confirm `git status --short` includes only expected Raycast package/config files.

Risks/fallbacks:

- Risk: root `tsconfig.json` includes every `**/*.ts` and `**/*.tsx`, causing Raycast generated globals or React version assumptions to conflict with Next.
  - Fallback: add a targeted root exclude for the Raycast package or place the Raycast extension outside root TS inclusion if maintainers prefer.
- Risk: public Raycast Store metadata requires a real Raycast username and production icon.
  - Fallback: ship local/private extension first with a clearly marked metadata TODO, then update before store submission.
- Risk: maintainers prefer Apache-2.0 but Raycast CLI/store validation requires MIT in the extension manifest.
  - Fallback: keep the Raycast manifest on MIT so local and store checks pass, and resolve any broader Apache-2.0 licensing requirement before public release without bypassing Raycast validation.
- Risk: npm lockfile differs from repo's Bun preference.
  - Fallback: keep npm strictly scoped to `raycast/agentbox` because Raycast Store CI expects npm.

### Phase 2: Shared Agentbox API Client for Raycast

Files to read before starting:

- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/service/service.go`
- `internal/agentbox/types/types.go`
- `internal/agentbox/cli/cli.go`
- `internal/agentbox/profiles/profiles.go`
- `app/components/agentbox-write.ts`
- `internal/agentbox/httpapi/server_test.go`
- `internal/agentbox/cli/cli_test.go`
- Raycast docs: Preferences API, Toast API

What to do:

- Implement `src/api.ts` inside the Raycast package with typed functions:
  - `getPreferences()`
  - `agentboxFetch(path, init)`
  - `health()`
  - `listThreads(limit)`
  - `searchThreads(params)`
  - `getThread(threadId)`
  - `createThread({ title, initialMessage, bodyContentType })`
  - `postMessage({ threadId, body, bodyContentType, uploadedAssets })`
  - `createUploadIntents(threadId, files)`
  - `uploadFileToPresignedUrl(upload, filePath)`
  - `getAssetDownloadUrl(assetId, expiresIn)`
  - `mcpUrl()`
  - `dashboardThreadUrl(threadId)`
- Model response and error payloads from `types.go` and `server.go`.
- Add URL construction that trims trailing slashes and appends `key` safely.
- Prefer direct HTTP behavior matching the CLI.
- Implement user-facing errors that preserve backend `code` and `error` when present.
- Add optional helper to read existing CLI profiles only as a convenience if preferences are missing. Keep this behind a later action/fallback, not the first-run default.

Validation strategy:

- Unit-test URL construction and JSON error parsing if the Raycast package test setup is lightweight.
- Build the package with `npm run build`.
- Run Go tests that guarantee API behavior remains intact: `go test ./internal/agentbox/httpapi ./internal/agentbox/service ./internal/agentbox/cli`.
- Manually point preferences at production or a local test server and run `doctor`.

Risks/fallbacks:

- Risk: Raycast runtime file APIs around file upload need careful use of Node `fs`, `Blob`, or `FormData` depending on available runtime.
  - Fallback: first implement posting text-only messages; add attachments once file reading and presigned `PUT` are verified in Raycast.
- Risk: copying MCP URLs includes secrets.
  - Fallback: make the command explicit, use `Clipboard.copy(..., { concealed: true })` where appropriate, and clearly title the action.
- Risk: the backend accepts API keys only as query params.
  - Fallback: keep using query params for parity; do not invent bearer auth unless the backend changes.

### Phase 3: Search and Inspect Threads Command

Files to read before starting:

- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/db/repository.go`
- `internal/agentbox/cli/cli.go`
- `app/threads/inbox-view.tsx`
- `app/threads/[threadId]/thread-view.tsx`
- `app/threads/[threadId]/markdown-message.tsx`
- Raycast docs: List UI, Clipboard API, Toast API, Preferences API

What to do:

- Implement `src/search-threads.tsx` as the primary command.
- Use `List` with:
  - loading state
  - `onSearchTextChange` with throttled server-side search
  - recent threads when search text is empty
  - `filtering={false}` for server results
  - result accessories for updated date, creator, and message count when available
  - `isShowingDetail` with preview, snippets, metadata, and selected thread details
- On selecting a thread, fetch full thread details or push a detail view.
- Render messages as markdown in the detail pane with author/time/id metadata.
- Include actions:
  - Open in Dashboard
  - Copy Thread ID
  - Copy Thread URL
  - Copy MCP URL
  - Post Message
  - Refresh
  - Open Extension Preferences on auth/config errors
- Include attachment actions:
  - Get signed download URL
  - Open in Browser
  - Copy Download URL
- Keep dashboard URLs based on `baseUrl` and `/threads/{threadId}`.

Validation strategy:

- `npm run build` and `npm run lint` in the Raycast package.
- Manual Raycast development run with `npm run dev`.
- Validate states:
  - no preferences set
  - bad API key
  - empty inbox
  - recent threads
  - search with no results
  - search with snippets
  - thread with Markdown body
  - thread with attachments
- Compare visible data against `agentbox list --json`, `agentbox search --json`, and `agentbox get --json` for the same profile.

Risks/fallbacks:

- Risk: large thread bodies make the detail pane noisy.
  - Fallback: show last N messages in list detail and provide a nested full-detail screen.
- Risk: search endpoint requires a non-empty query.
  - Fallback: use `listThreads` while search text is blank and only call `searchThreads` after trimming non-empty input.
- Risk: dashboard URL may be different from API backend URL in self-hosted deployments.
  - Fallback: add optional `dashboardBaseUrl` preference later; initially assume `baseUrl` is the dashboard/proxy URL because production dashboard proxies `/api/*`.

### Phase 4: Create Thread and Post Message Commands

Files to read before starting:

- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/service/service.go`
- `internal/agentbox/messageformat/messageformat.go`
- `internal/agentbox/validate/validate.go`
- `internal/agentbox/cli/cli.go`
- `app/components/message-composer.tsx`
- `app/components/agentbox-write.ts`
- Raycast docs: Form UI, Toast API, Clipboard API

What to do:

- Implement `src/create-thread.tsx`:
  - `Form.TextField` for title
  - Markdown-enabled `Form.TextArea` for optional initial message
  - body format dropdown: `auto`, `text/markdown`, `text/plain`
  - drafts enabled
  - success toast with actions to open thread, copy thread ID, or post follow-up
- Implement `src/post-message.tsx`:
  - accept an optional `threadId` command argument if useful
  - include a thread ID field or prefilled thread ID from action context
  - Markdown-enabled body textarea
  - format dropdown
  - optional `Form.FilePicker` for local attachments
  - drafts enabled
- For attachments, prefer the dashboard direct upload pattern:
  - create upload intents with file name, MIME type, size
  - `PUT` local file bytes to each presigned URL with required headers
  - post JSON message with `uploaded_assets`
- If direct upload proves awkward in Raycast runtime, use backend multipart posting for one attachment first because the CLI and HTTP API already support it.
- Expose `Post Message` action from thread search/detail so daily replies are one keystroke away.

Validation strategy:

- `npm run build` and `npm run lint`.
- Manual Raycast validation:
  - create empty thread
  - create thread with initial plain text
  - create thread with Markdown
  - post reply to existing thread
  - post with empty body only if attachment exists, if product wants that behavior
  - post with one attachment
  - post with multiple attachments if presigned flow is implemented
- Cross-check created/post content through dashboard and `agentbox get --json`.
- Run `go test ./internal/agentbox/httpapi ./internal/agentbox/cli` to keep existing parity behavior intact.

Risks/fallbacks:

- Risk: first-message creation does not support local attachments in one call.
  - Fallback: create the thread first, then post the initial message with attachment through the message endpoint.
- Risk: file size limits differ on Vercel because `MultipartLimitBytes` clamps to roughly 4.5 MB while presigned upload supports larger configured limits.
  - Fallback: use presigned upload for Raycast attachments, and surface backend errors clearly.
- Risk: Raycast drafts do not preserve password fields and may not preserve nested forms.
  - Fallback: keep create/post forms as root commands and store no secrets in drafts.

### Phase 5: Doctor, MCP URL, and Daily Utility Commands

Files to read before starting:

- `internal/agentbox/cli/cli.go`
- `internal/agentbox/cli/profiles.go`
- `internal/agentbox/profiles/profiles.go`
- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/mcpserver/mcpserver.go`
- Raycast docs: Manifest, Preferences API, Clipboard API, Toast API

What to do:

- Implement `src/doctor.tsx` as a quick diagnostic command:
  - profile/preferences present
  - health endpoint status
  - authenticated API status through `/api/threads?limit=1`
  - MCP URL construction
  - optional signed URL check only if recent assets exist
- Implement `src/copy-mcp-url.ts` as `no-view`:
  - build `/api/mcp?key=...`
  - copy to clipboard as concealed content when possible
  - show HUD/toast confirmation
- Add utility actions in existing commands:
  - Copy API Base URL
  - Copy Thread URL
  - Open Dashboard
  - Open Extension Preferences
- Decide whether to add a `open-inbox` no-view command that opens `/threads`.

Validation strategy:

- Compare `doctor` results against `agentbox doctor --json` for the same base URL/API key.
- Compare copied MCP URL against `agentbox mcp-url`.
- Verify `copy-mcp-url` does not open an unnecessary Raycast view.
- Verify failure cases with wrong API key and wrong base URL.

Risks/fallbacks:

- Risk: Clipboard history containing MCP URLs leaks API keys.
  - Fallback: use concealed clipboard copy and consider requiring an explicit confirmation action rather than copying from incidental list actions.
- Risk: `doctor` duplicates CLI behavior and can drift.
  - Fallback: keep checks minimal and endpoint-based; do not reimplement deep CLI diagnostics unless needed.

### Phase 6: Store Readiness, Documentation, and Release Path

Files to read before starting:

- `raycast/agentbox/package.json`
- `raycast/agentbox/package-lock.json`
- `raycast/agentbox/src/*`
- `raycast/agentbox/assets/*`
- `LICENSE`
- `package.json`
- Raycast docs: Prepare for Store, Publish an Extension, Install an Extension

What to do:

- Replace placeholder Raycast metadata:
  - real `author`
  - final icon
  - final categories
  - polished command titles/descriptions in Apple/Raycast title style
- Add a root README inside the Raycast package only if additional setup instructions are needed for Raycast preferences and API key generation.
- Add screenshots metadata if preparing for public store.
- Add a small implementation note in repo scripts only if maintainers want root shortcuts such as `bun run raycast:build`; otherwise keep commands local to the Raycast package.
- Ensure public store path uses npm and includes `package-lock.json`.
- If this is intended for a private team store first, set `owner` appropriately and publish privately.

Validation strategy:

- In `raycast/agentbox`: `npm run lint`, `npm run build`.
- Open in Raycast with `npm run dev` and test all commands.
- Test production build in Raycast, not only development mode.
- From root: `bun run typecheck`, `bun run lint`, targeted Go tests, and optionally full `go test ./...`.
- Store checklist:
  - MIT license in the Raycast manifest unless Raycast validation changes; Apache-2.0 remains a tracked release/legal follow-up
  - no default Raycast icon
  - no external analytics
  - no admin key required for daily use
  - no bundled opaque binaries
  - preferences are used for credentials
  - screenshots do not expose secrets

Risks/fallbacks:

- Risk: public store review dislikes requiring users to manually obtain an Agentbox API key.
  - Fallback: include a clear README and preference onboarding; later add an admin-key-assisted setup command only if necessary, but keep it out of the first daily workflow.
- Risk: extension package inside the app repo complicates submission to `raycast/extensions`.
  - Fallback: develop in-repo for product velocity, then copy or subtree-split the Raycast package for store submission.
- Risk: production Agentbox URL/backend split confuses users.
  - Fallback: prefer the dashboard/proxy URL as the extension base URL by default because it serves both `/threads` and `/api/*`.

## Cross-Runtime, Build, and Package Requirements

- Raycast package runtime: Node.js 22.14+ and npm 7+, per current Raycast docs.
- Repo runtime remains Bun/Next/Go; do not replace root package manager behavior.
- Keep Raycast package dependency management local and npm-based.
- Do not depend on the compiled Go CLI at runtime. Direct HTTP is simpler, store-safe, and avoids binary distribution issues.
- Do not require backend changes for the first version.
- If root checks include the Raycast package, isolate TypeScript and ESLint explicitly.
- The Raycast extension should use `fetch`, Raycast preferences, and typed local models of Agentbox JSON responses.
- File uploads need Node/Raycast filesystem access plus either presigned `PUT` flow or multipart form data. Presigned upload is more scalable and already used by the dashboard.

## Recommendation

Build the first version as a dedicated npm-based Raycast extension package that talks directly to the existing Agentbox HTTP API, with `Search Threads` as the primary command and `Create Thread`, `Post Message`, `Doctor`, and `Copy MCP URL` as supporting commands.

Do not start with Raycast AI tools, menu bar polling, admin key management, backend API changes, or CLI binary integration. Those are all plausible later additions, but the existing API already supports a strong daily macOS Raycast workflow with lower implementation and review risk.

No truly blocking technical decision prevents implementation. The only product/release decision needed before public Store submission is the Raycast author/owner identity and final icon/metadata.
