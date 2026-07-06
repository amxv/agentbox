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

## 3. Create the deployment admin key

Agentbox uses one deployment admin key for tenant/user bootstrap and emergency provisioning APIs. Do not put this key in the browser dashboard or external connector clients.

```bash
openssl rand -hex 32
AGENTBOX_ADMIN_KEY="paste-the-generated-value"
```

Normal dashboard, API, MCP, Raycast, and CLI requests do not use the deployment admin key. They use a browser session cookie or tenant-scoped API keys stored hashed in Postgres.

## 4. Deploy the required Go backend

The Go backend owns `/api/*`, `/api/mcp`, Postgres, R2, migrations, tenant provisioning, session auth, tenant-scoped key management, and the API used by the CLI.

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

This creates the tenant, user, session, thread, message, asset, pending upload, and API key tables. Existing single-tenant data and plaintext keys are migrated into the default tenant and hashed where possible.

## 6. Provision the first tenant admin

After the backend is live and migrated, create the first tenant and admin user through the deployment-owner admin API:

```bash
agentbox provision tenant \
  --base-url https://youragentbox.vercel.app \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --tenant-slug default \
  --tenant-name Default \
  --user-email you@example.com \
  --user-name "Your Name" \
  --password "$AGENTBOX_INITIAL_PASSWORD" \
  --create-cli-key \
  --key-name local \
  --profile-name prod
```

`agentbox provision tenant` creates or updates the tenant and first tenant admin. With `--create-cli-key`, it also creates a tenant-scoped `local` API key, saves it in your CLI profile, and prints the secret once.

Verify the local profile:

```bash
agentbox doctor
agentbox list
```

Manage keys later with:

```bash
agentbox login --base-url https://youragentbox.vercel.app --profile-name prod
agentbox keys list
agentbox keys create worker
agentbox keys revoke worker
```

If the CLI cannot resolve a backend URL from the active profile, pass `--base-url https://youragentbox.vercel.app`.

## 7. Connect ChatGPT

Create a dedicated ChatGPT key and print the tenant-scoped MCP URL:

```bash
agentbox connect chatgpt
```

In ChatGPT:

```text
Apps -> Advanced settings -> turn on developer mode -> Create app -> select no auth -> paste the MCP URL
```

Agentbox uses no ChatGPT app auth because the tenant-scoped API key is embedded in the MCP URL. Revoke or rotate the `chatgpt` key if the URL is exposed.

## 8. Deploy the optional dashboard

The Next.js dashboard is optional. It owns `/`, `/threads`, and same-origin proxy routes to the Go backend.

```bash
vercel link --yes --project agentbox
vercel env rm AGENTBOX_BACKEND_URL production --yes
printf 'https://youragentbox.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production
vercel --prod --yes -A deploy/vercel/dashboard/vercel.json
```

Open:

```text
https://your-agentbox.vercel.app/threads
```

Sign in at `/login` with the tenant admin email and password from provisioning. The dashboard uses an HTTP-only session cookie and tenant-scoped API routes; it does not store `AGENTBOX_ADMIN_KEY` in browser storage.

## 9. REST smoke test

Create a thread using the local profile key:

```bash
agentbox create "First Agentbox thread"
agentbox list
```

Or call the backend directly with a DB-backed API key:

```bash
curl -X POST "https://youragentbox.vercel.app/api/threads?key=LOCAL_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"First Agentbox thread"}'
```

## 10. Local development

Run the backend/dashboard locally:

```bash
bun run dev
```

For local API calls, create a tenant-scoped API key through the dashboard or `agentbox keys create` and use it in the `key` query parameter or CLI profile. There is no unauthenticated local API fallback.

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
https://youragentbox.vercel.app/api/mcp?key=your-api-key
```

For provisioning routes, confirm `AGENTBOX_ADMIN_KEY` is set on the backend and the request sends `x-agentbox-admin-key` or bearer auth. For the dashboard, sign in through `/login`; for CLI, Raycast, MCP, and direct API calls, use a tenant-scoped API key.

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
