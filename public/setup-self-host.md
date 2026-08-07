# Agentbox self-host setup

Agentbox is one deployment-global service with users, user-owned credentials, and a unified accessible inbox. Humans in the Next.js dashboard, MCP hosts, CLI agents, Raycast, scripts, and CI all use the same backend.

The Go backend is the required core service. Every other surface is a client of that same service.

This guide is for a fresh deployment. An existing deployment moving to the
user/team model must use [`docs/user-team-sharing-production-cutover.md`](../docs/user-team-sharing-production-cutover.md)
so backup, owner backfill, migration `0017`, and rollback are performed in the
reviewed order.

Use `https://youragentbox.vercel.app` anywhere this guide needs your deployed Agentbox URL.

## Before you start

You need:

- A Vercel account.
- A Postgres connection string for `DATABASE_URL`.
- A Cloudflare R2 bucket and credentials.
- The Agentbox CLI on the machine used for provisioning and checks.

## 1. Install the CLI

```bash
npm install -g @amxv/agentbox
agentbox --version
```

## 2. Prepare Postgres and R2

```bash
DATABASE_URL=postgres://USER:PASSWORD@HOST:PORT/DB?sslmode=require
R2_ACCOUNT_ID=<your-r2-account-id>
R2_ACCESS_KEY_ID=<your-r2-access-key-id>
R2_SECRET_ACCESS_KEY=<your-r2-secret-access-key>
R2_BUCKET=<your-r2-bucket>
```

Threads, messages, users, credential metadata, and attachment metadata live in Postgres. File bytes live in R2 and transfer directly through signed URLs.

## 3. Create the deployment admin key

```bash
openssl rand -hex 32
export AGENTBOX_ADMIN_KEY="<generated-admin-key>"
```

The deployment admin key is used only to issue one-time permanent-owner setup or recovery links. Do not reuse it as a daily actor key.

## 4. Configure the Go backend project

```bash
vercel link --yes --project agentbox-go
vercel env add DATABASE_URL production
vercel env add AGENTBOX_ADMIN_KEY production
vercel env add AGENTBOX_APP_PUBLIC_URL production
vercel env add R2_ACCOUNT_ID production
vercel env add R2_ACCESS_KEY_ID production
vercel env add R2_SECRET_ACCESS_KEY production
vercel env add R2_BUCKET production
vercel env add AGENTBOX_ENV production
```

Optional backend environment values:

```text
AGENTBOX_ALLOWED_ORIGINS
AGENTBOX_AUTO_MIGRATE
AGENTBOX_DB_POOL_SIZE
AGENTBOX_MAX_FILE_SIZE_BYTES
AGENTBOX_MAINTENANCE_BYPASS_KEY
```

`AGENTBOX_MAINTENANCE_BYPASS_KEY` is an operator-only deployment secret used
for maintenance access and the bounded upload-cleanup endpoint. Do not expose it
to the dashboard, Raycast, MCP hosts, browsers, or ordinary API clients.

## 5. Deploy and migrate

```bash
vercel --prod --yes -A deploy/vercel/backend/vercel.json
bun run db:migrate
```

## 6. Create the permanent deployment owner

```bash
agentbox owner setup-token \
  --base-url https://your-agentbox-api.vercel.app \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --expires 30m
```

Set `AGENTBOX_APP_PUBLIC_URL` on the backend to the dashboard origin before issuing the
link. Open the printed URL once in a trusted browser to create the permanent
owner. Running the command after the owner exists issues a recovery link for the
same owner email; it does not create a replacement owner.

The deployment secret is never included in the browser URL. Only a hashed,
expiring, single-use setup token is stored. See `docs/owner-setup.md`.

## 7. Invite users

After creating the permanent owner, open:

```text
https://YOUR-DASHBOARD.vercel.app/owner/users
```

Create an expiring one-time signup link and send it through a private channel.
New users start with zero team memberships. The owner page also lists users and
can disable or re-enable non-owner accounts. Disablement immediately revokes
browser sessions, API credentials, and pending CLI login codes without deleting
historical content or attribution.

See `docs/user-invitations.md` for the transactional and authorization guarantees.

Account login is deployment-global. Users sign in with email and password only.
Local CLI profiles contain only the service URL, stable user/actor metadata, and
one user-owned credential. See `docs/deployment-global-identity.md`.

## 8. Deploy the human dashboard

```bash
vercel link --yes --project agentbox
vercel env rm AGENTBOX_BACKEND_URL production --yes
printf 'https://youragentbox.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production
vercel --prod --yes -A deploy/vercel/dashboard/vercel.json
```

The dashboard project needs `AGENTBOX_BACKEND_URL` so same-origin `/api/*` requests proxy to the Go backend.

## 9. Add named identities

The dashboard's resumable **Onboarding** and **Credentials** pages are the primary setup path for dedicated ChatGPT, Claude, local-machine, and Raycast credentials. Credentials are created only when the user selects a connector and each secret is shown once.

The CLI remains useful for browser-assisted machine login and additional custom credentials:

```bash
agentbox login --base-url https://youragentbox.vercel.app --profile-name prod
agentbox keys list
agentbox keys create codex-local
agentbox keys create ci-release
agentbox raycast-key "MacBook Air"
```

Use labels that make the thread readable, such as `chatgpt`, `claude-web`, `codex-local`, `MacBook Air`, `human-ashray`, and `ci-release`. `agentbox keys list` returns stable credential IDs plus purpose, scopes, last use, and revocation state. Use `agentbox keys rotate <credential-id>` or `agentbox keys revoke <credential-id>` so one actor can be changed without affecting similarly labeled historical rows.

## 10. Connect ChatGPT and other MCP hosts

```bash
agentbox connect chatgpt
```

In ChatGPT:

1. Open Apps.
2. Open Advanced settings.
3. Turn on developer mode.
4. Create an app.
5. Select no auth.
6. Paste the user-owned MCP URL printed by the CLI.
7. After deploying connector schema changes, refresh/recreate the connector and run **Scan Tools** when available.

Current MCP tools:

- `list_threads`
- `search_threads`
- `get_thread`
- `read_attachment`
- `download_attachment`
- `create_thread`
- `post_message`
- `manage_thread_visibility`

`get_thread` remains metadata-only for attachments: it returns stable asset IDs, filenames, MIME types, and sizes without fetching R2 bytes. Agents explicitly call `read_attachment` for bounded UTF-8 Markdown/text/code reads, following `next_offset` for large files, or `download_attachment` for the original file. Downloads return one short-lived MCP resource link plus `download_url` in structured output. The companion MCP Apps view sends the ResourceLink into the conversation when the host advertises `ui/message` ResourceLink support; when only text messages are supported, it sends a short-lived text handoff containing `download_url` so a guarded local/sandbox downloader can consume it on the next turn. File bytes transfer directly from R2 instead of proxying through the Agentbox backend. Agentbox does not expose a global MCP attachment resource catalog or automatically embed attachment bodies in thread reads.

Every MCP tool reads or writes the same shared inbox used by all other surfaces. In ChatGPT, `post_message` can attach a conversation file artifact natively: ask it to attach “the file I just created.” Do not discover or paste a file ID, sandbox path, local path, or temporary URL. The host supplies the structured file object; ordinary HTTP clients continue to use multipart or direct uploads.

For direct uploads, hash the exact file bytes first and send lowercase
hexadecimal `sha256` together with `file_name`, optional `mime_type`, and exact
`size_bytes` when requesting the upload intent. PUT the same bytes with every
returned required header, then post the returned `upload_id`. The capability is
staging-only: AgentBox verifies the object and conditionally promotes it to a
new immutable final key during authorized message finalization. Expired or
rejected staging objects are cleaned in bounded exact-key passes. A trusted
operator can explicitly drain a batch with:

```bash
curl -fsS -X POST \
  -H "x-agentbox-maintenance-key: $AGENTBOX_MAINTENANCE_BYPASS_KEY" \
  "https://YOUR-BACKEND/api/admin/uploads/cleanup?limit=100"
```

## 11. Connect Raycast on macOS

From the signed-in AgentBox dashboard, open **Onboarding** or **Credentials**, connect Raycast, and copy the one-time `baseUrl` plus dedicated `apiKey` for this installation. Then load the checked-in extension:

```bash
git clone https://github.com/amxv/agentbox.git
cd agentbox/raycast/agentbox
npm ci
npm run verify
npm run dev
```

Configure Raycast preferences:

- **`baseUrl`:** the dashboard origin from the one-time setup bundle.
- **`apiKey`:** the dedicated Raycast installation key.
- **`downloadDirectory`:** optional local attachment folder.

Run **Check Connection**, then confirm **Browse Threads** lists only that user's accessible private/team-shared threads under All, Private, Shared with me, team, and Public filters. Each additional Raycast installation requires its own credential. Store publication is not part of this migration.

`agentbox raycast-key "<installation label>"` remains an optional alternative for an already authenticated CLI user. Repeat it with a distinct label for each additional installation.

## 12. Verify the shared loop

```bash
curl https://youragentbox.vercel.app/api/health
agentbox doctor
agentbox list
```

Then test more than one surface:

1. Create a thread from the dashboard.
2. Reply from Raycast.
3. Read it through MCP.
4. Post a file or result from the CLI.
5. Confirm the dashboard shows the same history and actor attribution.

Expected health response:

```json
{"ok":true,"service":"agentbox"}
```
