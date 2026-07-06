# Agentbox Multitenancy/Auth Implementation Plan

Date: 2026-07-07

## State of Current System

Agentbox currently behaves as one shared workspace behind a deployment-level admin key and global DB-backed actor API keys.

- Normal API and MCP auth is `?key=...` only. `internal/agentbox/httpapi/server.go` calls `service.AuthenticateAPIKey`, which returns `types.Actor{Name: key.Name, KeyName: key.Name}`.
- Admin auth is separate and deployment-wide. `internal/agentbox/auth/auth.go` accepts `AGENTBOX_ADMIN_KEY` through `x-agentbox-admin-key` or bearer auth.
- `threads`, `messages`, `assets`, `pending_uploads`, and `api_keys` have no `tenant_id`, `user_id`, membership, role, or scope fields. Repository methods in `internal/agentbox/db/repository.go` operate globally.
- API keys are plaintext in `api_keys.key_value`, keyed by global `name`. Creating a key with an existing name rotates the global key.
- Asset storage keys are `agentbox/{threadID}/{messageHint}/{uuid}-{filename}` through `internal/agentbox/assets/assets.go`, and signed download by asset id only checks that any actor key is valid.
- The dashboard stores `agentbox_admin_key` in localStorage for reading `/api/viewer/*`, and `app/components/agentbox-write.ts` creates a hidden global actor key named `user` for writes.
- CLI profiles in `internal/agentbox/profiles/profiles.go` store only `name`, `base_url`, and `api_key`. `internal/agentbox/cli/cli.go` appends the key as a query parameter to every authenticated route.
- `agentbox init`, `agentbox keys`, `agentbox mcp-url`, and `agentbox connect chatgpt` are all built around admin-key provisioning and query-string MCP URLs.
- Raycast stores only `baseUrl` and `apiKey`, appends `?key=...`, and constructs MCP URLs locally in `raycast/agentbox/src/api.ts`.
- Next.js API routes are thin proxies in `app/api/*`, with `app/api/_proxy/proxy.ts` forwarding headers, query strings, request bodies, and response bodies to the Go backend.

## State of Ideal System

One Agentbox deployment can host multiple isolated tenants while keeping setup simple for humans, CLI users, agents, MCP clients, and Raycast.

- Public signup is disabled. Tenant creation, first user creation, and initial key creation happen only through admin CLI/API/script flows protected by the deployment admin key or a trusted local migration command.
- Every authenticated request resolves to an auth context containing `tenant_id`, optional `user_id`, subject type, actor display name, scopes, key id or session id, and role.
- Threads, messages, assets, pending uploads, API keys, MCP access, and R2 object keys are tenant-isolated. A valid credential for tenant A cannot list, read, write, finalize uploads, sign downloads, list keys, rotate keys, or use MCP tools for tenant B.
- Browser sign-in is first-party and session-cookie based. Users sign in with an admin-provisioned email/username and password. There is no public self-service signup in the first implementation.
- CLI login is browser-assisted: `agentbox login` opens the dashboard, the user signs in, chooses a tenant if needed, and the CLI receives a tenant-scoped profile credential. Immediately after login, `agentbox doctor`, `agentbox list`, and `agentbox mcp-url` work without manual key setup.
- Existing API-key flows remain available, but API keys become tenant-scoped, hashed at rest, revocable, optionally scoped, and shown only once on creation.
- `agentbox mcp-url` prints a tenant-scoped remote MCP URL. Initially this can remain `/api/mcp?key=<tenant-scoped-key>` for compatibility, while the implementation also adds bearer/header auth internally where possible.
- `agentbox keys create --purpose raycast` or an equivalent command prints a Raycast-ready tenant key. After the user pastes that key into Raycast preferences, Raycast works through the same tenant-scoped API.
- Existing single-tenant deployments migrate into one default tenant with all current data, pending uploads, assets, and API keys attached to it.

## Recommended Architecture and Auth Model

Recommendation: implement first-party tenant/user auth in the Go backend and keep third-party identity providers out of the first release.

Rationale:

- The codebase currently has no auth provider abstraction, no server-side sessions, and no user/membership model. Introducing OAuth/OIDC now would add callback, provider, deployment, and account-linking complexity without solving the core tenant isolation work.
- Admin-provisioned users fit the product requirement to disable public signup.
- CLI and Raycast still need headless credentials. Tenant-scoped API keys remain the most pragmatic connector credential.

Core model:

- `types.AuthContext`: resolved on every protected HTTP/MCP request. Suggested fields: `TenantID`, `TenantSlug`, `UserID`, `SubjectType` (`user_session`, `api_key`, `admin`), `ActorName`, `KeyID`, `SessionID`, `Scopes`, `Role`.
- `types.Actor`: keep as display/audit identity for `created_by` and `author`, but do not use it for authorization.
- Browser sessions: signed opaque session cookie such as `agentbox_session`, backed by a `user_sessions` table. Store only a high-entropy session id in the cookie; hash session secrets in DB. Use `HttpOnly`, `Secure` in production, `SameSite=Lax`, and bounded expiration.
- Password auth: use a standard password hash such as Argon2id or bcrypt from a maintained Go package. Users are created by admin provisioning with an initial password or one-time password reset token.
- API keys: generate opaque secrets with a prefix, store `token_hash` and `token_prefix`, never plaintext. Return raw secret only once. Support `Authorization: Bearer <key>` and keep `?key=...` for compatibility with MCP/Raycast/current CLI.
- Admin key: keep `AGENTBOX_ADMIN_KEY` only for deployment-owner provisioning APIs and emergency break-glass. Do not let it list tenant data except through explicit provisioning/admin endpoints.

## Data Model and Migration Strategy

Add a new migration, for example `migrations/0005_multitenancy_auth.sql`, and mirror the final schema in `Repository.EnsureSchema`.

Recommended tables and columns:

- `tenants`
  - `id text primary key`
  - `slug text not null unique`
  - `name text not null`
  - `created_at timestamptz not null default now()`
  - `updated_at timestamptz not null default now()`
- `users`
  - `id text primary key`
  - `tenant_id text not null references tenants(id)`
  - `email text not null`
  - `display_name text not null`
  - `password_hash text`
  - `role text not null default 'member'`
  - `created_at`, `updated_at`, `disabled_at`
  - unique index on `(tenant_id, lower(email))`
- `user_sessions`
  - `id text primary key`
  - `tenant_id text not null references tenants(id)`
  - `user_id text not null references users(id)`
  - `secret_hash text not null unique`
  - `created_at`, `last_used_at`, `expires_at`, `revoked_at`
- `cli_login_codes`
  - `id text primary key`
  - `tenant_id text not null references tenants(id)`
  - `user_id text not null references users(id)`
  - `code_hash text not null unique`
  - `state_hash text not null`
  - `redirect_uri text not null`
  - `created_at`, `expires_at`, `consumed_at`
- Replace/upgrade `api_keys`
  - `id text primary key`
  - `tenant_id text not null references tenants(id)`
  - `user_id text references users(id)`
  - `name text not null`
  - `token_prefix text not null`
  - `token_hash text not null unique`
  - `scopes text[] not null default array['threads:read','threads:write','assets:read','assets:write','mcp:use']`
  - `created_at`, `updated_at`, `last_used_at`, `revoked_at`
  - unique active-name index scoped by tenant, for example `(tenant_id, lower(name)) where revoked_at is null`
- Add `tenant_id` to `threads`, `messages`, `assets`, and `pending_uploads`.
- Add `created_by_user_id` or `created_by_key_id` where useful, while preserving `created_by` and `author` as string display fields for API compatibility.

Migration path:

1. Create a default tenant, for example `ten_default` / slug `default`, if no tenants exist.
2. Add nullable `tenant_id` columns to existing tables.
3. Backfill every existing thread, message, asset, pending upload, and API key to the default tenant.
4. Make `tenant_id` not null after backfill.
5. Convert existing plaintext API keys to hashed API keys in place. During the migration, compute `token_hash` from existing `key_value`, set `token_prefix`, and keep `key_value` temporarily only if a compatibility flag is required. Drop or stop querying `key_value` once all code uses hashes.
6. Create a default admin user only when explicitly requested by the provisioning command, not automatically during normal request handling.
7. Do not rewrite existing R2 keys during migration. Existing objects can stay at `agentbox/{threadID}/...` because DB ownership checks protect them. New objects must use tenant-prefixed keys.

## Tenant Isolation Invariants

Implementation must preserve these invariants and tests should encode them directly:

- Every service method that reads or writes tenant-owned data requires `types.AuthContext` or an explicit `tenantID`.
- Repository methods must filter by `tenant_id`; HTTP handlers alone must not be the only isolation layer.
- `GetThread`, `ListThreads`, `SearchThreads`, `PostMessage`, `CreatePresignedUploads`, pending upload finalization, `GetAsset`, and signed download must all require matching tenant ownership.
- `messages.tenant_id`, `assets.tenant_id`, and `pending_uploads.tenant_id` must match their parent thread tenant. Enforce with application checks and indexes; add DB constraints where practical.
- API key names are unique only inside a tenant, not globally.
- API key secret lookup returns tenant context and fails for revoked keys.
- Browser sessions are tenant-bound. If a user can belong to multiple tenants later, every session request still resolves to one selected tenant.
- R2 key prefixes for new objects are `agentbox/{tenantID}/{threadID}/{messageHint}/{uuid}-{filename}` or `agentbox/{tenantSlug}/{threadID}/...` if slugs are immutable. Prefer `tenantID` to avoid slug rename risk.
- Signed R2 URLs are issued only after DB tenant ownership checks.
- Admin provisioning endpoints can create tenants and users, but normal app endpoints cannot create tenants or public users.

## Browser CLI Login Flow and Local Profile/Token Storage Behavior

Recommended first implementation:

1. `agentbox login --base-url <url> [--profile <name>]` starts a localhost callback server on `127.0.0.1` with a random state and code verifier.
2. The CLI opens `${baseURL}/login/cli?state=...&redirect_uri=http://127.0.0.1:<port>/callback`.
3. If the browser user has no session, the dashboard shows `/login` and authenticates with username/email plus password.
4. After sign-in, the dashboard shows tenant selection only if the session can access multiple tenants. In the first release, most users have one tenant.
5. The dashboard calls a backend endpoint such as `POST /api/auth/cli/authorize` using the session cookie. The backend creates a short-lived one-time code bound to tenant, user, state, redirect URI, and expiry.
6. The browser redirects to the CLI callback with `code` and `state`.
7. The CLI calls `POST /api/auth/cli/exchange` with the code and verifier. The backend creates or rotates a tenant-scoped API key named something like `cli-{hostname}` with appropriate scopes and returns the raw key once plus tenant metadata.
8. The CLI stores a profile in `profiles.json` with existing fields plus new optional metadata:
   - `base_url`
   - `api_key`
   - `tenant_id`
   - `tenant_slug`
   - `user_id`
   - `auth_type: "api_key"`
   - `key_name`
   - `created_by_login: true`
9. Existing command code can continue using `api_key` while `doctor` displays tenant/profile metadata.

Fallbacks:

- Keep `agentbox profiles add --api-key` for headless/manual setups.
- Keep `agentbox init` or replace it with `agentbox provision` for deployment-owner bootstrap.
- If browser opening fails, print the login URL and wait for the callback.

## User API Key / Remote MCP Key Creation and Rotation Behavior

API key creation should move from global admin-only endpoints to tenant-scoped user/admin endpoints plus deployment-owner provisioning endpoints.

Recommended endpoints:

- `GET /api/keys`: list masked keys for the current tenant. Requires user session or an API key with `keys:read`.
- `POST /api/keys`: create a tenant-scoped API key. Requires tenant admin role or `keys:write`. Accepts `name`, `scopes`, and optional `purpose`.
- `DELETE /api/keys/{id or name}`: revoke active key in the current tenant. Requires tenant admin role or ownership.
- `POST /api/keys/{id}/rotate`: revoke old key and issue a new secret under the same name, shown once.
- Keep `/api/admin/keys` during transition, but require a tenant selector and make it call the same tenant-scoped service path. Deprecate unscoped behavior.

MCP behavior:

- `agentbox mcp-url` should ensure the current profile has a key with `mcp:use` and relevant read/write scopes. If the profile key is suitable, print `/api/mcp?key=<key>`.
- Add `agentbox mcp-url --create-key [--name chatgpt]` or make `agentbox connect chatgpt` create a dedicated tenant-scoped key named `chatgpt` through the logged-in session/profile.
- For remote clients that support headers later, expose `agentbox mcp-url --no-secret` plus bearer-token setup guidance. For the first release, query-string keys are acceptable compatibility debt if all keys are tenant-scoped, revocable, and purpose-named.

Rotation:

- Rotating a CLI profile key should update local `profiles.json`.
- Rotating a ChatGPT/Raycast key should print the new secret once and clearly state that external clients must be updated.
- Revocation should immediately cause `AuthenticateAPIKey` and `/api/mcp` to fail.

## Raycast Setup Path After CLI Login

Recommended path:

1. User runs `agentbox login`.
2. User runs `agentbox keys create raycast --purpose raycast` or `agentbox raycast-key`.
3. CLI prints:
   - Dashboard/API base URL
   - Tenant slug/name
   - API key secret shown once
   - Raycast preference names to fill: `Agentbox URL`, `Agentbox API Key`
4. User pastes the base URL and key into Raycast preferences.
5. Raycast `Check Connection` continues to call health, authenticated `/api/threads?limit=1`, and MCP URL construction. It should display tenant details once the backend exposes `/api/me` or key-auth profile metadata.

Do not require Raycast browser login in the first implementation. Raycast is already designed around a stored API key and can remain so with tenant-scoped keys.

## Admin-Only Tenant/User Provisioning Flow

Replace the current "create global keys by admin key" bootstrap with an explicit provisioning command.

Recommended CLI/API:

- `agentbox provision tenant --base-url <url> --admin-key <deployment-admin-key> --tenant-slug <slug> --tenant-name <name> --user-email <email> --user-name <name> [--password <password>] [--create-cli-key] [--json]`
- Backend endpoint: `POST /api/admin/tenants`, protected by `AGENTBOX_ADMIN_KEY`.
- Endpoint behavior:
  - Create tenant if it does not exist.
  - Create initial admin user in that tenant.
  - Set password hash from provided password or return a one-time setup/reset token.
  - Create an initial API key if requested, scoped to that tenant.
  - Return tenant metadata and the raw initial key/setup token once.

Migration support:

- `agentbox provision default-admin` can attach an initial admin user to the migrated default tenant.
- `cmd/migrate` should remain schema-only unless explicitly passed provisioning arguments. Avoid creating human users implicitly in production.

## Backwards Compatibility Strategy for Existing Single-Tenant Data and API Keys

Compatibility should be deliberate and temporary:

- Existing single-tenant data migrates into the default tenant.
- Existing API keys continue to authenticate after migration by hashing their current plaintext `key_value` into `token_hash`.
- Old CLI profiles with only `base_url` and `api_key` continue to resolve. The first successful `doctor` can display tenant metadata if `/api/me` is added, but should not require profile rewrite.
- Existing MCP URLs using `?key=` continue to work if the key is still active.
- Existing Raycast preferences continue to work after key migration.
- Existing dashboard localStorage admin-key workflow should be replaced, not preserved indefinitely. During transition, `/api/admin/keys` can still exist for deployment owner operations, but the main dashboard should move to `/login` and session-backed `/api/viewer/*` or ordinary `/api/threads`.
- Keep response shapes for threads, messages, assets, search results, and MCP tools stable. Add fields like `tenant_id` only if useful and not harmful to clients.
- Keep `created_by` and `author` labels stable. New fields such as `created_by_user_id` should not break existing clients.

## Security Risks and Operational Rollout Notes

Security risks:

- Missing a tenant filter in one repository method would leak data. Mitigate by requiring tenant-aware repository signatures and tests that create same-id-like or cross-tenant fixtures.
- Query-string credentials can leak through logs and browser history. Mitigate by making keys scoped, purpose-specific, revocable, hashed at rest, and adding bearer support. Plan a later MCP auth upgrade when client support allows.
- Session cookies require CSRF protection for state-changing browser routes. Use `SameSite=Lax`, reject unexpected origins for unsafe methods, and add CSRF tokens if cross-site flows require it.
- Password handling must use a real password hash and rate-limited login attempts. Do not store plaintext setup passwords.
- Deployment admin key remains powerful. Keep it out of browser localStorage and use it only for provisioning APIs.
- Direct R2 uploads must not trust client-provided storage keys. The backend must generate tenant-prefixed keys and validate pending upload tenant/user ownership on finalization.

Operational rollout:

- Ship behind additive migrations first, then tenant-aware code.
- Run migration in staging and verify old key auth, old MCP URL auth, CLI doctor, Raycast doctor, and attachment download.
- After deployment, run `agentbox provision default-admin` to create the first human user for the default tenant.
- Communicate that old dashboard admin-key login is replaced by browser sign-in.
- Add logging for auth subject type, tenant id, key id/session id, route, and request id. Do not log secrets or raw query strings.

## Explicit Options/Trade-Offs Considered

Option A: first-party users, sessions, and tenant-scoped API keys.

- Pros: fits disabled signup, simplest deployment, no third-party dependency, easy browser sign-in, preserves CLI/Raycast/API-key flows.
- Cons: the project owns password/session security and future account recovery.
- Recommendation: choose this for the first release.

Option B: external OAuth/OIDC provider.

- Pros: less password handling, enterprise-friendly later.
- Cons: new provider config, callback domain issues across dashboard/backend split, account-linking work, still needs API keys for Raycast/MCP/agents.
- Recommendation: defer until the internal tenant/auth model is stable.

Option C: tenant id in every URL path, for example `/api/tenants/{tenant}/threads`.

- Pros: explicit tenant targeting, useful for multi-tenant users.
- Cons: breaks many clients and requires every command/client to supply tenant path.
- Recommendation: defer path-based tenant addressing. Resolve tenant from auth context first; add tenant selection metadata to login/profile.

Option D: keep plaintext API keys.

- Pros: lowest migration effort.
- Cons: poor security posture, bad fit for broader hosted multi-tenant use.
- Recommendation: do not keep plaintext keys. Hash tokens during multitenancy migration.

Option E: immediately replace MCP query-key URLs with OAuth or header-only auth.

- Pros: avoids URL secret leakage.
- Cons: high compatibility risk with current CLI, Raycast, ChatGPT setup instructions, and MCP clients.
- Recommendation: keep query-key MCP URLs initially, but make them tenant-scoped and revocable, and add bearer support internally.

## Plan Phases

### Phase 1: Tenant/Auth Schema and Types

#### Files to read before starting

- `migrations/0001_init.sql`
- `migrations/0003_api_keys.sql`
- `migrations/0004_pending_uploads.sql`
- `internal/agentbox/db/repository.go`
- `internal/agentbox/db/memory.go`
- `internal/agentbox/types/types.go`
- `internal/agentbox/service/service.go`
- `internal/agentbox/config/config.go`
- `cmd/migrate/main.go`

#### What to do

- Add `migrations/0005_multitenancy_auth.sql` with tenants, users, sessions, CLI login codes, upgraded API keys, and `tenant_id` columns on tenant-owned tables.
- Backfill a deterministic default tenant for existing data.
- Add indexes for all tenant-scoped access paths:
  - `threads(tenant_id, updated_at desc)`
  - `messages(tenant_id, thread_id, created_at asc)`
  - `assets(tenant_id, message_id)`
  - `assets(tenant_id, id)`
  - `pending_uploads(tenant_id, thread_id, created_at desc)`
  - `api_keys(token_hash)`, `api_keys(tenant_id, lower(name)) where revoked_at is null`
- Update `Repository.EnsureSchema` to match migrations.
- Add `types.AuthContext`, tenant/user/session/API-key structs, and tenant-aware fields on internal types.
- Update `MemoryRepository` to carry tenant ids for tests.
- Add config fields for session cookie name/secret or token hash pepper if needed, app public URL, and auth cookie secure behavior. Prefer secure defaults in production.

#### Validation strategy

- Run migration command against an empty local DB and a copy of a single-tenant DB.
- Add repository tests for default tenant backfill assumptions if there are existing migration test helpers.
- Run `go test ./internal/agentbox/db ./internal/agentbox/config`.
- Verify `EnsureSchema` on an empty DB produces the same logical schema as migrations.

#### Risks / fallbacks

- Risk: converting `api_keys` primary key from `name` to `id` can be awkward in-place. Fallback: create `api_keys_new`, copy rows, drop/rename in one migration transaction where Postgres allows it.
- Risk: hashing existing keys requires application code or SQL digest support. Fallback: add `token_hash` nullable, deploy code that lazily upgrades plaintext keys on successful auth, then run a cleanup migration later.
- Risk: lazy `EnsureSchema` can hide migration drift. Fallback: reduce `EnsureSchema` to additive safety only after the migration lands, and make production rely on `cmd/migrate`.

### Phase 2: Auth Context, API Key Hashing, and Tenant-Scoped Service Boundaries

#### Files to read before starting

- `internal/agentbox/auth/auth.go`
- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/service/service.go`
- `internal/agentbox/db/repository.go`
- `internal/agentbox/db/memory.go`
- `internal/agentbox/types/types.go`
- `internal/agentbox/httpapi/server_test.go`
- `internal/agentbox/service/service_test.go`

#### What to do

- Replace `AuthenticateAPIKey(ctx, secret) -> *types.Actor` with a method returning `*types.AuthContext`.
- Support both `Authorization: Bearer <key>` and `?key=<key>` for normal API/MCP requests.
- Generate API keys as opaque secrets with a stable prefix and enough entropy. Store only hash and prefix.
- Change service signatures so tenant-owned operations receive auth context:
  - `ListThreads(ctx, auth, limit)`
  - `SearchThreads(ctx, auth, params)`
  - `CreateThread(ctx, auth, title)`
  - `CreateThreadWithMessage(ctx, auth, ...)`
  - `GetThread(ctx, auth, threadID)`
  - `PostMessage(ctx, auth, params)`
  - `CreatePresignedUploads(ctx, auth, threadID, files)`
  - `GetAsset(ctx, auth, assetID)`
  - key management methods receive auth/admin context and tenant id.
- Update repository signatures to require tenant id and filter every query by `tenant_id`.
- Preserve `ActorName` as the value written to `threads.created_by`, `messages.author`, `assets.created_by`, and `pending_uploads.created_by`.
- Mark key `last_used_at` asynchronously or inline after successful auth.
- Add `PERMISSION_DENIED` or `THREAD_NOT_FOUND` behavior for cross-tenant access. Prefer not-found for object reads where exposing existence is unnecessary.

#### Validation strategy

- Add HTTP and service tests with two tenants and two keys:
  - Tenant A key cannot list tenant B threads.
  - Tenant A key cannot get tenant B thread by id.
  - Tenant A key cannot post to tenant B thread.
  - Tenant A key cannot sign tenant B asset download URL.
  - Same API key name can exist in both tenants.
  - Revoking one tenant key does not affect another tenant key with the same name.
- Run `go test ./internal/agentbox/httpapi ./internal/agentbox/service ./internal/agentbox/db`.

#### Risks / fallbacks

- Risk: wide signature churn touches most Go packages. Fallback: introduce new tenant-aware methods in parallel, migrate callers package by package, then delete old methods.
- Risk: tests using `types.Actor` will break broadly. Fallback: helper constructors such as `testAuth(tenantID, actorName)` keep test churn manageable.
- Risk: query-string key compatibility can accidentally remain the only path. Fallback: add explicit tests for bearer auth and query auth.

### Phase 3: Tenant-Isolated Assets and Uploads

#### Files to read before starting

- `internal/agentbox/assets/assets.go`
- `internal/agentbox/assets/assets_test.go`
- `internal/agentbox/service/service.go`
- `internal/agentbox/db/repository.go`
- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/httpapi/server_test.go`

#### What to do

- Add tenant id to asset store upload parameters:
  - `UploadBytesParams.TenantID`
  - `PresignedUploadParams.TenantID`
  - `UploadChatGPTFile(ctx, auth or tenantID, threadID, input)`
- Change `MakeStorageKey` to include tenant id for new objects: `agentbox/{tenantID}/{threadID}/{messageHint}/{uuid}-{filename}`.
- Keep signing existing storage keys exactly as stored in DB. Do not infer tenant from old R2 keys.
- Add `tenant_id` to `pending_uploads` and enforce pending upload lookup by tenant id, thread id, upload ids, and owner identity.
- Ensure direct upload intent creation verifies the thread belongs to the tenant before issuing R2 PUT URLs.
- Ensure finalization verifies every upload belongs to the same tenant/thread and was created by the current subject/actor.
- Ensure `GetAsset` joins or filters on `assets.tenant_id` before signing.

#### Validation strategy

- Update `TestFilenameMimeStorageAndPublicURLHelpers` to assert tenant-prefixed storage keys.
- Add cross-tenant upload tests:
  - Tenant A cannot create upload intent for tenant B thread.
  - Tenant A cannot finalize tenant B upload id.
  - Tenant A cannot sign tenant B asset id.
- Run `go test ./internal/agentbox/assets ./internal/agentbox/httpapi ./internal/agentbox/service`.

#### Risks / fallbacks

- Risk: old assets have non-tenant-prefixed R2 keys. Fallback: treat DB tenant ownership as authoritative and only tenant-prefix new keys.
- Risk: R2 public base URLs may expose object paths. Fallback: continue using signed URLs for private access and avoid relying on public URLs for authorization.

### Phase 4: First-Party Browser Sign-In and Session-Backed Dashboard

#### Files to read before starting

- `internal/agentbox/auth/auth.go`
- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/service/service.go`
- `internal/agentbox/db/repository.go`
- `app/api/_proxy/proxy.ts`
- `app/threads/inbox-view.tsx`
- `app/threads/[threadId]/thread-view.tsx`
- `app/keys/keys-view.tsx`
- `app/components/agentbox-write.ts`
- `app/components/viewer-profiles.ts`

#### What to do

- Add backend auth endpoints:
  - `POST /api/auth/login`
  - `POST /api/auth/logout`
  - `GET /api/auth/me`
  - optional `POST /api/auth/change-password`
- Implement password verification, session creation, cookie setting, session lookup, and logout revocation.
- Replace dashboard admin-key localStorage sign-in with `/login`.
- Make dashboard thread list/detail call authenticated session-backed endpoints. Either:
  - keep `/api/viewer/threads` but change it to accept user session auth, or
  - use normal `/api/threads` with cookie auth after `requireAuth` supports sessions.
- Remove hidden global key creation from `app/components/agentbox-write.ts`. Dashboard writes should use the user session auth context directly, with `ActorName` from the user display name.
- Replace `/keys` admin-key UI with tenant-scoped key management for signed-in tenant admins.
- Ensure `app/api/_proxy/proxy.ts` forwards cookies and set-cookie headers correctly. It already forwards headers; verify Next/Vercel does not strip `Set-Cookie`.
- Add an unauthenticated state that routes users to `/login` and an authenticated nav with sign out.

#### Validation strategy

- Add backend tests for login success, login failure, session cookie, logout, and `/api/auth/me`.
- Add HTTP tests proving cookie session can list/create/get/post within a tenant and cannot cross tenants.
- Run `bun run typecheck` for dashboard changes.
- Run `go test ./internal/agentbox/httpapi ./internal/agentbox/auth ./internal/agentbox/service`.
- Manually verify in a browser locally: login, list threads, open thread, create thread, post message with attachment, create key, logout.

#### Risks / fallbacks

- Risk: dashboard and backend are deployed as separate Vercel services. Fallback: keep browser API calls through the dashboard proxy so cookies are scoped to the dashboard origin, and let the proxy forward them to the backend.
- Risk: CSRF on cookie-auth state changes. Fallback: require same-origin checks for unsafe methods immediately; add CSRF token if same-origin is insufficient.
- Risk: user password reset is out of scope. Fallback: admin provisioning can set or rotate an initial password until a reset flow is added.

### Phase 5: Admin Provisioning and Deployment Bootstrap

#### Files to read before starting

- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/auth/auth.go`
- `internal/agentbox/service/service.go`
- `internal/agentbox/db/repository.go`
- `internal/agentbox/cli/bootstrap.go`
- `internal/agentbox/cli/cli.go`
- `cmd/migrate/main.go`
- `cmd/api/main.go`

#### What to do

- Add deployment-owner admin endpoints protected by `AGENTBOX_ADMIN_KEY`:
  - `POST /api/admin/tenants`
  - `POST /api/admin/tenants/{tenantID}/users`
  - optional `POST /api/admin/tenants/{tenantID}/keys`
- Implement idempotent provisioning service methods for tenant, initial admin user, password/setup token, and initial API key.
- Add CLI command `agentbox provision tenant ...` or evolve `agentbox init` carefully:
  - Prefer `provision` for tenant/user setup.
  - Keep `init` as a compatibility wrapper or mark it legacy.
- Update deployment guide output in `runDeployVercel` to run migrations and then provision a tenant/admin user.
- Ensure public signup does not exist. Do not add a public create-user endpoint.

#### Validation strategy

- Add CLI tests for `provision tenant --json` with a test server.
- Add HTTP tests for admin provisioning authorization and idempotency.
- Verify an untrusted session or API key cannot call provisioning endpoints.
- Run `go test ./internal/agentbox/cli ./internal/agentbox/httpapi ./internal/agentbox/service`.

#### Risks / fallbacks

- Risk: `agentbox init` users expect key creation. Fallback: keep `agentbox init` for old global/single-tenant deployments but have it require/select a tenant once multitenancy is enabled.
- Risk: printing passwords/secrets can leak in shell history/logs. Fallback: prefer one-time setup tokens or prompt for passwords through stdin where possible.

### Phase 6: Browser-Based CLI Login and Profile Migration

#### Files to read before starting

- `internal/agentbox/cli/cli.go`
- `internal/agentbox/cli/bootstrap.go`
- `internal/agentbox/profiles/profiles.go`
- `internal/agentbox/profiles/profiles_test.go`
- `internal/agentbox/cli/cli_test.go`
- `internal/agentbox/httpapi/server.go`
- `app` login-related files added in Phase 4

#### What to do

- Add `agentbox login` command.
- Implement localhost callback flow with random state and short timeout.
- Add backend CLI auth endpoints:
  - `POST /api/auth/cli/authorize` from browser session.
  - `POST /api/auth/cli/exchange` from CLI.
- Add dashboard `/login/cli` page that signs the user in if needed, then authorizes CLI access.
- On exchange, create or rotate a tenant-scoped API key for the CLI profile and return tenant/user metadata.
- Extend `profiles.Profile` with optional metadata while continuing to parse old profile shapes:
  - `tenant_id`
  - `tenant_slug`
  - `tenant_name`
  - `user_id`
  - `key_name`
  - `auth_type`
- Keep `api_key` as the command credential so existing request code remains simple.
- Update `doctor` to display tenant metadata and prove:
  - profile resolves,
  - health works,
  - authenticated API works,
  - signed download URL works when assets exist,
  - MCP URL works.

#### Validation strategy

- Add profile parse/write tests proving old JSON still reads and new JSON writes with optional metadata.
- Add CLI tests using a fake browser opener or printed URL mode for login exchange.
- Add HTTP tests for CLI code expiry, state mismatch, consumed-code replay, and tenant binding.
- Run `go test ./internal/agentbox/cli ./internal/agentbox/profiles ./internal/agentbox/httpapi`.
- Manual smoke: `agentbox login`, `agentbox doctor`, `agentbox list`, `agentbox mcp-url`.

#### Risks / fallbacks

- Risk: opening browsers from CLI is platform-specific. Fallback: print the URL and allow `--no-open`.
- Risk: localhost callback can be blocked. Fallback: support manual copy/paste of the one-time code.
- Risk: profile metadata could break existing env profile parsing. Fallback: make all new fields optional and ignore unknown fields in parse logic.

### Phase 7: Tenant-Scoped MCP, ChatGPT, and Raycast Key Paths

#### Files to read before starting

- `internal/agentbox/httpapi/server.go`
- `internal/agentbox/mcpserver/mcpserver.go`
- `internal/agentbox/mcpserver/mcpserver_test.go`
- `internal/agentbox/cli/bootstrap.go`
- `internal/agentbox/cli/cli.go`
- `raycast/agentbox/src/api.ts`
- `raycast/agentbox/src/doctor.tsx`
- `raycast/agentbox/src/copy-mcp-url.ts`
- `raycast/agentbox/package.json`

#### What to do

- Make `/api/mcp` authenticate into `types.AuthContext`, not just `types.Actor`.
- Change `mcpserver.Server` to hold auth context and pass it to service methods.
- Keep MCP tool names and response shapes stable.
- Update `agentbox connect chatgpt` to create or reuse a tenant-scoped `chatgpt` key with `mcp:use` scope, then print the MCP URL.
- Update `agentbox mcp-url` to report tenant metadata in JSON and sanitize secrets in diagnostics.
- Add `agentbox keys create raycast` or `agentbox raycast-key` path that creates a `raycast` key and prints Raycast preferences.
- Update Raycast only as much as needed:
  - Keep `baseUrl` and `apiKey` preferences.
  - Allow keys to be tenant-scoped transparently.
  - Add tenant/account display to doctor if `/api/auth/me` or `/api/me` supports API-key auth.
  - Keep `copy-mcp-url` behavior unless MCP URL shape changes.

#### Validation strategy

- Add MCP tests proving tenant A MCP cannot see tenant B threads.
- Add CLI tests for `connect chatgpt` creating or printing tenant-scoped MCP URL.
- Build/lint Raycast if the repo scripts are available, otherwise run TypeScript check for the Raycast package.
- Manual smoke:
  - `agentbox mcp-url`
  - `agentbox connect chatgpt`
  - create Raycast key, paste into Raycast, run Raycast doctor.

#### Risks / fallbacks

- Risk: automatic key creation from `mcp-url` might surprise users. Fallback: make creation explicit in `connect chatgpt` and keep `mcp-url` as a pure print command.
- Risk: ChatGPT remote MCP may only support URL-based no-auth setup for this workflow. Fallback: keep `/api/mcp?key=` while documenting revocation and rotation.

### Phase 8: Compatibility Cleanup, End-to-End Tests, and Rollout

#### Files to read before starting

- `internal/agentbox/httpapi/server_test.go`
- `internal/agentbox/service/service_test.go`
- `internal/agentbox/cli/cli_test.go`
- `internal/agentbox/mcpserver/mcpserver_test.go`
- `internal/agentbox/assets/assets_test.go`
- `internal/agentbox/profiles/profiles_test.go`
- `app/keys/keys-view.tsx`
- `app/threads/inbox-view.tsx`
- `app/threads/[threadId]/thread-view.tsx`
- `raycast/agentbox/src/doctor.tsx`
- `deploy/vercel/backend/vercel.json`
- `deploy/vercel/dashboard/vercel.json`

#### What to do

- Remove or deprecate unscoped admin key UI paths.
- Remove dashboard hidden `agentbox_actor_key` behavior.
- Add a `/api/me` or `/api/auth/me` response that works for sessions and API keys and returns tenant/user/key metadata without leaking secrets.
- Add structured auth logging without raw secrets or raw query strings.
- Update command help text for `login`, `provision`, tenant-aware `keys`, `mcp-url`, and `connect`.
- Update deployment env guidance for new auth/session settings.
- Add an end-to-end test matrix covering:
  - migrated old key,
  - browser session,
  - CLI login-created key,
  - Raycast-style key,
  - ChatGPT/MCP key,
  - cross-tenant denial for data and assets.

#### Validation strategy

- Run the broad gate:
  - `bun run test:parity`
  - `bun run typecheck`
  - `bun run lint`
  - `go test ./...`
  - `go vet ./...`
  - `bun run build:api`
  - `bun run build:cli`
- Add manual smoke on a staging deployment:
  - provision tenant and admin user,
  - browser login,
  - CLI login,
  - CLI doctor/list/mcp-url,
  - Raycast key setup and doctor,
  - ChatGPT MCP URL setup,
  - attachment upload/download.

#### Risks / fallbacks

- Risk: broad changes break deployment. Fallback: ship schema and backend tenant scoping first, then dashboard login, then CLI login/Raycast polish in separate deploys behind compatible key auth.
- Risk: old localStorage admin keys create confusing browser state. Fallback: force sign-out migration by ignoring/removing `agentbox_admin_key` and showing the new login page.
- Risk: broad gate is slow. Fallback: run targeted Go tests during phases and the full gate only before merge/deploy.

