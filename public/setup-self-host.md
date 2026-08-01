# Agentbox self-host setup

Agentbox is one tenant-scoped shared inbox. Humans in the Next.js dashboard, MCP hosts, CLI agents, Raycast, scripts, and CI all read and write the same threads, messages, and files.

The Go backend is the required core service. Every other surface is a client of that same service.

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

Threads, messages, tenants, users, API-key metadata, and attachment metadata live in Postgres. File bytes live in R2 and transfer directly through signed URLs.

## 3. Create the deployment admin key

```bash
openssl rand -hex 32
export AGENTBOX_ADMIN_KEY="<generated-admin-key>"
```

The deployment admin key is used for provisioning and deployment-level administration. Do not reuse it as a daily actor key.

## 4. Configure the Go backend project

```bash
vercel link --yes --project agentbox-go
vercel env add DATABASE_URL production
vercel env add AGENTBOX_ADMIN_KEY production
vercel env add APP_PUBLIC_URL production
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
R2_PUBLIC_BASE_URL
```

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

Set `APP_PUBLIC_URL` on the backend to the dashboard origin before issuing the
link. Open the printed URL once in a trusted browser to create the permanent
owner. Running the command after the owner exists issues a recovery link for the
same owner email; it does not create a replacement owner.

The deployment secret is never included in the browser URL. Only a hashed,
expiring, single-use setup token is stored. See `docs/owner-setup.md`.

## 7. Deploy the human dashboard

```bash
vercel link --yes --project agentbox
vercel env rm AGENTBOX_BACKEND_URL production --yes
printf 'https://youragentbox.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production
vercel --prod --yes -A deploy/vercel/dashboard/vercel.json
```

The dashboard project needs `AGENTBOX_BACKEND_URL` so same-origin `/api/*` requests proxy to the Go backend.

## 8. Add named identities

```bash
agentbox login --base-url https://youragentbox.vercel.app --profile-name prod
agentbox keys list
agentbox keys create codex-local
agentbox keys create ci-release
agentbox raycast-key
```

Use names that make the thread readable, such as `chatgpt`, `claude-web`, `codex-local`, `raycast`, `human-ashray`, and `ci-release`.

## 9. Connect ChatGPT and other MCP hosts

```bash
agentbox connect chatgpt
```

In ChatGPT:

1. Open Apps.
2. Open Advanced settings.
3. Turn on developer mode.
4. Create an app.
5. Select no auth.
6. Paste the tenant-scoped MCP URL printed by the CLI.

Current MCP tools:

- `list_threads`
- `search_threads`
- `get_thread`
- `create_thread`
- `post_message`

Every MCP tool reads or writes the same shared inbox used by all other surfaces.

## 10. Connect Raycast on macOS

```bash
agentbox raycast-key

git clone https://github.com/amxv/agentbox.git
cd agentbox/raycast/agentbox
npm install
npm run dev
```

Configure Raycast preferences:

- **Agentbox URL:** `https://youragentbox.vercel.app`
- **Agentbox API Key:** the actor key printed by `agentbox raycast-key`
- **Attachment Download Folder:** optional; defaults to `~/Downloads/Agentbox`

## 11. Verify the shared loop

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
