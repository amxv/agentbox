# Agentbox First-Time Setup

This guide sets up a self-hosted Agentbox instance with the Go backend, Postgres, Cloudflare R2, the npm CLI, an optional Next.js dashboard, and ChatGPT MCP.

## 1. Install the CLI

The npm CLI is the user-facing tool you keep using after deployment.

```bash
npm install -g @amxv/agentbox
agentbox --version
```

## 2. Prepare Postgres and R2

Agentbox stores threads, messages, API keys, and asset metadata in Postgres. Uploaded files and generated assets live in Cloudflare R2.

Required backend values:

```bash
DATABASE_URL="postgres://USER:PASSWORD@HOST:PORT/DATABASE?sslmode=require"
R2_ACCOUNT_ID="your-cloudflare-account-id"
R2_ACCESS_KEY_ID="your-r2-access-key-id"
R2_SECRET_ACCESS_KEY="your-r2-secret-access-key"
R2_BUCKET="your-agentbox-bucket-name"
```

Optional:

```bash
R2_PUBLIC_BASE_URL="https://assets.example.com"
AGENTBOX_ALLOWED_ORIGINS="https://chatgpt.com"
AGENTBOX_MAX_FILE_SIZE_BYTES="26214400"
AGENTBOX_DB_POOL_SIZE="4"
```

## 3. Create the admin key

Agentbox uses one admin key for setup, read-only viewer access, and API key management.

```bash
openssl rand -hex 32
AGENTBOX_ADMIN_KEY="paste-the-generated-value"
```

Normal API and MCP requests do not use the admin key. They use named API keys stored in Postgres.

## 4. Deploy the required Go backend

The Go backend owns `/api/*`, `/api/mcp`, Postgres, R2, migrations, admin key management, and the API used by the CLI.

```bash
vercel link --yes --project agentbox-go
vercel env add DATABASE_URL production
vercel env add AGENTBOX_ADMIN_KEY production
vercel env add R2_ACCOUNT_ID production
vercel env add R2_ACCESS_KEY_ID production
vercel env add R2_SECRET_ACCESS_KEY production
vercel env add R2_BUCKET production
vercel env add AGENTBOX_ENV production
vercel --prod --yes -A deploy/vercel/backend/vercel.json
```

Add optional backend environment variables only when needed.

## 5. Run migrations

Run migrations once with the backend production environment available:

```bash
bun run db:migrate
```

This creates the thread, message, asset, and API key tables, including `api_keys`.

## 6. Initialize local and ChatGPT keys

After the backend is live and migrated, create two DB-backed API keys through the admin API:

```bash
agentbox init \
  --profile-name prod \
  --base-url https://your-agentbox-go.vercel.app \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --local-key-name local \
  --chatgpt-key-name chatgpt
```

`agentbox init` creates a `local` key and a `chatgpt` key in Postgres, saves the local key in your CLI profile, and prints the ChatGPT key and MCP URL once. Store the ChatGPT secret immediately.

Verify the local profile:

```bash
agentbox doctor
agentbox list
```

Manage keys later with:

```bash
agentbox keys list --admin-key "$AGENTBOX_ADMIN_KEY"
agentbox keys create worker --admin-key "$AGENTBOX_ADMIN_KEY"
agentbox keys revoke worker --admin-key "$AGENTBOX_ADMIN_KEY"
```

If the CLI cannot resolve a backend URL from the active profile, pass `--base-url https://your-agentbox-go.vercel.app`.

## 7. Connect ChatGPT

Use the MCP URL printed by `agentbox init`, or print one for the active local profile:

```bash
agentbox connect chatgpt
```

In ChatGPT:

```text
Apps -> Advanced settings -> turn on developer mode -> Create app -> select no auth -> paste the MCP URL
```

Agentbox uses no ChatGPT app auth because the API key is embedded in the MCP URL.

## 8. Deploy the optional dashboard

The Next.js dashboard is optional. It owns `/`, `/threads`, and same-origin proxy routes to the Go backend.

```bash
vercel link --yes --project agentbox
vercel env rm AGENTBOX_BACKEND_URL production --yes
printf 'https://your-agentbox-go.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production
vercel --prod --yes -A deploy/vercel/dashboard/vercel.json
```

Open:

```text
https://your-agentbox.vercel.app/threads
```

Enter `AGENTBOX_ADMIN_KEY` in the viewer dialog. The browser stores it locally and sends it as `x-agentbox-admin-key` to viewer/admin API routes.

## 9. REST smoke test

Create a thread using the local profile key:

```bash
agentbox create "First Agentbox thread"
agentbox list
```

Or call the backend directly with a DB-backed API key:

```bash
curl -X POST "https://your-agentbox-go.vercel.app/api/threads?key=LOCAL_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"First Agentbox thread"}'
```

## 10. Local development

Run the backend/dashboard locally:

```bash
bun run dev
```

For local API calls, create an API key through the admin API and use it in the `key` query parameter or CLI profile. There is no unauthenticated local API fallback.

Useful checks:

```bash
bun run lint
bun run typecheck
bun run build:cli
bun run build
```

## Troubleshooting

### `Unauthorized`

For normal API or MCP requests, confirm the URL includes a DB-backed API key:

```text
https://your-agentbox-go.vercel.app/api/mcp?key=your-api-key
```

For admin routes and the viewer, confirm `AGENTBOX_ADMIN_KEY` is set on the backend and the request sends `x-agentbox-admin-key` or bearer auth.

### `DATABASE_URL is required`

Set `DATABASE_URL` on the backend service and redeploy.

### CLI cannot connect

Check the resolved profile and stored key:

```bash
agentbox profiles show
agentbox doctor
```

### Asset upload fails

Confirm the R2 environment values are present and the credentials can write to the configured bucket.
