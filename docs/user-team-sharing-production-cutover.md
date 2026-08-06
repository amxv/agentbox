# User/team production cutover and rollback

This is the credentialed Phase 20 procedure. Run it only from a trusted local
machine with production PostgreSQL, Cloudflare R2, Vercel, AgentBox, macOS,
Raycast, and ChatGPT connector access. The shared Zodex implementation
environment must not execute these steps.

Do not execute this document until blueprint Phases 15-19 are complete and Phase
19 has updated this runbook, the postcheck, the companion Raycast developer-mode
smoke runbook, and the ChatGPT host-attachment smoke procedure for the final
extension, MCP schema, and migration set. The current text preserves the
already-tested core cutover sequence; Phase 20 uses the combined reviewed version.

The cutover is intentionally split around migration `0017`. Migrations through
`0016` create the replacement identity, team, visibility, and purge structures
while retaining temporary legacy schema. The permanent owner must then be
created explicitly. Migration `0017` verifies that every preserved thread has
that owner before removing the temporary schema; the same migration command
then continues through the reviewed additive migrations, including `0021`
credential inventory and `0022` immutable staged uploads. There is no down
migration.

## 1. Pin the reviewed branch state

```bash
git switch feat/user-team-sharing
git pull --ff-only origin feat/user-team-sharing
git status --short --branch
git log -1 --oneline
bun install --frozen-lockfile
bun run verify:cutover
```

Record the commit SHA in the maintenance log. Do not continue with local
changes, a failing verification command, or a commit different from the final
Phase 19 checkpoint.

## 2. Produce and restore-test the backup

Follow [`user-team-sharing-backup-preflight.md`](user-team-sharing-backup-preflight.md)
and keep the resulting directory outside the deployment:

```bash
export AGENTBOX_BACKUP_OUTPUT_DIR=/secure/off-host/agentbox-backups
export CUTOVER_RUN_ID=user-team-cutover-$(date -u +%Y%m%dT%H%M%SZ)

bun run backup:preflight -- \
  --run-id "$CUTOVER_RUN_ID" \
  --source-prefix agentbox/ \
  --backup-prefix "agentbox-recovery/$CUTOVER_RUN_ID"
```

Require `"ready": true`, preserve `database.dump` and `manifest.json`, run
`pg_restore --list`, restore the dump into a disposable database, compare all
four content counts, and sample recovery objects. Any missing object, orphan,
size mismatch, failed copy, or unexplained count difference blocks cutover.

## 3. Enter maintenance mode

Generate a temporary bypass key for the trusted operator shell. It is not an
AgentBox credential and grants no authorization by itself; it only passes the
maintenance gate, after which normal session/API-key checks still apply.

```bash
export AGENTBOX_MAINTENANCE_BYPASS_KEY="$(openssl rand -hex 32)"
```

On the **backend** Vercel project, set:

```text
AGENTBOX_AUTO_MIGRATE=false
AGENTBOX_MAINTENANCE_MODE=true
AGENTBOX_MAINTENANCE_BYPASS_KEY=<the generated value>
```

Deploy the reviewed backend commit, then deploy the dashboard commit with its
normal `AGENTBOX_BACKEND_URL`. Do not place the maintenance bypass key in the
dashboard environment or browser.

Both Vercel projects build from the same repository root and neither stores a
framework setting, so a bare `vercel --prod` auto-detects Next.js and would ship
the dashboard onto the backend project. Always pass the matching local config:

```bash
# backend (Go API)
vercel --prod --yes --scope <scope> \
  --local-config deploy/vercel/backend/vercel.json

# dashboard (Next.js), pinned to its own project without re-linking the checkout
VERCEL_ORG_ID=<team-id> VERCEL_PROJECT_ID=<dashboard-project-id> \
  vercel --prod --yes --scope <scope> \
  --local-config deploy/vercel/dashboard/vercel.json
```

After the backend deploy, confirm the build produced a Go lambda and not a
Next.js route manifest. A backend deployment that lists dozens of `/api/*` app
routes is the dashboard and must be redeployed with the backend local config.

Production also requires `AGENTBOX_APP_PUBLIC_URL` set to the dashboard origin as
a single absolute HTTPS origin; the API refuses to start without it. The owner setup endpoints and health endpoint
are explicitly available during maintenance; after setup, the permanent owner's
browser session is also allowed through the maintenance gate. All other browser
traffic receives HTTP 503.

Verify the gate before changing schema:

```bash
curl -fsS https://YOUR-BACKEND/api/health

curl -i https://YOUR-BACKEND/api/threads
# Expected: 503 with code MAINTENANCE_MODE

curl -i \
  -H "x-agentbox-maintenance-key: $AGENTBOX_MAINTENANCE_BYPASS_KEY" \
  https://YOUR-BACKEND/api/threads
# Expected: the maintenance gate is passed, then normal auth returns 401.
```

Keep the bypass key only in the trusted operator shell. CLI requests launched
from that shell include it automatically.

## 4. Apply the reversible side through migration 0016

With production backend environment loaded locally:

```bash
go run ./cmd/migrate --through 0016 --timeout 5m
```

Confirm `schema_migrations` ends at `0016`. Do not run the unbounded command yet.
At this point the replacement schema exists, old authentication data has been
intentionally reset, preserved content remains in place, and `0017` has not
removed the temporary schema.

## 5. Create the permanent owner

Issue the one-time link from the trusted shell:

```bash
agentbox owner setup-token \
  --base-url https://YOUR-BACKEND \
  --app-url https://YOUR-DASHBOARD \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --expires 30m
```

Open the printed URL once in the trusted browser and complete owner setup. The
owner insert trigger assigns every preserved thread that still lacks an owner
to this permanent owner without changing thread/message/asset IDs, timestamps,
ordering, attribution strings, or R2 keys.

Before continuing, verify exactly one enabled owner exists and no thread lacks
an owner:

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
select id, email, display_name, disabled_at from users where is_owner;
select count(*) as threads_without_owner from threads where owner_user_id is null;
SQL
```

The second query must return zero.

## 6. Apply the point-of-no-return migration

```bash
go run ./cmd/migrate --timeout 5m
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -f scripts/user-team-cutover-postcheck.sql | tee cutover-postcheck.txt
```

Migration `0017` refuses to run if owner backfill is incomplete. The postcheck
then requires migration `0022`, proves exactly one enabled owner exists, checks
all content relationships, validates the pending-upload state machine and exact
staging cleanup inventory, and confirms that no legacy table or column remains.

Compare the postcheck thread/message/asset/pending-upload counts with the backup
manifest. Also compare stable IDs, message ordering, bodies, content types,
historical actor snapshots, filenames, sizes, and every stored R2 key using the
restored backup or an exported verification query. Any unexplained mismatch
keeps maintenance mode enabled.

## 7. Recreate users and credentials, then run the smoke matrix

Old sessions, API keys, setup tokens, and local profiles are intentionally
invalid. Prove an old credential fails, then create fresh state only through the
new product paths:

1. From the permanent-owner browser, create signup invitations in
   `/owner/users` for a zero-team user and users belonging to overlapping teams.
2. Register those users. For direct API/curl checks during maintenance, include
   `x-agentbox-maintenance-key`; the header never replaces normal auth.
3. Use `agentbox login` for each test user and create distinct ChatGPT, Claude,
   local, Raycast, and automation credentials.
4. Exercise private thread create/list/search/get/post, multipart uploads, and
   checksum-bound direct uploads. For each direct upload, calculate lowercase
   hexadecimal SHA-256 over the exact bytes, send it with exact size/MIME in the
   intent, PUT the unchanged body with the returned headers, finalize by
   `upload_id`, and verify the persisted asset uses a distinct immutable final
   key. Replay the original PUT after finalization and prove the attachment and
   signed download identity do not change. Also exercise signed
   previews/downloads and CLI download.
5. Share one thread with one team and another with overlapping teams. Remove a
   membership and a share independently and prove access disappears immediately.
6. Use direct foreign thread and asset IDs from another user and require denial
   for read, post, upload finalization, signing, and download.
7. Enable a public URL, read it without authentication, revoke it, and prove the
   old token and attachment URLs no longer work.
8. Disable a non-owner user and prove browser sessions, API keys, pending CLI
   codes, and memberships are invalidated while content remains. Exercise a
   bounded attachment purge, including an already-missing R2 key, and verify the
   tombstone plus retry behavior.
9. In `/owner/content`, read another user's private thread and a non-purged
   attachment. Use the owner's API key, CLI profile, and MCP URL against normal
   endpoints and prove they cannot read that same private thread.
10. Confirm every timeline entry still shows stable `User · Actor` attribution.
11. Remote MCP connectors cannot pass the maintenance gate, because ChatGPT and
    Claude cannot send `x-agentbox-maintenance-key`; discovery receives
    `503 MAINTENANCE_MODE` and the host reports only a generic failure. Verify the
    MCP contract now using the bypass header from the trusted shell, then perform
    the actual ChatGPT/Claude/Raycast connector smokes after step 8 reopens writes:

    ```bash
    curl -fsS -X POST \
      -H "x-agentbox-maintenance-key: $AGENTBOX_MAINTENANCE_BYPASS_KEY" \
      -H 'content-type: application/json' \
      -H 'accept: application/json, text/event-stream' \
      -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
      "https://YOUR-BACKEND/api/mcp?key=<connector-key>"
    ```

12. Execute [`raycast-developer-mode-smoke.md`](raycast-developer-mode-smoke.md) on the local Mac against this exact deployed commit. Preserve sanitized evidence for all five filters, private creation, replies, ordered/colliding attachments, signed and unavailable states, visibility/self-revocation, per-installation rotation, owner non-bypass, and disabled-user invalidation.
13. Execute [`chatgpt-file-attachment-smoke.md`](chatgpt-file-attachment-smoke.md) against this exact deployed commit. Refresh/recreate the connector, run Scan Tools when available, attach “the file I just created” without exposing an ID/path/URL, compare exact bytes, verify filename/MIME plus `User · ChatGPT` message/asset attribution and private R2 persistence, prove text-only posting still works, and prove a literal sandbox path creates no partial state.
14. Drain the upload cleanup inventory in bounded batches before removing the
    operator secret. Repeat until `attempted` is zero, then verify that expired,
    rejected, abandoned, and stale-finalization staging/final-candidate keys are
    gone while every asset-backed final key remains:

    ```bash
    curl -fsS -X POST \
      -H "x-agentbox-maintenance-key: $AGENTBOX_MAINTENANCE_BYPASS_KEY" \
      "https://YOUR-BACKEND/api/admin/uploads/cleanup?limit=100"
    ```

Useful trusted-shell checks include:

```bash
export AGENTBOX_BASE_URL=https://YOUR-BACKEND
export AGENTBOX_MAINTENANCE_BYPASS_KEY

agentbox --profile owner doctor
agentbox --profile owner list --json
agentbox --profile owner search preservation-marker --json
agentbox --profile owner get <owned-thread-id> --json
agentbox --profile owner post <owned-thread-id> 'cutover smoke' --json
agentbox --profile owner download <owned-thread-id> \
  --output ./tmp/cutover-download --json
```

Record request/response evidence without recording cookies, setup tokens, API
keys, the admin secret, database URLs, or the maintenance bypass key.

## 8. Reopen writes

Only after backup, preservation, schema, authorization, attachment, public-link,
disablement, purge, and owner-browser checks pass:

1. Remove `AGENTBOX_MAINTENANCE_BYPASS_KEY` from the trusted shell and backend.
2. Set `AGENTBOX_MAINTENANCE_MODE=false` or remove it.
3. Keep `AGENTBOX_AUTO_MIGRATE=false` for normal production operation.
4. Redeploy the backend.
5. Verify health, ordinary browser login, one private create/post/download loop,
   and clean backend/dashboard logs.

Update the blueprint's Phase 20 checkpoint with the production commit, backup
and manifest locations, pre/post counts, migration ledger, smoke evidence, and
the time writes reopened.

## Rollback

There is no supported down migration after `0017`.

### Before migration 0017

Keep maintenance mode enabled. Stop the cutover, preserve the failed evidence,
and either correct the owner/preflight issue or restore the verified database
dump and referenced R2 recovery objects. Redeploy the prior known-good release
only after its expected schema and credentials are restored.

### After migration 0017

Keep maintenance mode enabled and do not attempt manual column/table recreation.
Restore the verified PostgreSQL dump as one unit, restore every referenced R2
object from the recorded recovery keys to its exact original key, deploy the
prior known-good backend/dashboard configuration, and compare restored counts,
IDs, ordering, attribution, and object metadata with `manifest.json`. Writes may
reopen only when the restored system passes its previous-version smoke checks.

If the defect is in the new code rather than migrated data, prefer a narrow fix
on `feat/user-team-sharing`, run `bun run verify:cutover`, commit and push it,
redeploy under maintenance, and repeat the blocked checks. Never force-push or
continue with unexplained preservation differences.
