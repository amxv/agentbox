# Agentbox Go Rollout

This document is the rollout checklist for the split-runtime Agentbox port.

## Runtime Shape

Agentbox now has two runtime services:

- Go backend service: owns `/api/*`, including `/api/mcp`, PostgreSQL access, R2 attachment handling, migrations, and the `agentbox` CLI implementation.
- Next.js dashboard service: owns page routes such as `/`, `/threads`, and `/threads/:threadId`.

The public product URL should remain same-origin for clients:

```text
https://agentbox.example.com/api/*       -> Go backend service
https://agentbox.example.com/api/mcp     -> Go backend service
https://agentbox.example.com/            -> Next.js dashboard service
https://agentbox.example.com/threads     -> Next.js dashboard service
```

The dashboard keeps lightweight Next.js API proxy routes under `app/api/*` so preview deployments can still make same-origin browser requests. Set `AGENTBOX_BACKEND_URL` on the dashboard service to the Go backend URL. `AGENTBOX_GO_BACKEND_URL` is kept as a compatibility alias.

## Vercel Services

Use separate Vercel Services or projects for the Go backend and the Next.js dashboard. Do not add a root `vercel.json` for both services because one project-level `framework` cannot be both `go` and `nextjs`.

Templates are checked in for service setup:

```text
deploy/vercel/backend/vercel.json
deploy/vercel/dashboard/vercel.json
```

For the backend service:

- Framework preset: `go`.
- Entrypoint: `cmd/api/main.go`.
- Build expectation: Vercel detects root `go.mod` and `cmd/api/main.go`.
- Runtime expectation: the server listens on `PORT`.
- Region: start with `iad1` unless the database is in another region.

For the dashboard service:

- Framework preset: `nextjs`.
- Build command: `bun run build`.
- Set `AGENTBOX_BACKEND_URL` to the Go backend preview or production URL.

## Backend Environment

Set these on the Go backend service:

```text
DATABASE_URL
AGENTBOX_ADMIN_KEY
R2_ACCOUNT_ID
R2_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY
R2_BUCKET
AGENTBOX_ENV=production
```

Optional backend variables:

```text
AGENTBOX_ALLOWED_ORIGINS
AGENTBOX_AUTO_MIGRATE
AGENTBOX_DB_POOL_SIZE
AGENTBOX_MAX_FILE_SIZE_BYTES
R2_PUBLIC_BASE_URL
```

`AGENTBOX_AUTO_MIGRATE=true` is acceptable for a preview smoke test but should not be the production default. Production rollout should run `bun run db:migrate` once before traffic is cut over.

API keys are not managed through Vercel environment rewrites. After the backend is deployed and migrated, create the first local and ChatGPT keys through the backend admin API:

```bash
agentbox init \
  --profile-name prod \
  --base-url https://agentbox-go-preview-or-prod.example.com \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --local-key-name local \
  --chatgpt-key-name chatgpt
```

Use `agentbox keys create|list|revoke` for later DB-backed key management.

Vercel currently caps function request and response payloads at 4.5 MB. On Vercel, direct multipart uploads through `/api/threads/:threadId/messages` must stay below that cap until a direct-to-R2 upload flow exists. Non-Vercel/self-hosted deployments can keep the current default `AGENTBOX_MAX_FILE_SIZE_BYTES` of 25 MiB.

## Dashboard Environment

Set these on the Next.js dashboard service:

```text
AGENTBOX_BACKEND_URL=https://agentbox-go-preview-or-prod.example.com
```

The browser viewer stores the admin key locally and sends it as `x-agentbox-admin-key` to `/api/viewer/*`. The dashboard proxy forwards that same-origin request to the Go backend.

## Migrations

The checked-in schema is `migrations/0001_init.sql`, and the Go repository applies the same idempotent schema through `Repository.EnsureSchema`.

Run migrations from a trusted environment with backend env vars loaded:

```bash
bun run db:migrate
```

This runs:

```bash
go run ./cmd/migrate
```

Migration policy:

- Preview: run `bun run db:migrate` before smoke tests, or temporarily set `AGENTBOX_AUTO_MIGRATE=true`.
- Production: run `bun run db:migrate` before shifting traffic.
- Rollback: keep the prior TypeScript deployment and database schema available. The Go schema is intentionally compatible with the existing `threads`, `messages`, and `assets` tables.

## CLI Distribution

The release CLI is now the Go command at `cmd/agentbox`.

Local build:

```bash
bun run build:cli
./dist/agentbox doctor
```

Release build matrix:

```bash
bun run build:cli:all
```

This writes OS/architecture binaries under `dist/release/` for:

```text
agentbox-darwin-arm64
agentbox-darwin-amd64
agentbox-linux-amd64
agentbox-linux-arm64
agentbox-windows-amd64.exe
```

Recommended distribution order:

1. Publish binaries from `dist/release/` to GitHub Releases.
2. Add a Homebrew tap formula pointing at the macOS/Linux release artifacts.
3. If npm global install compatibility must be preserved, publish a wrapper package that downloads the correct Go binary. The current package builds a local `dist/agentbox` binary for development but is not yet a cross-platform npm binary installer.

## Preview Smoke Checks

Run these against the Go backend preview URL:

```bash
curl https://agentbox-go-preview.example.com/api/health
agentbox profiles add preview --base-url https://agentbox-go-preview.example.com --api-key LOCAL_KEY --activate
agentbox doctor
agentbox create "Go preview smoke"
agentbox list
agentbox post thr_xxx "Go backend smoke message"
agentbox get thr_xxx
```

Run MCP validation:

```text
https://agentbox-go-preview.example.com/api/mcp?key=CHATGPT_KEY
```

Validate the dashboard preview:

- Set dashboard `AGENTBOX_BACKEND_URL` to the Go backend preview URL.
- Open `/threads`.
- Add a viewer profile with the admin key.
- Open a thread and verify messages and attachment links render.

## Production Cutover

1. Deploy Go backend preview.
2. Run `bun run db:migrate`.
3. Run CLI, REST, dashboard, and MCP smoke checks.
4. Deploy dashboard preview with `AGENTBOX_BACKEND_URL` pointed at the Go backend preview.
5. Point production API/MCP routing to the Go backend service and page routing to the Next.js dashboard service.
6. Update local CLI profiles to the production same-origin URL.
7. Keep the old TypeScript deployment available for rollback until production MCP, CLI, and dashboard traffic has been stable.

## Superseded TypeScript Code

The superseded TypeScript backend, CLI, migration helper, and parity harness were removed after the Go cutover. Use Git history if you need to inspect the old implementation.

The active Next.js dashboard and dashboard proxy routes remain under `app/`; backend, MCP, migrations, and CLI behavior are owned by the Go code.
