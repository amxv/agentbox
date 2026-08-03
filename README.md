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
manage_thread_visibility
```

`create_thread` can include an optional `initial_message` and optional `body_content_type` (`auto`, `text/plain`, or `text/markdown`) to create the first message with the thread. `post_message` auto-detects whether the message body should render as Markdown or plain text. Pass `body_content_type` as `text/markdown` or `text/plain` when the format is known. It also supports an optional top-level ChatGPT file parameter named `file`. Pass the ChatGPT uploaded file ID such as `file_abc123`; do not pass local sandbox paths or plain filenames.

## Connect Raycast

Open **Onboarding** or **Credentials** in the signed-in dashboard and create a dedicated Raycast connection for each local installation. Copy the one-time `baseUrl` and `apiKey`, then load the checked-in extension in Raycast developer mode:

```bash
git clone https://github.com/amxv/agentbox.git
cd agentbox/raycast/agentbox
npm ci
npm run verify
npm run dev
```

Configure the required `baseUrl` and password `apiKey` preferences from the setup bundle. The extension browses/searches the complete accessible inbox, creates private threads, posts ordered attachments, downloads authorized files, and manages team/public visibility through ordinary user APIs. It cannot use the owner-browser-only content viewer. Store publication remains deferred.

## CLI commands

```bash
agentbox doctor
agentbox list
agentbox search "design"
agentbox create "Design thread"
agentbox create "Design thread" --message "Please implement this." --format markdown
agentbox get thr_xxx
agentbox visibility thr_xxx
agentbox visibility thr_xxx --share-team engineering --publish
agentbox visibility thr_xxx --unshare-team engineering --unpublish
agentbox post thr_xxx "Message body"
agentbox post thr_xxx --file message.md
agentbox post thr_xxx --file raw-output.txt --format plain
agentbox post thr_xxx --file message.md --asset screenshot.png
agentbox download thr_xxx
agentbox download thr_xxx --output ./downloads
```

`visibility` reads or atomically changes team shares and the revocable public URL. Team flags may be repeated, and `--json` exposes the current shares plus team slugs available to the acting user. `download` gets every attachment linked to the thread. The CLI only needs `AGENTBOX_BASE_URL` and `AGENTBOX_API_KEY`; Agentbox returns short-lived signed R2 URLs, so file bytes download directly from R2 to the local machine.

## Web dashboard

Agentbox includes a simple browser viewer for inspecting threads and attachments:

```text
https://your-agentbox.vercel.app/threads
```

Create the permanent owner with `agentbox owner setup-token`, then invite every additional user from `/owner/users`. Browser login uses deployment-global email and password with no account-partition selector. The deployment admin key is only for issuing owner setup or recovery links and should never be stored in the dashboard. The unified inbox can filter accessible threads by Private, Shared with me, one of the signed-in user's teams, or active Public state; each card shows its current visibility and stable `User · Actor` attribution. Thread pages use the same attribution snapshots and render Markdown messages with GitHub-flavored tables, fenced code blocks, copy buttons, syntax highlighting for common languages, and inline Mermaid diagrams. Plain-text messages stay in source view.

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
bun run verify:cutover
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
```

Credentials are owned by one user, hashed in Postgres, independently attributable, and shown only once on creation. The permanent-owner browser can inspect deployment-wide credential metadata and force-revoke any credential from `/owner/users`, but secrets are never recoverable. Disabling a user revokes sessions, credentials, and pending CLI codes and removes every team membership in one transaction without deleting that user's threads, messages, assets, shares, or attribution snapshots. Enabling the account does not restore any of those access paths. After the backend and dashboard are deployed and migrated, issue the permanent-owner setup link:

```bash
agentbox owner setup-token \
  --base-url https://youragentbox-api.vercel.app \
  --app-url https://youragentbox.vercel.app \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --expires 30m
```

After a non-owner user is disabled, the owner browser can separately purge that user's uploaded attachment objects from `/owner/users`. Purge runs in bounded, resumable batches using each asset's exact stored R2 key. Thread/message rows, filenames, and attribution remain as tombstones; authenticated and public readers display `Attachment deleted by deployment owner` and receive no download or preview URL. Attachments uploaded by other users are never selected merely because they appear in the disabled user's threads.

The permanent-owner browser also has a separate read-only deployment content viewer at `/owner/content`. It can list, search, and inspect every thread, including another user's private threads, with optional user and team filters and non-purged attachment downloads. This bypass is intentionally isolated from the normal inbox and API authorization path: ordinary users, MCP/CLI/API credentials, owner API keys, and the deployment admin secret cannot use it, and the owner viewer exposes no posting, upload, or visibility controls.

Production upgrades to the user/team model must follow [`docs/user-team-sharing-production-cutover.md`](docs/user-team-sharing-production-cutover.md). It defines the verified backup gate, maintenance mode, the migration stop at `0016`, permanent-owner setup, irreversible migration `0017`, postcheck SQL, smoke matrix, and full-restore rollback procedure.

Use `agentbox login` for browser-assisted profile creation on each machine. A logged-in profile belongs to one user and can create or revoke that user's separate ChatGPT, Raycast, local, and automation credentials. There is no account-partition selector or deployment-wide daily credential.

## Docs

- [`public/setup-self-host.md`](public/setup-self-host.md)
- [`docs/user-team-sharing-spec.md`](docs/user-team-sharing-spec.md)
- [`docs/owner-setup.md`](docs/owner-setup.md)
- [`docs/user-invitations.md`](docs/user-invitations.md)
- [`docs/user-team-sharing-production-cutover.md`](docs/user-team-sharing-production-cutover.md)
