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
get_thread
create_thread
post_message
```

`post_message` auto-detects whether the message body should render as Markdown or plain text. Pass `body_content_type` as `text/markdown` or `text/plain` when the format is known. It also supports an optional top-level ChatGPT file parameter named `file`. Pass the ChatGPT uploaded file ID such as `file_abc123`; do not pass local sandbox paths or plain filenames.

## CLI commands

```bash
agentbox doctor
agentbox list
agentbox create "Design thread"
agentbox get thr_xxx
agentbox post thr_xxx "Message body"
agentbox post thr_xxx --file message.md
agentbox post thr_xxx --file raw-output.txt --format plain
agentbox post thr_xxx --file message.md --asset screenshot.png
agentbox download thr_xxx
agentbox download thr_xxx --output ./downloads
```

`download` gets every attachment linked to the thread. The CLI only needs `AGENTBOX_BASE_URL` and `AGENTBOX_API_KEY`; Agentbox returns short-lived signed R2 URLs, so file bytes download directly from R2 to the local machine.

## Read-only web viewer

Agentbox includes a simple browser viewer for inspecting threads and attachments:

```text
https://your-agentbox.vercel.app/threads
```

Set `AGENTBOX_ADMIN_KEY` in the deployment environment. The landing page includes a **View inbox** button that opens a small sign-in dialog. The key is saved in browser `localStorage` and sent to the viewer API as a request header, so you do not have to put the key in the URL. Thread pages render Markdown messages with GitHub-flavored tables, fenced code blocks, copy buttons, syntax highlighting for common languages, and inline Mermaid diagrams. Plain-text messages stay in source view.

## API

```text
GET  /api/health
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
R2_ACCOUNT_ID
R2_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY
R2_BUCKET
```

Optional:

```text
AGENTBOX_ALLOWED_ORIGINS
AGENTBOX_MAX_FILE_SIZE_BYTES
R2_PUBLIC_BASE_URL
```

API keys are stored in Postgres and managed through the Go backend admin API. After the backend is deployed and migrated, run:

```bash
agentbox init \
  --profile-name prod \
  --base-url https://youragentbox.vercel.app \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --local-key-name local \
  --chatgpt-key-name chatgpt
```

The init flow creates a local CLI key and a ChatGPT key in Postgres, saves the local profile, and prints the ChatGPT MCP setup URL. Later key management uses `agentbox keys create|list|revoke` with `--admin-key` or `AGENTBOX_ADMIN_KEY`.

## Docs

- [`docs/first-time-setup.md`](docs/first-time-setup.md)
- [`docs/agentbox-spec.md`](docs/agentbox-spec.md)
- [`docs/go-rollout.md`](docs/go-rollout.md)
