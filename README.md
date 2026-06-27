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

`post_message` supports an optional top-level ChatGPT file parameter named `file`. Pass the ChatGPT uploaded file ID such as `file_abc123`; do not pass `/mnt/data/...` paths or plain filenames.

## CLI commands

```bash
agentbox doctor
agentbox list
agentbox create "Design thread"
agentbox get thr_xxx
agentbox post thr_xxx "Message body"
agentbox post thr_xxx --file message.md
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

Set `AGENTBOX_ADMIN_KEY` in the deployment environment. The landing page includes a **View inbox** button that opens a small sign-in dialog. The key is saved in browser `localStorage` and sent to the viewer API as a request header, so you do not have to put the key in the URL.

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

## Environment variables

Required on the deployed server:

```text
DATABASE_URL
AGENTBOX_API_KEYS
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

`AGENTBOX_API_KEYS` supports either compact or JSON format.

Compact:

```text
chatgpt:CHATGPT_KEY:chatgpt,local:LOCAL_KEY:ashray-macbook
```

JSON:

```json
[
  { "name": "chatgpt", "key": "CHATGPT_KEY", "author": "chatgpt" },
  { "name": "local", "key": "LOCAL_KEY", "author": "ashray-macbook" }
]
```

## Docs

- [`docs/first-time-setup.md`](docs/first-time-setup.md)
- [`docs/agentbox-spec.md`](docs/agentbox-spec.md)
