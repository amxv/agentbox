# Agentbox self-host setup

This guide is for operators deploying their own Agentbox instance. The npm CLI is maintained by Agentbox; the backend and optional dashboard run in your own Vercel account.

Use `https://youragentbox.vercel.app` anywhere this guide needs your deployed Agentbox backend URL.

## 1. Install the CLI

```bash
npm install -g @amxv/agentbox
agentbox --version
```

## 2. Prepare backend inputs

You need:

- A Vercel account.
- A Postgres connection string for `DATABASE_URL`.
- A Cloudflare R2 bucket and credentials.
- One admin key for setup and key management.

Generate the admin key locally:

```bash
openssl rand -hex 32
export AGENTBOX_ADMIN_KEY="<generated-admin-key>"
```

## 3. Deploy the Go backend

The Go backend is the required service. It owns `/api/*`, `/api/mcp`, Postgres access, R2 attachments, migrations, and API-key management.

Link the backend project:

```bash
vercel link --yes --project agentbox-go
```

Set production environment variables on the backend project:

```bash
vercel env add DATABASE_URL production
vercel env add AGENTBOX_ADMIN_KEY production
vercel env add R2_ACCOUNT_ID production
vercel env add R2_ACCESS_KEY_ID production
vercel env add R2_SECRET_ACCESS_KEY production
vercel env add R2_BUCKET production
vercel env add AGENTBOX_ENV production
```

Optional backend env vars:

```bash
vercel env add AGENTBOX_ALLOWED_ORIGINS production
vercel env add AGENTBOX_AUTO_MIGRATE production
vercel env add AGENTBOX_DB_POOL_SIZE production
vercel env add AGENTBOX_MAX_FILE_SIZE_BYTES production
vercel env add R2_PUBLIC_BASE_URL production
```

Deploy the backend:

```bash
vercel --prod --yes -A deploy/vercel/backend/vercel.json
```

After deploy, use your backend URL as the Agentbox base URL. In examples below:

```bash
export AGENTBOX_BASE_URL="https://youragentbox.vercel.app"
```

## 4. Run migrations

Run migrations with backend production env loaded:

```bash
bun run db:migrate
```

This creates the thread/message/asset tables and the DB-backed `api_keys` table.

## 5. Initialize API keys

Use the admin key once to create:

- `local`: saved into your local CLI profile.
- `chatgpt`: printed once for ChatGPT MCP setup.

```bash
agentbox init \
  --profile-name prod \
  --base-url "$AGENTBOX_BASE_URL" \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --local-key-name local \
  --chatgpt-key-name chatgpt
```

Verify the local profile:

```bash
agentbox doctor
agentbox list
```

Manage keys later through the backend admin API:

```bash
agentbox keys list --admin-key "$AGENTBOX_ADMIN_KEY"
agentbox keys create worker --admin-key "$AGENTBOX_ADMIN_KEY"
agentbox keys revoke worker --admin-key "$AGENTBOX_ADMIN_KEY"
```

## 6. Connect ChatGPT

Print the MCP URL and setup steps:

```bash
agentbox connect chatgpt
```

In ChatGPT:

1. Open Apps.
2. Go to Advanced settings.
3. Turn on developer mode.
4. Create an app.
5. Select no auth.
6. Paste the MCP URL from the CLI.

Agentbox expects no auth in the ChatGPT app config because the API key is already embedded in the MCP URL.

## 7. Deploy the optional dashboard

The Next.js dashboard is optional. It provides a browser inbox for threads, messages, and attachments.

Link the dashboard project:

```bash
vercel link --yes --project agentbox
```

Point the dashboard at the Go backend:

```bash
vercel env rm AGENTBOX_BACKEND_URL production --yes
printf 'https://youragentbox.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production
```

Deploy the dashboard:

```bash
vercel --prod --yes -A deploy/vercel/dashboard/vercel.json
```

## 8. Smoke test

```bash
curl https://youragentbox.vercel.app/api/health
agentbox doctor
agentbox mcp-url
```

Expected health response:

```json
{"ok":true,"service":"agentbox"}
```
