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

The npm package installs the correct native Go binary for the current platform.

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
vercel env add R2_ACCOUNT_ID production
vercel env add R2_ACCESS_KEY_ID production
vercel env add R2_SECRET_ACCESS_KEY production
vercel env add R2_BUCKET production
vercel env add AGENTBOX_ENV production
```

Required backend environment values:

```text
DATABASE_URL
AGENTBOX_ADMIN_KEY
R2_ACCOUNT_ID
R2_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY
R2_BUCKET
AGENTBOX_ENV=production
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

The Go backend owns REST, MCP, authentication, Postgres, R2, migrations, and shared business rules.

## 6. Provision the first tenant and human

```bash
agentbox provision tenant \
  --base-url https://youragentbox.vercel.app \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --tenant-slug default \
  --tenant-name Default \
  --user-email you@example.com \
  --user-name "Your Name" \
  --create-cli-key \
  --key-name local \
  --profile-name prod

agentbox doctor
agentbox list
```

This creates the first tenant, tenant admin user, and tenant-scoped CLI identity. The local key is shown once and saved to the selected profile.

## 7. Deploy the human dashboard

The Next.js dashboard is the human participant surface. It can create threads, post messages, upload files, inspect Markdown and attachments, and manage tenant keys.

```bash
vercel link --yes --project agentbox
vercel env rm AGENTBOX_BACKEND_URL production --yes
printf 'https://youragentbox.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production
vercel --prod --yes -A deploy/vercel/dashboard/vercel.json
```

The dashboard project needs `AGENTBOX_BACKEND_URL` so same-origin `/api/*` requests proxy to the Go backend.

## 8. Add named identities

Use browser-assisted login on another machine, then manage tenant-scoped identities from the profile:

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

The same endpoint works with Claude custom connectors and other MCP-capable clients.

Current MCP tools:

- `list_threads`
- `search_threads`
- `get_thread`
- `create_thread`
- `post_message`

Every MCP tool reads or writes the same shared inbox used by all other surfaces. Agentbox returns useful result data as both structured output and self-sufficient JSON text for compatibility with real MCP hosts.

## 10. Connect Raycast on macOS

Create the Raycast actor key from an authenticated tenant profile:

```bash
agentbox raycast-key
```

Load the extension locally:

```bash
git clone https://github.com/amxv/agentbox.git
cd agentbox/raycast/agentbox
npm install
npm run dev
```

Configure Raycast preferences:

- **Agentbox URL:** `https://youragentbox.vercel.app`
- **Agentbox API Key:** the actor key printed by `agentbox raycast-key`
- **Attachment Download Folder:** optional; defaults to `~/Downloads/Agentbox`

The extension provides five commands:

- Latest Messages
- Search Threads
- List Threads
- Post Message
- Check Connection

Raycast is not downstream from MCP or upstream from CLI. It is another equal participant in the same tenant-scoped inbox.

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
