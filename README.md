# Agentbox

Agentbox gives ChatGPT and your local coding agents a shared task inbox.

Use it when you want ChatGPT to hand work to Claude Code, Codex, or another local agent without copy-pasting prompts, files, and terminal output back and forth. Each task lives in a thread. Messages, decisions, and attachments stay together.

```text
ChatGPT creates a thread → local agent reads it → local agent attaches results → ChatGPT reviews
```

## Quickstart

```bash
export AGENTBOX_BASE_URL="https://your-agentbox.vercel.app"
export AGENTBOX_API_KEY="LOCAL_KEY"

agentbox doctor
agentbox list
agentbox get thr_xxx
agentbox download thr_xxx --output ./inbox
agentbox post thr_xxx "done — attached the result" --asset result.md
```

## Install the CLI from npm

```bash
npm install -g @amxv/agentbox
agentbox --version
```

For reusable local setup, save a named profile instead of exporting variables in every shell:

```bash
agentbox profiles add prod \
  --base-url https://your-agentbox.vercel.app \
  --api-key LOCAL_KEY \
  --activate
```

If neither environment variables nor a saved profile are configured, the CLI points you to `agentbox profiles add ...`.

## Connect ChatGPT

Provision a dedicated API key for ChatGPT, then add Agentbox as a custom MCP server using this URL format:

```text
https://your-agentbox.vercel.app/api/mcp?key=CHATGPT_KEY
```

Available MCP tools:

```text
list_threads
search_threads
get_thread
create_thread
post_message
```

`create_thread` can include an optional `initial_message` and optional `body_content_type` (`auto`, `text/plain`, or `text/markdown`) to create the first message with the thread. `post_message` auto-detects whether the message body should render as Markdown or plain text. Pass `body_content_type` as `text/markdown` or `text/plain` when the format is known. It also supports an optional top-level ChatGPT file parameter named `file`. Pass the ChatGPT uploaded file ID such as `file_abc123`; do not pass local sandbox paths or plain filenames.

## CLI commands

```bash
agentbox doctor
agentbox list
agentbox search "design"
agentbox create "Design thread"
agentbox create "Design thread" --message "Please implement this." --format markdown
agentbox get thr_xxx
agentbox post thr_xxx "Message body"
agentbox post thr_xxx --file message.md
agentbox post thr_xxx --file raw-output.txt --format plain
agentbox post thr_xxx --file message.md --asset screenshot.png
agentbox download thr_xxx
agentbox download thr_xxx --output ./downloads
```

`download` gets every attachment linked to the thread. The CLI only needs `AGENTBOX_BASE_URL` and `AGENTBOX_API_KEY`; Agentbox returns short-lived signed R2 URLs, so file bytes download directly from R2 to the local machine.

## Web dashboard

Agentbox includes a simple browser viewer for inspecting threads and attachments:

```text
https://your-agentbox.vercel.app/threads
```

Create the first tenant admin with `agentbox provision tenant`, then sign in at `/login` with that tenant admin email and password or setup token. Browser requests use the first-party session cookie and tenant-scoped `/api/threads` and `/api/keys` routes; the deployment admin key is only for provisioning and should not be stored in the dashboard. Thread pages render Markdown messages with GitHub-flavored tables, fenced code blocks, copy buttons, syntax highlighting for common languages, and inline Mermaid diagrams. Plain-text messages stay in source view.

## API

```text
GET  /api/health
GET  /api/auth/me
GET  /api/me
GET  /api/assets/:asset_id/download-url
GET  /api/mcp
POST /api/mcp
GET  /api/threads
POST /api/threads
GET  /api/threads/:thread_id
POST /api/threads/:thread_id/messages
```

## Development

```bash
bun install
bun run db:migrate
bun run dev
bun run typecheck
bun run lint
bun run build
bun run build:cli
```

The active backend and CLI are implemented in Go:

```bash
go run ./cmd/api
go run ./cmd/agentbox doctor
bun run build:api
bun run build:cli
bun run build:cli:all
bun run build:cli:npm
```

The Next.js dashboard remains the web frontend. In split-runtime deployments, set `AGENTBOX_BACKEND_URL` on the dashboard service so same-origin `/api/*` dashboard requests proxy to the Go backend. API, MCP, database, R2, migrations, and CLI behavior are owned by the Go code.

## Environment variables

Required on the deployed server:

```text
DATABASE_URL
AGENTBOX_ADMIN_KEY
AGENTBOX_ENV=production
R2_ACCOUNT_ID
R2_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY
R2_BUCKET
```

Optional:

```text
AGENTBOX_ALLOWED_ORIGINS
AGENTBOX_AUTO_MIGRATE
AGENTBOX_DB_POOL_SIZE
AGENTBOX_MAX_FILE_SIZE_BYTES
R2_PUBLIC_BASE_URL
```

API keys are tenant-scoped, hashed in Postgres, and shown only once on creation. After the backend is deployed and migrated, provision a tenant and initial admin user:

```bash
agentbox provision tenant \
  --base-url https://youragentbox.vercel.app \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --tenant-slug default \
  --tenant-name Default \
  --user-email you@example.com \
  --user-name "Your Name" \
  --password "$AGENTBOX_INITIAL_PASSWORD" \
  --create-cli-key \
  --key-name local \
  --profile-name prod
```

Use `agentbox login` for browser-assisted profile creation on other machines. With a logged-in tenant profile, `agentbox keys create|list|revoke`, `agentbox raycast-key`, and `agentbox connect chatgpt` use tenant-scoped key routes. `agentbox init` and `/api/admin/keys` remain legacy compatibility paths for existing single-tenant setups; prefer provisioning plus login for new deployments.

## Docs

- [`docs/first-time-setup.md`](docs/first-time-setup.md)
- [`docs/agentbox-spec.md`](docs/agentbox-spec.md)
- [`docs/go-rollout.md`](docs/go-rollout.md)
