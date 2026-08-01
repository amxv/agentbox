# Agentbox Agent Guide

Short operating guide for agents maintaining and deploying this repo. For the full rollout checklist, read `docs/go-rollout.md`.

## Runtime Shape

Agentbox is split into two Vercel services:

- `agentbox-go`: Go backend for `/api/*`, `/api/mcp`, Postgres, R2 attachments, migrations, and the Go CLI implementation.
- `agentbox`: Next.js dashboard for `/`, `/threads`, and `/threads/:threadId`. Its `app/api/*` routes are thin proxies to the Go backend.

Production URLs:

```text
Go backend: https://agentbox-go.vercel.app
Dashboard:  https://agentbox-black.vercel.app
Dashboard:  https://agentbox.amaimmigration.com
```

The dashboard must have:

```text
AGENTBOX_BACKEND_URL=https://agentbox-go.vercel.app
```

## Local Checks

Run the broad gate before shipping cross-runtime changes:

```bash
bun run test:parity
bun run typecheck
bun run lint
go test ./...
go vet ./...
bun run build:api
bun run build:cli
```

Builds:

```bash
bun run build        # Next.js dashboard
bun run build:api    # Go backend binary at dist/agentbox-api
bun run build:cli    # Go CLI binary at dist/agentbox
```

The local global CLI should point at the Go build output:

```bash
ln -sf "$PWD/dist/agentbox" "$HOME/.local/bin/agentbox"
agentbox --version
```

After this, every `bun run build:cli` refreshes the binary used by `agentbox`.

## Deploy Backend

The backend project is `agentbox-go`.

Link or deploy with the backend config:

```bash
vercel link --yes --project agentbox-go
vercel --prod --yes -A deploy/vercel/backend/vercel.json
```

Required production env on `agentbox-go`:

```text
DATABASE_URL
AGENTBOX_API_KEYS
AGENTBOX_ADMIN_KEYS or AGENTBOX_ADMIN_KEY
R2_ACCOUNT_ID
R2_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY
R2_BUCKET
AGENTBOX_ENV=production
```

Optional backend env:

```text
AGENTBOX_ALLOWED_ORIGINS
AGENTBOX_AUTO_MIGRATE
AGENTBOX_DB_POOL_SIZE
AGENTBOX_MAX_FILE_SIZE_BYTES
R2_PUBLIC_BASE_URL
```

Verify backend:

```bash
curl https://agentbox-go.vercel.app/api/health
```

Expected:

```json
{"ok":true,"service":"agentbox"}
```

## Run Migrations

Run migrations from a trusted shell with backend production env loaded:

```bash
bun run db:migrate
```

This runs:

```bash
go run ./cmd/migrate
```

The ordered SQL files under `migrations/` are embedded in the Go binary and are
the canonical schema history. Applied versions and checksums are recorded in
`schema_migrations`. Normal repository requests never execute schema DDL.

Do not rely on `AGENTBOX_AUTO_MIGRATE=true` for production by default.

## Permanent Owner Setup

The deployment admin secret is not a daily actor credential. Use it only from a
trusted operator shell to issue a one-time owner bootstrap or recovery link:

```bash
agentbox owner setup-token \
  --base-url https://api.agentbox.example.com \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --expires 30m
```

Set `APP_PUBLIC_URL` on the Go backend to the Next.js dashboard origin when the
projects are deployed separately. The token is hash-only in PostgreSQL,
single-use, expiring, and replaced whenever a new token is issued. Recovery is
restricted to the same permanent owner email. Browser sessions and API keys
cannot issue setup tokens, and owner API keys never receive owner-browser-only
authority. See `docs/owner-setup.md`.

## User/team Cutover Backup Preflight

Before any production user/team authorization cutover, run the content backup
preflight from a trusted machine with production PostgreSQL/R2 env loaded and a
compatible `pg_dump` installed:

```bash
bun run backup:preflight -- \
  --output-dir /secure/off-host/agentbox-backups \
  --run-id user-team-cutover-YYYY-MM-DD \
  --source-prefix agentbox/ \
  --backup-prefix agentbox-recovery/user-team-cutover-YYYY-MM-DD
```

The command writes `database.dump` and `manifest.json`, copies every referenced
R2 object to the recovery prefix, and exits non-zero for missing objects, orphaned
content rows, mismatched sizes, failed copies, or dump failures. Do not continue
unless the manifest says `"ready": true`. Rerun the same run ID to resume safely.

Read `docs/user-team-sharing-backup-preflight.md` for separate-bucket options,
manifest semantics, and restore verification. The Zodex machine has no production
credentials; the real backup and recorded production counts are a credentialed
local-agent gate.

## Deploy Dashboard

The dashboard project is `agentbox`.

Make sure the local Vercel link points back to the dashboard project:

```bash
vercel link --yes --project agentbox
```

Set or replace the backend URL on the dashboard project:

```bash
vercel env rm AGENTBOX_BACKEND_URL production --yes
printf 'https://agentbox-go.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production
```

Deploy:

```bash
vercel --prod --yes -A deploy/vercel/dashboard/vercel.json
```

Verify dashboard and proxy:

```bash
curl -i https://agentbox-black.vercel.app/threads
curl -i https://agentbox-black.vercel.app/api/health
```

The `/api/health` request should return the Go backend health JSON through the dashboard proxy.

## CLI Smoke

Use the stored `ashray` profile for the public product URL:

```bash
agentbox --profile ashray doctor
agentbox --profile ashray list
agentbox --profile ashray get <thread-id>
agentbox --profile ashray download <thread-id> --output tmp/agentbox-download-smoke --json
```

If the profile is missing:

```bash
agentbox profiles add ashray \
  --base-url https://agentbox-black.vercel.app \
  --api-key '<valid-api-key>' \
  --activate
```

The `doctor` command should pass health, authenticated API, signed download URL, and MCP URL checks.

## Common Failure Modes

- `go: command not found` during Vercel dashboard install: the dashboard package `prepare` script must skip CLI build when `VERCEL=1`.
- Dashboard `/api/*` returns `AGENTBOX_BACKEND_URL or AGENTBOX_GO_BACKEND_URL must be set`: set `AGENTBOX_BACKEND_URL` on the `agentbox` dashboard project and redeploy.
- Backend health fails: inspect `agentbox-go` env and deployment logs first.
- CLI command not found or points to old `dist/index.js`: relink `~/.local/bin/agentbox` to `dist/agentbox`.
- Attachments larger than Vercel's request/response cap need a direct-to-R2 upload flow; current Vercel multipart uploads must stay below that cap.

## Cleanup

Remove temporary files that may contain env values:

```bash
rm -rf tmp/*env* tmp/vercel-env-values tmp/agentbox-download-smoke
```

Keep `main` clean before and after deploy work:

```bash
git status --short --branch
```
