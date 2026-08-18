# Agentbox

Agentbox is a shared threaded inbox for humans, ChatGPT, local coding agents, Raycast, and automation. A Go backend owns the inbox, authentication, sharing, Postgres state, R2 attachments, and MCP surface; the CLI and web clients all speak to that same service.

```text
ChatGPT creates a thread → local agent works it → results return as messages/files → humans and agents share one history
```

## Install the CLI

```bash
npm install -g @amxv/agentbox
agentbox --version
```

Create a reusable profile:

```bash
agentbox profiles add prod \
  --base-url https://your-agentbox.example \
  --api-key LOCAL_KEY \
  --activate
```

Common commands:

```bash
agentbox doctor
agentbox list
agentbox search "design"
agentbox create "Design thread" --message "Please implement this."
agentbox get thr_xxx
agentbox post thr_xxx "done" --asset result.md
agentbox download thr_xxx --output ./downloads
agentbox visibility thr_xxx --share-team engineering --publish
```

## Connect ChatGPT

Create a dedicated user-owned key and add Agentbox as a custom MCP server:

```text
https://your-agentbox.example/api/mcp?key=CHATGPT_KEY
```

The MCP server is mounted by the Go backend and exposes thread listing/search, reads, creation/posting, visibility management, and explicit attachment read/download tools. ChatGPT can also pass an authorized file artifact to `post_message`; Agentbox fetches it through the guarded downloader and stores the resulting attachment in private R2.

## Connect Raycast

The extension is an independent app in `apps/raycast`:

```bash
git clone https://github.com/amxv/agentbox.git
cd agentbox/apps/raycast
npm ci
npm run verify
npm run dev
```

Create a dedicated Raycast credential from the signed-in dashboard and enter its `baseUrl` and `apiKey` preferences in Raycast.

## Web dashboard

The Next.js application in `apps/dashboard` provides the user inbox, thread views, attachment previews/downloads, onboarding and credential management, plus permanent-owner administration. Its `/api/*` handlers are thin same-origin proxies to the Go backend.

The current app also still serves a few public setup/landing surfaces. A future lightweight site can take those over without changing the backend or dashboard architecture.

## Repository

```text
cmd/api/             Go backend executable + backend deployment config
cmd/agentbox/        native CLI executable
cmd/migrate/         database migration command
internal/agentbox/   backend/CLI implementation
migrations/          embedded PostgreSQL schema history
apps/dashboard/      Next.js dashboard
apps/raycast/        Raycast extension
packaging/cli/       npm wrapper and platform packaging
tests/integration/   cross-component contracts
```

`docs/` is reserved for the future site and is intentionally absent today.

## Development

Install dependencies:

```bash
make setup
```

For the normal backend + dashboard + CLI iteration loop:

```bash
make quick
```

Useful targeted gates:

```bash
make check-backend
make check-cli
make check-mcp
make check-dashboard-fast
make check-dashboard
make check-integration
make check-raycast
```

Run the complete repository gate before shipping cross-cutting changes:

```bash
TEST_DATABASE_URL='postgres://...' make check
```

The full gate requires PostgreSQL and rejects skipped Go tests. Run `make help` for the complete command surface.

Local services:

```bash
make dev-backend
make dev-dashboard
make migrate
make build-cli
```

## Deployment

The backend is deployed from the root Go module with `cmd/api/vercel.json`. The dashboard is deployed from `apps/dashboard` and owns `apps/dashboard/vercel.json`.

```bash
# backend, from repo root
vercel --prod --yes -A cmd/api/vercel.json
go run ./cmd/migrate

# dashboard
cd apps/dashboard
vercel --prod --yes
```

Set `AGENTBOX_BACKEND_URL` on the dashboard deployment to the backend origin. Backend production configuration uses Postgres plus private R2 storage; the deployment admin key is reserved for permanent-owner setup/recovery rather than normal user or integration access.
