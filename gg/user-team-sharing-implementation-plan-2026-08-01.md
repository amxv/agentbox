# AgentBox User, Team, and Public Sharing Implementation Plan

## Planning Basis

- **Written against:** branch `feat/user-team-sharing`, commit `fbb8f2c` (`docs: define user team sharing specification`).
- **Date:** 2026-08-01.
- **Primary specification:** `docs/user-team-sharing-spec.md` (read in full). Every phase below must preserve its terminology and acceptance criteria.
- **Planning format consumed:** the user-supplied `gg-plan-doc.md` and `blueprint-writer.md` artifacts. The discussion-first portion of the blueprint workflow was explicitly waived because the product decisions were resolved in the preceding conversation.
- **Code inspected:** `cmd/api`, `cmd/migrate`, `internal/agentbox/{types,db,service,httpapi,mcpserver,assets,auth,config,profiles,cli}`, the Next.js routes and dashboard under `app/`, the SQL files under `migrations/`, and the existing Go tests.
- **Verified baseline:** `go test ./...` passed on the cloned repository before these documentation-only commits.
- **Not verified from production:** actual PostgreSQL row counts, actual R2 object counts and missing-object state, provider-specific backup facilities, current production environment variables, and whether anything outside this repository calls the legacy admin/tenant endpoints. The rollout phases treat these as pre-cutover facts that must be measured rather than assumed.
- **Execution model:** Phases 1-14 are the shared Zodex implementation track. Agents working across ChatGPT sessions on `feat/user-team-sharing` must complete and push all code, migrations, tests, UI, CLI, MCP, documentation, credential-free tooling, and production runbooks. Phase 15 is reserved exclusively for a credentialed local agent to perform the real PostgreSQL/R2 backup, Vercel deployment, live migration, production credential setup, and final production verification.

## State of Current System

### Runtime and deployment

AgentBox is split into two deployable services:

- `cmd/api/main.go` builds the Go backend that owns `/api/*`, `/api/mcp`, PostgreSQL, R2, and the service/repository layers.
- The Next.js application under `app/` owns pages and proxies same-origin API calls through `app/api/_proxy/proxy.ts` to `AGENTBOX_BACKEND_URL`.
- `cmd/agentbox` builds the Go CLI, and `npm/agentbox` packages platform binaries behind `@amxv/agentbox`.

The split is useful and should remain. The authorization boundary is not: the current backend treats a tenant as both an account namespace and the complete thread-visibility boundary.

### Schema management is duplicated and runs on hot paths

There are SQL files `migrations/0001_init.sql` through `migrations/0005_multitenancy_auth.sql`, but `cmd/migrate/main.go` does not execute them. It opens the repository and calls:

```go
if err := repo.EnsureSchema(ctx); err != nil {
    log.Fatal(err)
}
```

`internal/agentbox/db/repository.go` embeds the entire schema again inside `Repository.EnsureSchema`. Every normal repository method begins by calling `EnsureSchema`, including `ListThreads`, `SearchThreads`, `GetThread`, `PostMessage`, and key/session operations. This means normal requests repeatedly execute a large `CREATE TABLE IF NOT EXISTS` / `ALTER TABLE` batch. It also means the checked-in SQL files are not the authoritative migration history.

This must be corrected before the new authorization schema becomes load-bearing. A security migration cannot safely depend on two DDL definitions that may drift, and schema work must not remain on read/write hot paths.

### Authentication and identity are tenant-shaped

`internal/agentbox/types/types.go` defines this current authentication context:

```go
type AuthContext struct {
    TenantID    string
    TenantSlug  string
    UserID      string
    SubjectType AuthSubjectType
    ActorName   string
    KeyID       string
    SessionID   string
    Scopes      []string
    Role        string
}
```

Users, sessions, CLI login codes, threads, messages, assets, pending uploads, and API keys all carry `tenant_id`. `users` are unique by `(tenant_id, lower(email))`, so the same email can identify several user rows. `api_keys` are unique by `(tenant_id, lower(name))`, so two users in one tenant cannot independently own a `chatgpt` credential without rotating the same record.

`Service.Login` accepts an optional tenant ID, and `app/login/login-view.tsx` exposes a Tenant ID input. CLI browser login returns tenant metadata and stores it in `profiles.Profile` as `TenantID`, `TenantSlug`, and `TenantName`.

There are two administrative mechanisms:

- `AGENTBOX_ADMIN_KEY`, validated by `auth.RequireAdminRequest`, protects deployment-level `/api/admin/*` routes.
- A browser user with `role == "admin"`, or an API key with `keys:read` / `keys:write`, can manage the keys exposed by `/api/keys`.

Neither maps cleanly to the approved single permanent deployment owner. In particular, normal API keys can acquire key-management scopes, while the desired owner-only content access must be impossible through API keys.

### Thread and asset authorization is tenant-wide

The service passes `auth.TenantID` into the repository for every normal operation. For example:

```go
func (s *Service) ListThreads(ctx context.Context, auth types.AuthContext, limit int) ([]types.Thread, error) {
    // scope validation omitted
    return s.repo.ListThreads(ctx, auth.TenantID, limit)
}
```

`Repository.ListThreads` and `Repository.SearchThreads` filter only on `threads.tenant_id`. `Repository.GetThread` loads a thread by `(tenant_id, thread_id)`. `Repository.GetAsset` loads any asset by `(tenant_id, asset_id)`. There is no owner predicate, team membership join, thread-share table, public-share table, or reusable access-decision object.

The consequence is broader than list/read. `PostMessage`, multipart uploads, direct-to-R2 upload intents, pending-upload finalization, and signed downloads all inherit tenant-wide access. Any change that fixes only thread listing but leaves asset signing or uploads tenant-scoped would still leak data.

### Attribution has useful legacy snapshots but no durable actor model

Threads retain `created_by`; messages retain `author`; assets retain `created_by`. The multitenancy migration added nullable `created_by_user_id` and `created_by_key_id` fields. These snapshots are valuable for preserving historical attribution, but current UI renders only the free-form author string, and authorization must not trust these strings.

Browser messages are created from the signed-in session directly. There is no hidden browser key in the current dashboard write path, which is the correct direction for the new `User · Web dashboard` attribution model.

### R2 storage is tenant-prefixed and optionally publicly addressable

`assets.MakeStorageKey` currently creates:

```text
agentbox/{tenantID}/{threadID}/{messageHint}/{uuid}-{filename}
```

`AssetStore` can upload, prepare a presigned upload, sign a download, and ingest a ChatGPT file. It cannot list, head, copy, or delete objects, which the backup verifier and disabled-user attachment purge will require. `assets.FakeStore` mirrors the current interface and must remain the test double for these operations.

When `R2_PUBLIC_BASE_URL` is configured, upload records may carry a direct `public_url`. The final system cannot rely on that URL because authenticated and public access must both pass through a thread authorization decision before a short-lived signed URL is produced.

### MCP, CLI, and dashboard all share the same backend behavior

`internal/agentbox/mcpserver/mcpserver.go` exposes `list_threads`, `search_threads`, `get_thread`, `create_thread`, and `post_message`. The MCP HTTP handler authenticates one API key, captures the resulting `AuthContext`, and passes it to every tool call. This is a useful propagation pattern to retain.

The CLI routes all normal commands through `Runner.request`, which appends the active profile key to the request URL. Its `list`, `search`, `create`, `get`, `download`, and `post` commands already use the same public API as MCP and the dashboard. Its bootstrap and login commands, however, are tenant-centric and include compatibility paths that must disappear after cutover.

The dashboard has working thread creation, direct uploads, posting, Markdown rendering, GitHub-flavored tables, syntax highlighting, Mermaid, image previews, and signed downloads. There are no invite, registration, onboarding, team, public-share, or owner-administration pages. `app/keys/keys-view.tsx` currently lists every key returned for a tenant rather than only the signed-in user's credentials.

### Test coverage is broad at the service/HTTP surface but does not verify PostgreSQL authorization

The current suite covers service, HTTP, MCP, CLI, profiles, password hashing, assets, and validation. Most authorization tests use `db.MemoryRepository`, whose behavior is a separate implementation from the SQL repository. There are no PostgreSQL-backed migration/access tests in the repository, and the only GitHub workflow is the npm publishing workflow with a subset of CLI tests.

For a multi-user access-control migration, memory-only tests are insufficient. The SQL predicates, uniqueness constraints, transactions, and migrations need first-class integration coverage.

## State of Ideal System

### Product boundaries

- One deployment is the infrastructure boundary.
- One permanent deployment owner administers users, invitations, teams, credential metadata, disablement, attachment purge, and a read-only owner web content viewer.
- A user is one person plus all credentials acting for that person.
- Teams are overlapping many-to-many groups managed only by the deployment owner.
- Every thread has one owner and is private at creation.
- A canonical thread may be shared with zero or more teams and may have one active public share.
- Normal browser sessions and user credentials receive the same effective thread set.
- The owner's special view-all capability exists only in an authenticated owner browser-session path; normal operations never inspect owner status to broaden access.

### Cross-phase vocabulary

The following names and responsibilities are fixed because several phases depend on them:

- `User` - the deployment-global human account retained when disabled.
- `Credential` - an API-key-backed actor owned by one user. The SQL table may remain `api_keys` to minimize meaningless storage churn, but domain and UI language should use “credential.”
- `Team` - an owner-managed sharing group.
- `TeamMembership` - the active relation between one user and one team.
- `ThreadTeamShare` - the active relation granting a team full participation in one thread.
- `PublicThreadShare` - the single active public token for one thread.
- `ThreadAccess` - the centralized normal-access decision and metadata for a user/thread pair.
- `OwnerWebContext` - an explicit owner browser-session authorization type or service entry point. It must not be constructible from API-key authentication.
- `AttachmentTombstone` - retained attachment metadata after an owner purge deletes the underlying R2 object.

### Authentication context

The final normal authentication context contains stable user and actor identity, not tenant identity. Its exact Go fields may follow existing conventions, but every authenticated request must resolve at least:

```text
user_id
subject_type = user_session | api_key
actor_id = session_id or credential_id
user_display_name snapshot source
actor_display_name
scopes for API-key subjects
is_owner only for browser-session owner checks
```

`requireAuthContext` must require a valid active user. Disabled users and revoked sessions/credentials do not produce an authentication context.

### Canonical schema and constraints

The final migration history is ordered and recorded in a `schema_migrations` table. Runtime repository calls do not execute DDL.

The final relational model explicitly supports:

- `users`, with globally unique `lower(email)`, `disabled_at`, and one protected owner marker; a unique partial constraint permits exactly one active owner.
- `user_sessions`, owned by a user and revocable in bulk.
- `api_keys`, owned by a user, with active-name uniqueness on `(user_id, lower(name))`, hashed secrets, token prefix, purpose, last-used time, and revocation.
- `cli_login_codes`, owned by a user and single-use.
- `signup_invitations`, with hashed token, expiry, revocation, and consumption state.
- `signup_invitation_teams`, linking an invitation to zero or more initial teams.
- `teams`, with deployment-wide unique slug and stable ID.
- `team_memberships`, unique on `(team_id, user_id)` and removable without deleting historical users.
- `threads.owner_user_id`, non-null after migration.
- `thread_team_shares`, unique on `(thread_id, team_id)`.
- `public_thread_shares`, constrained to at most one active share per thread.
- `messages` and `assets` with nullable legacy-compatible stable user/credential references plus immutable display snapshots.
- `assets.purged_at` and purge metadata so deleted objects render as tombstones.
- `pending_uploads` tied to stable uploader user and credential identity.

Legacy `tenant_id`, tenant role, and tenant tables are migration scaffolding only. They do not exist in the finished authorization path.

### Central normal-access contract

All normal list, search, get, post, upload, finalize, preview, and download operations use the same predicate:

```sql
t.owner_user_id = $current_user_id
or exists (
  select 1
  from thread_team_shares tts
  join team_memberships tm on tm.team_id = tts.team_id
  where tts.thread_id = t.id
    and tm.user_id = $current_user_id
)
```

The repository must express this as reusable SQL fragments or dedicated query functions so callers cannot accidentally invent weaker predicates. The service must return `THREAD_NOT_FOUND` for inaccessible thread IDs on normal surfaces so the existence of private content is not disclosed.

List/search queries implement the predicate in SQL and remain indexed and paginated. They must not list all threads and filter in Go. Asset signing joins asset -> message -> thread and applies the same access predicate before an R2 URL is signed.

### Visibility operation

The backend exposes one atomic visibility operation shared by web, MCP, and CLI. Its public input/output semantics are fixed by `docs/user-team-sharing-spec.md`:

```text
thread_id
add_teams[]
remove_teams[]
public?
regenerate_public_link?
```

A read-only call with only `thread_id` returns current shares and the caller's available teams. Mutations are idempotent and transactional. Thread creation remains private-only.

The MCP tool is exactly `manage_thread_visibility`. The CLI surface is exactly `agentbox visibility <thread-id>` with repeatable `--share-team` / `--unshare-team` and the public-link flags from the specification.

### Public access contract

`/share/<opaque-token>` is live, read-only, unlisted, and `noindex`. Public reads resolve the token to one thread, return only safe display fields, and sign only that thread's non-purged attachments. Public token revocation or regeneration immediately invalidates old URLs.

The public page reuses the existing message renderer rather than maintaining a second Markdown implementation.

### Migration and cutover contract

Before production authorization changes, a verified PostgreSQL backup and R2 backup/inventory exist with matching manifests and counts. A dry run reports missing or orphaned content. Cutover creates the permanent owner account and assigns every legacy thread to it privately while preserving IDs, message ordering, bodies, attachment rows, storage keys, and free-form author snapshots.

Credentials, sessions, tenant records, and CLI profile metadata are deliberately reset. Phase 14 removes the old application path and leaves the branch code-complete with no permanent compatibility fallback. Phase 15 applies and verifies that finished system against production.

## Decisions and Assumptions

### Decisions

1. **Preserve all thread, message, and attachment data.** Production content must be backed up and verified before cutover; authentication and credential data may be reset.
2. **Replace tenant authorization rather than extend it.** The current tenant implementation is untested and does not match the user/team model.
3. **Use one permanent deployment owner.** Additional admins, ownership transfer, and user-created teams are out of scope.
4. **Limit view-all-content to owner browser sessions.** The owner's CLI, MCP URLs, and API keys receive only normal user access.
5. **Keep new threads private on every surface.** Sharing is always a separate explicit visibility operation; create APIs and commands receive no visibility flags.
6. **Make teams many-to-many.** Users may belong to zero or several teams, and a thread may be shared with several teams without copies.
7. **Give every currently authorized participant full thread participation.** They may post, upload, add teams they belong to, remove any share, publish, unpublish, and regenerate the public URL.
8. **Use one unified accessible inbox.** Normal list/search operations return owned threads plus threads shared through any current team membership.
9. **Use live, opaque, revocable public URLs.** Public pages are read-only, `noindex`, render Markdown, and expose authorized attachment previews/downloads.
10. **Use one visibility MCP tool and one CLI subcommand.** `manage_thread_visibility` and `agentbox visibility` own both team and public changes.
11. **Create ChatGPT, Claude, and local credentials on demand.** They use separate secrets and actor attribution; onboarding presents them as numbered steps.
12. **Assume one local machine for first onboarding.** Additional machine credentials are created later from settings.
13. **Let users manage only their credentials.** The owner may inspect metadata and revoke, but cannot retrieve secrets.
14. **Disable users rather than deleting or transferring them.** History and attribution remain; sessions, credentials, and memberships are revoked.
15. **Allow owner purge of disabled-user attachments only.** Purge is by stable uploader user ID, deletes R2 objects, and leaves tombstones.
16. **Defer Raycast distribution.** The data model may support future Raycast credentials, but this track does not publish or redesign the extension.

### Assumptions

1. **[Cutover-critical] The existing production content belongs to the future deployment owner.** Every legacy thread can therefore be assigned to the new owner as a private thread without requiring per-thread ownership classification.
2. **[Cutover-critical] Existing R2 rows contain the authoritative storage keys.** Objects do not need to be renamed; the migration treats legacy keys as opaque.
3. **The public link is manually shared.** No email-delivery service is required for invitations or public shares.
4. **A public share stores enough token material to redisplay its URL.** Unlike invitations, which are validate-once and can store only a hash, an active public URL must remain copyable from the dashboard. Store a high-entropy token encrypted or plaintext-at-rest according to the deployment's existing database trust model, while never logging it; do not weaken invitations to match this behavior.
5. **Owner content access is read-only.** The specification asks the owner to browse all content for debugging; it does not authorize posting or changing ordinary thread content through the bypass.
6. **Removing the final team share may remove the caller's own access.** The mutation commits successfully, and later requests are denied, as explicitly specified.
7. **Team removal preserves contributions and shares.** Removing a membership changes who qualifies for access; it does not delete the team's share rows or historical messages.
8. **The existing browser-assisted CLI login remains a supported optional setup path.** The first-run onboarding prompt is the primary local setup, but `agentbox login` is useful for later machines once made user-scoped.
9. **The API continues to accept query-string API keys for MCP and CLI.** This track changes ownership and access, not the existing remote MCP credential transport.
10. **Pagination can preserve the current limit-based API initially, but SQL indexes and predicates must support cursor pagination later.** No phase may introduce in-memory filtering or N+1 team lookups on list/search hot paths.
11. **Production deployment can tolerate a planned write pause for the final content-preserving cutover.** If zero-downtime writes are required, the final rollout phase must be amended before execution because dual writes would materially change the plan.
12. **No external consumer requires the legacy `/api/admin/tenants`, `/api/admin/keys`, `agentbox init`, or `agentbox provision tenant` contracts.** The final cutover removes them. This must be verified against production usage before the deletion phase.

## Acceptance Criteria

1. A verified PostgreSQL backup and R2 backup/inventory cover every production thread, message, asset row, and referenced R2 object before cutover.
2. A dry-run report identifies row counts, object counts, orphan rows, missing objects, and the exact owner backfill without mutating production.
3. Existing production thread IDs, titles, timestamps, messages, message order, content types, attachment metadata, storage keys, and historical author strings survive cutover.
4. The final login and CLI profile model has no tenant selector or tenant metadata requirement.
5. Exactly one permanent deployment owner exists and cannot be disabled, deleted, demoted, or transferred through normal APIs.
6. Public signup fails; a valid owner-generated invitation is required to register.
7. Invitations are random, hashed, expiring, revocable, single-use, and can atomically create zero or several initial team memberships.
8. Newly registered users immediately appear in the owner dashboard and receive an authenticated session plus resumable onboarding.
9. Every new thread is private regardless of whether it is created by browser, API, MCP, or CLI.
10. A user's browser sessions and active credentials can access exactly the same owned/team-shared thread set, subject only to credential scopes.
11. A user with no teams has a fully functional private inbox and can create public links.
12. A user may belong to several teams; a thread may be shared with several teams; no thread, message, or attachment is copied by sharing.
13. List, search, get, post, direct upload, pending-upload finalize, preview, and download all enforce the same stable-ID-based access rule.
14. Search and asset endpoints do not reveal the existence or metadata of inaccessible threads.
15. Any authorized participant can atomically add shares to teams they belong to, remove any existing share, publish, unpublish, and regenerate the public link.
16. The MCP server exposes exactly one new `manage_thread_visibility` tool, and `create_thread` remains private-only.
17. The CLI exposes `agentbox visibility <thread-id>` with the specified team/public flags, and `agentbox create` remains private-only.
18. The dashboard exposes a unified inbox with All, Private, Shared with me, per-team, and Public filters.
19. Messages visibly distinguish user and actor surface, including Web dashboard, ChatGPT, Claude, and Local CLI, while preserving legacy snapshots.
20. ChatGPT, Claude, and local credentials are independently created, independently revocable, and shown only once.
21. Onboarding presents numbered ChatGPT, Claude, and local setup cards and generates a local-agent prompt that installs the npm CLI, saves a profile, and successfully lists accessible threads.
22. Public links are opaque, live, read-only, `noindex`, revocable, and render existing Markdown/Mermaid/code/table functionality without private account metadata.
23. Public attachment URLs are short-lived and can be signed only through a valid active share for the containing thread.
24. The owner can manage users, invitations, teams, memberships, and credential metadata from the web dashboard.
25. Owner view-all-content works only through an owner browser session; an owner API key cannot use that capability.
26. Disabling a user immediately invalidates sessions and credentials, removes effective team access, prevents login, and preserves all content and attribution.
27. Purging a disabled user's attachments deletes only objects uploaded by that user, is safe to retry, and leaves readable tombstones.
28. PostgreSQL-backed tests prove migrations, access predicates, uniqueness, transactions, and public/owner authorization; memory-only tests are not the sole evidence.
29. Runtime repository calls no longer execute schema DDL.
30. The finished codebase has one authorization model: legacy tenant routes, fields, profile metadata, and compatibility paths are removed.

## Cross-Session Execution Protocol

This blueprint is also the durable handoff record for agents implementing the track across separate ChatGPT sessions. Zodex agents execute Phases 1-14. The credentialed local agent executes Phase 15 only after the shared branch is code-complete. Every implementation agent must follow the relevant protocol before and during work.

### Start-of-session procedure

1. Work only on `feat/user-team-sharing`. Never commit or push this work to `main`.
2. Open the existing checkout at `/home/zodex-agent/work/agentbox` when present. If it is absent, clone `amxv/agentbox`, fetch the remote branch, and check out `feat/user-team-sharing` tracking `origin/feat/user-team-sharing`.
3. Inspect `git status` before changing anything. Do not discard, reset, overwrite, or clean work that may belong to another agent. Resolve or preserve any unexpected state before proceeding.
4. Pull with fast-forward-only semantics. Do not force-push, rewrite shared history, or rebase already-pushed commits on this branch.
5. Read `docs/user-team-sharing-spec.md` and this blueprint fully, including `Implementation Progress` and `Amendments`.
6. Inspect the recent branch history and relevant diffs. Treat committed code and the progress ledger as the source of truth for what previous agents actually completed.
7. Continue the earliest unfinished phase or the explicitly recorded next slice. Do not redo completed work merely because a prior session used a different implementation style.

### Work and checkpoint discipline

- Work in coherent, reviewable slices. A slice may be smaller than a phase when the phase cannot safely fit in one session, but it must leave the branch buildable and must have a clear tested outcome.
- Run the narrow checks that prove the slice, followed by the appropriate broader regression checks for the touched subsystems. Never mark work complete based only on compilation when the phase requires behavior or authorization evidence.
- After every completed slice or phase, immediately update `Implementation Progress` in this blueprint with the status, commit, validation performed, remaining work, and the exact next recommended action.
- Commit the code and progress update together when practical. Push the branch immediately after the commit succeeds. Do not accumulate several completed slices locally before pushing.
- If interrupted, push the latest coherent checkpoint and record what remains. When partially written code cannot be made coherent, do not commit broken work; instead restore a clean branch state and record the investigation or blocker in the progress log.
- Amend this plan only when repository evidence invalidates an assumption or changes later phases. Record those changes under `Amendments`; routine implementation choices belong in code and commit messages, not in the amendment log.

### Remote-machine boundary

Shared Zodex agents are responsible for completing every code-bearing part of Phases 1-14, including:

- canonical migrations and dry-run/backup tooling;
- PostgreSQL-backed tests using a credential-free local or CI test database where available;
- R2 behavior behind fakes or test doubles, plus production-safe commands and runbooks;
- all Go backend, Next.js dashboard, MCP, CLI, npm package, and documentation changes;
- final removal of tenant-era code from the feature branch once the replacement path is proven by tests.

Shared Zodex agents must not attempt or claim completion of:

- production PostgreSQL backup or row-count verification;
- production R2 inventory, backup, deletion, or object verification;
- production Vercel deployment or environment-variable changes;
- creation or rotation of real production owner, ChatGPT, Claude, or local credentials;
- the live maintenance window and production cutover.

Those live actions belong only to Phase 15. When every code-bearing task is complete, the Zodex agents must mark Phases 1-14 `Complete`, leave Phase 15 `Reserved for local agent`, and stop. The exact production runbook, commands, expected evidence, and rollback procedure must already be committed for the credentialed local agent.

### Credentialed local-agent boundary

The local agent starts only after Phases 1-14 are complete on the branch. It must read the full specification, this blueprint, all progress checkpoints, and all amendments before executing Phase 15. It may make and push narrowly scoped fixes discovered during real backup, deployment, migration, or production verification, but it must not redesign the approved architecture during cutover. Any discovery that changes later work belongs in `Amendments` immediately.

### Shared-branch safety

- Prefer additive commits and normal `git push` to `origin feat/user-team-sharing`.
- Never use `git push --force`, destructive resets, broad cleanup commands, or branch deletion.
- Before each push, verify the current branch name and inspect the outgoing commits.
- Keep secrets, copied MCP URLs, production tokens, database URLs, and environment dumps out of source files, commits, test fixtures, and command output.
- When a long-running Zodex command returns a `session_handle`, poll it with `write_stdin`; do not start a duplicate build, test, or push command.
- Always provide an explicit repository `workdir` to Zodex commands. Use delayed polling for long Go/Next.js builds rather than frequent repeated checks.

## Implementation Progress

### Phase status

Allowed statuses are `Pending`, `In progress`, `Code complete`, `Complete`, and `Reserved for local agent`.

| Phase | Status | Last commit | Evidence / remaining work |
|---|---|---|---|
| 1. Canonical migrations and backup workflow | In progress | `3cb9265` | Canonical checksum-ledgered migration runner and PostgreSQL CI coverage are implemented; backup/preflight tooling and R2 verification remain. |
| 2. Deployment-global users and credentials | Pending | — | No implementation started. |
| 3. User-owned private thread access | Pending | — | No implementation started. |
| 4. Invitations and zero-team registration | Pending | — | No implementation started. |
| 5. Owner-managed teams and memberships | Pending | — | No implementation started. |
| 6. Onboarding and connector setup | Pending | — | No implementation started. |
| 7. Team sharing and effective access | Pending | — | No implementation started. |
| 8. Public sharing and public page | Pending | — | No implementation started. |
| 9. MCP and CLI visibility operation | Pending | — | No implementation started. |
| 10. Unified inbox and attribution UX | Pending | — | No implementation started. |
| 11. Owner user and credential administration | Pending | — | No implementation started. |
| 12. Disabled-user attachment purge | Pending | — | No implementation started. |
| 13. Owner-only web content viewer | Pending | — | No implementation started. |
| 14. Final code cutover and tenant removal | Pending | — | Zodex implementation phase; finish all code and remove the tenant path. |
| 15. Credentialed production cutover | Reserved for local agent | — | Start only after Phases 1-14 are complete and pushed. |

### Checkpoint log

Append one entry immediately after every pushed slice or phase using this shape:

```text
YYYY-MM-DD — Phase N / short slice name
- Status: In progress | Code complete | Complete | Reserved for local agent
- Commit: <short SHA>
- Implemented: <observable outcome>
- Validation: <tests/checks actually run and their result>
- Remaining: <specific unfinished work or “None for this slice”>
- Next: <the exact recommended next action for the next agent>
```

2026-08-01 — Phase 1 / canonical migration runner
- Status: In progress
- Commit: `3cb9265`
- Implemented: Embedded the ordered SQL migration files as the sole schema source, added advisory-lock serialization plus `schema_migrations` version/name/checksum tracking and drift detection, switched explicit and startup migration entry points to `Repository.Migrate`, removed schema DDL from the runtime repository interface and every request hot path, added legacy-content/idempotency/drift/hot-path PostgreSQL integration tests, and added a PostgreSQL-backed verification workflow.
- Validation: `go test ./...` passed; PostgreSQL-specific tests were discovered and skipped because Zodex has no `TEST_DATABASE_URL`; `go vet ./...`, `bun run build:api`, `bun run build:cli`, and `git diff --check` passed. The new CI workflow supplies PostgreSQL and runs the integration tests on the branch.
- Remaining: Implement the provider-neutral PostgreSQL backup/export plus row-count manifest, extend R2 storage inventory/head/copy capabilities and fakes, verify referenced/missing/orphaned objects, and add repeatability/readiness tests. Real production backup evidence remains a local credentialed verification gate.
- Next: Extend `AssetStore` and `FakeStore` with exact-key metadata, inventory, and recovery-copy operations, then implement the Phase 1 preflight command and machine-readable manifest around database counts/export and R2 verification.

## Plan Phases

The sequence intentionally adds and proves the new path before changing production semantics. Every phase ends with a buildable, testable system. Phases 1-14 are completed on Zodex and end with a code-complete branch containing only the new authorization model. Phase 15 is the separate credentialed local production cutover.

### Phase 1: Make migrations canonical and produce a verified content backup workflow

#### Files to read before starting

**Specification and migration contract:**

- `docs/user-team-sharing-spec.md` (Sections 13, 14, 16, and acceptance criteria 1 and 20) - the non-negotiable content-preservation and storage rules.
- `migrations/0001_init.sql` through `migrations/0005_multitenancy_auth.sql` (read in full; each is small) - the existing but currently non-canonical migration history.

**Current schema execution:**

- `internal/agentbox/db/repository.go` (`Repository.EnsureSchema`, lines approximately 45-270) - the duplicate inline DDL and indexes that currently execute from normal methods.
- `internal/agentbox/db/repository.go` (`ListThreads`, `SearchThreads`, `CreateThread`, and one auth method) - confirm the repeated `EnsureSchema` calls that must leave hot paths.
- `cmd/migrate/main.go` (read in full) - the current migration executable delegates entirely to `EnsureSchema`.
- `cmd/api/main.go` (`main`, `repositoryWithClose`) - preserve optional startup migration behavior without keeping repository-call DDL.

**R2 and deployment patterns:**

- `internal/agentbox/assets/assets.go` (`AssetStore`, `R2Store`, `MakeStorageKey`) - extend storage capabilities without changing existing opaque keys.
- `internal/agentbox/assets/fake.go` (read in full) - keep test parity for object inventory/copy/head operations.
- `internal/agentbox/config/config.go` (`Config`, `LoadFromEnv`) - add backup destination/config only where a deploy-time operation needs it.
- `package.json` (scripts block) and `AGENTS.md` (local checks/deployment sections) - expose documented migration/preflight entry points using repository conventions.

**Tests and CI:**

- `internal/agentbox/httpapi/server_test.go` (`TestHTTPTenantIsolationAndAuthMethods`) - representative current behavior that must still pass before authorization cutover.
- `.github/workflows/publish-agentbox-npm.yml` (steps 19-47) - the only current CI pattern; add a separate verification workflow rather than overloading release publishing.

#### What to do

Establish one ordered migration runner backed by a `schema_migrations` ledger. The checked-in files under `migrations/` become authoritative. `cmd/migrate` and optional API startup migration use the same runner. Repository methods stop calling DDL; `EnsureSchema` either becomes a thin call to the canonical runner used only by explicit bootstrap/test code or is replaced by a clearly named migration interface.

Create a production preflight/backup command under `cmd/` or the CLI's deployment-admin area that produces a machine-readable manifest before any new-schema cutover. It must capture PostgreSQL table counts for threads, messages, assets, and pending uploads; export a recoverable database backup using an explicit provider-neutral mechanism; enumerate every `assets.storage_key`; verify each referenced R2 object exists; record size/ETag where available; and copy or otherwise back up those objects to a distinct recovery prefix or bucket. Missing objects and orphan rows must make the preflight non-successful while still producing a report.

Do not rename existing R2 keys. Do not mutate production content in this phase. The output must be repeatable, timestamped, and safe to rerun.

On the shared Zodex branch, implement and test this workflow without production credentials. Use test databases, `assets.FakeStore`, and credential-free fixtures to prove the manifest, failure, retry, and idempotency behavior. Do not run or claim a real production backup from Zodex; Phase 15 is the only phase that gathers and records that production evidence.

Add PostgreSQL-backed migration tests that start from representative legacy schemas, apply migrations once and twice, and verify the ledger and content are unchanged on retry. Add CI capable of running those tests against a real PostgreSQL service.

This phase creates only the migration/backup foundation required by the first production-safe slice; it does not add teams or change authorization.

#### Validation strategy

- A test starting from the schema represented by `0005_multitenancy_auth.sql` must fail before the canonical migration runner exists and pass after it applies each migration once, records versions, and succeeds idempotently on rerun.
- A fixture with three asset rows, two present fake objects, and one missing object must produce a manifest that names the missing key and refuses to report backup readiness.
- A complete fixture must produce matching row/object counts and a restorable database/object backup manifest.
- Existing API, service, CLI, and asset tests must still pass because runtime semantics have not changed.
- A query or repository test must demonstrate that `ListThreads` no longer invokes migration DDL.

#### What must not break

- Existing development databases must migrate without dropping threads, messages, assets, API keys, sessions, or pending uploads.
- `AGENTBOX_AUTO_MIGRATE` may remain a deployment convenience, but startup and request handling must not race several copies of non-versioned DDL.
- Existing opaque R2 storage keys and signed downloads must continue working.

### Phase 2: Introduce deployment-global users, the permanent owner, and user-owned credentials

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 2.2-2.4, 5, 7, 11, and 13) - owner permanence, user/credential ownership, and resettable auth data.

**Identity types and persistence:**

- `internal/agentbox/types/types.go` (`AuthContext`, `Tenant`, `User`, `UserSession`, `CLILoginCode`, `APIKey`) - replace tenant-shaped identity while retaining stable user/actor fields.
- `internal/agentbox/db/repository.go` (`CreateAPIKey`, `ListAPIKeys`, `RevokeAPIKey`, `FindAPIKeyBySecret`, `UpsertProvisionedUser`, `FindUserByEmail`, session and CLI-code methods) - current tenant uniqueness and lookup behavior.
- `internal/agentbox/db/memory.go` (corresponding identity/key methods) - keep the test repository contract aligned, but do not let it substitute for SQL tests.
- `internal/agentbox/service/service.go` (`Repository` interface; `CreateAPIKeyWithScopes`; provisioning, login, authentication, CLI login; `requireAuthContext`) - current identity orchestration and tenant assumptions.

**HTTP and browser:**

- `internal/agentbox/httpapi/server.go` (`authLogin`, `authMe`, `keys`, `key`, `requireAuth`, `requireAdmin`, `tenantAdmin`) - separate bootstrap administration, user credential management, and owner browser identity.
- `internal/agentbox/auth/auth.go` (`RequireAdminRequest`) - retain the deployment secret only for owner bootstrap/recovery, not daily user/team administration.
- `app/components/session.ts` (read in full) - update the browser's auth contract.
- `app/login/login-view.tsx` (state and submit payload) - remove Tenant ID and tenant copy.
- `app/keys/keys-view.tsx` (types, `loadKeys`, `createKey`, `deleteKey`) - make this a per-user credential surface.

**CLI:**

- `internal/agentbox/profiles/profiles.go` (`Profile`, `SaveProfile`, `writeStore`, `normalizeProfile`) - remove tenant metadata from the final profile shape.
- `internal/agentbox/cli/login.go` (`cliLoginExchangeResponse`, `runLogin`, `exchangeCLILoginCode`) - return user/credential metadata without tenant.
- `internal/agentbox/cli/bootstrap.go` (`runProvision`, key commands, `createTenantAPIKeyForProfile`) - introduce owner bootstrap and user-scoped credential calls while leaving final legacy deletion to Phase 14.
- `internal/agentbox/cli/cli_test.go` (`TestCLIKeysListAndRevokeUseTenantProfile`, `TestCLIProvisionTenantCreatesProfile`, `TestCLILoginSavesTenantProfile`) - replace tenant assertions with user ownership assertions.

#### What to do

Add the deployment-global identity schema through canonical migrations. Convert users to globally unique email identities, add a protected owner marker with a database constraint preventing multiple owners, and make sessions, CLI login codes, and API keys directly user-owned. Active credential names become unique per user rather than per tenant. Preserve hashed-secret and one-time-display behavior.

Introduce an explicit owner bootstrap flow, for example `agentbox provision owner`, protected by `AGENTBOX_ADMIN_KEY`. It creates the sole owner account idempotently without creating tenant records. After bootstrap, normal owner administration will use the owner browser session in later phases; the deployment secret is not a general web-session replacement.

Change login to email/password only. Change API-key and session authentication to resolve an active user and a concrete actor. Disabled-user rejection is completed in Phase 11, but the shape must already support it. Ensure owner status is present only on session-derived auth contexts and is not a scope that API keys can request.

Make `/api/keys` list/create/revoke credentials belonging to `auth.UserID` only. A credential with `keys:*` scopes must not manage another user's credentials. The owner-wide metadata/revoke endpoints are separate and arrive in Phase 11.

Update browser session types, login UI, keys UI, CLI browser login, and CLI profiles to the user-owned model. Existing profiles and secrets are explicitly disposable; do not create a permanent tenant compatibility layer. It is acceptable for old profile files to require re-login after production cutover.

Thread data remains served by the old tenant predicate temporarily in this phase, but only the bootstrapped owner is exposed in the transitional deployment. Do not invite ordinary users until Phase 4 and do not claim multi-user isolation until Phase 3.

#### Validation strategy

- A PostgreSQL test must prove only one owner can be created, repeated owner bootstrap is idempotent for the same account, and a second owner attempt fails.
- Two users must be able to own active credentials with the same label `chatgpt`; rotating one must not change the other's key.
- A user session and each of that user's API keys must authenticate to the same stable `UserID` while preserving distinct actor IDs/names.
- `/api/keys` must return only the caller's credentials even when another user has identically named keys.
- A login request containing the old `tenant_id` field must not change account selection; the UI must no longer ask for it.
- CLI profile serialization and browser-assisted login must work with base URL, API key, user ID, key label, and auth type only.

#### What must not break

- Secrets remain hashed and shown only once.
- Query-string MCP authentication and normal bearer/query API-key authentication remain functional.
- Existing owner content remains readable during the transitional phase; no production invitation flow may be enabled before private thread ownership lands.

### Phase 3: Cut normal thread, message, upload, and asset access over to user ownership

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 2.6, 3.1, 3.4, 4, 10, 13, and 14) - private-by-default ownership, centralized authorization, attribution, and migration backfill.

**Thread and asset persistence:**

- `internal/agentbox/db/repository.go` (`ListThreads`, `SearchThreads`, `CreateThread`, `CreateThreadWithMessage`, `GetThread`, `GetAsset`, pending-upload methods, `PostMessage`) - replace every tenant predicate in one coherent access slice.
- `internal/agentbox/db/memory.go` (the same methods) - update the in-memory model for unit tests.
- `internal/agentbox/service/service.go` (`ListThreads` through `SignedAssetDownloadURL`) - centralize effective access rather than performing ad hoc existence checks.
- `internal/agentbox/types/types.go` (`Thread`, `Message`, `Asset`, `PendingUpload`, `NewAsset`, search DTOs) - introduce owner/uploader and durable attribution fields while preserving legacy snapshots.

**R2:**

- `internal/agentbox/assets/assets.go` (`UploadBytesParams`, `PresignedUploadParams`, `UploadChatGPTFile`, `MakeStorageKey`, `PublicURLForKey`) - new keys use user/thread identity; legacy keys remain opaque.
- `internal/agentbox/assets/fake.go` (matching methods) - preserve testability.

**HTTP and client consumers:**

- `internal/agentbox/httpapi/server.go` (`threads`, `getThread`, `postMessage`, `createUploadIntents`, `assetSubroutes`, `viewerThreads`, `viewerThread`, `withViewerAssetURLs`) - all normal surfaces must call the same authorized service path.
- `internal/agentbox/mcpserver/mcpserver.go` (`listThreads`, `searchThreads`, `getThread`, `createThread`, `postMessage`) - preserve tool contracts while semantics become user-private.
- `internal/agentbox/cli/cli.go` (`runList`, `runSearch`, `runCreate`, `runGet`, `runDownload`, `runPost`) - verify no client-side tenant or owner bypass assumptions.
- `app/components/agentbox-write.ts`, `app/threads/inbox-view.tsx`, and `app/threads/[threadId]/thread-view.tsx` - preserve current working write/read UX under the new private model.

**Tests:**

- `internal/agentbox/service/service_test.go` (`TestServiceThreadAndMessageFlow`, `TestServiceTenantIsolationAndAPIKeys`) - replace tenant isolation with user ownership.
- `internal/agentbox/httpapi/server_test.go` (`TestThreadRoutesAndMultipartAsset`, `TestDirectUploadIntentAndFinalize`, `TestHTTPTenantIsolationAndAuthMethods`, `TestAPIKeyScopesConstrainThreadAndAssetRoutes`) - cover every access path.
- `internal/agentbox/mcpserver/mcpserver_test.go` (`TestMCPToolsUseTenantAuthContext`) - convert to user-private semantics.

#### What to do

Add `threads.owner_user_id` and backfill every legacy thread to the permanent owner selected by the migration preflight. Keep existing thread IDs and all content untouched. New thread creation always writes the authenticated user's ID and accepts no visibility input.

Introduce the `ThreadAccess` repository/service boundary for normal access. In this phase its predicate is owner-only; later team sharing widens the same predicate rather than adding a second path. List/search/get/post/upload/download must all consume that boundary. An inaccessible ID returns the same normal not-found response as a missing ID.

Change asset lookup/signing so an authenticated asset ID is resolved through its containing message and thread under the caller's access predicate. Do not load an asset tenant-wide and sign it in a separate step. Pending upload creation and finalization must bind to uploader user and actor IDs and to an accessible thread.

Persist new message/asset attribution as stable user/credential references plus display snapshots. Browser sessions use an actor label such as `Web dashboard`; credentials use their own label. Preserve `author`/`created_by` legacy strings, and allow migrated rows to have null new actor references.

New R2 object keys should include stable user and thread identity, but authorization must use database relationships rather than parsing keys. Stop producing or consuming direct `R2_PUBLIC_BASE_URL` links for new assets. Existing `storage_key` values continue to work unchanged.

This is the first complete authorization cutover for normal private use. Do not add teams yet.

#### Validation strategy

- Two user fixtures must create private threads; every browser/API/MCP/CLI read, search, post, upload, finalize, and download attempt across users must return not found or permission denial without metadata leakage.
- The same user's browser session and several credentials must see and modify the same thread while messages retain distinct actor attribution.
- A PostgreSQL query-plan/integration test must demonstrate list/search filtering occurs in SQL with an index on owner/update ordering, not by reading all rows into Go.
- An asset-ID test must prove a user cannot sign an attachment from another user's thread, even if the asset ID is known.
- A migration fixture with legacy author strings and storage keys must retain both exactly after owner backfill.
- Existing Markdown, multipart upload, direct upload, MCP, CLI download, and dashboard smoke flows must continue to work for the owner.

#### What must not break

- Legacy content IDs, timestamps, order, author strings, filenames, sizes, and storage keys must not change.
- `create_thread` and `agentbox create` remain private-only.
- API-key scopes still constrain read/write/asset/MCP operations in addition to user ownership.

### Phase 4: Add owner-generated invitations and zero-team registration

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 2.2, 5, 6 introduction, and acceptance criteria 3-5) - invitation lifecycle and immediate onboarding redirect.

**Auth patterns:**

- `internal/agentbox/service/service.go` (session-secret generation, `Login`, `AuthenticateSession`, `CodedError`) - reuse secure token/session patterns without tenant provisioning.
- `internal/agentbox/db/repository.go` (session and CLI-code transaction patterns) - implement invitation consumption as one SQL transaction.
- `internal/agentbox/auth/password.go` (read in full) - use the existing password hash contract.
- `internal/agentbox/httpapi/server.go` (`authLogin`, `routes`, `requireAuth`, error mapping) - add public invite validation/registration and owner-session-protected admin routes.

**Next.js patterns:**

- `app/login/login-view.tsx` and `app/login/auth.module.css` - closest registration-page visual/form pattern.
- `app/api/auth/login/route.ts` and `app/api/_proxy/proxy.ts` - thin proxy pattern for new invite routes and `Set-Cookie` forwarding.
- `app/components/session.ts` - registration response must establish the same browser session contract.

**Tests:**

- `internal/agentbox/httpapi/server_test.go` (`TestBrowserSessionAuthLifecycleAndTenantKeys`) - session-cookie assertions to preserve.
- `internal/agentbox/service/service_test.go` (`TestServiceProvisionUserSetupToken`) - replace provisioning assumptions with invitation transaction tests.

#### What to do

Add hashed, random, expiring, revocable, single-use signup invitations. The permanent owner creates and revokes invitations through owner-session-only APIs; do not expose these operations to API keys or the deployment admin secret after bootstrap. This first invitation slice creates no team memberships.

Add a public invite landing/registration route under the dashboard. Valid-token inspection returns only safe invitation state. Registration accepts email, display name, and password and atomically creates the user, consumes the invitation, creates the browser session, and returns a redirect to `/onboarding`. Global email uniqueness and invitation single-use must be enforced by schema/transaction constraints so concurrent submissions cannot create duplicate accounts.

Add an initial owner web page that lists users and invitations and can create a no-team invitation with a chosen expiry. A new user must appear in the list immediately. Public `/signup` without a token remains unavailable.

Do not add team selection yet; Phase 5 extends the same invitation transaction.

#### Validation strategy

- A valid invitation must create one user and one authenticated session and redirect to onboarding.
- Concurrent registration attempts with the same token must produce exactly one user and consume the token once.
- Expired, revoked, malformed, and consumed links must create no user/session and reveal no existing account details.
- Reusing an email already registered must leave the invitation and database in a defined, test-covered state without partial membership or session rows.
- An API key owned by the deployment owner must be unable to create or list invitations; the owner browser session must succeed.
- A newly registered user with no teams must see an empty private inbox and be able to create a private thread.

#### What must not break

- Normal email/password login and browser-assisted CLI login remain functional.
- Invitation tokens must never be stored or logged in plaintext after creation.
- The owner bootstrap secret must not become a browser invitation API credential.

### Phase 5: Add owner-managed teams, memberships, and invitation team assignment

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 2.5, 5, 8, and 10) - owner-only team administration and overlapping memberships.

**Identity/invitation code produced by Phase 4:**

- The new `User`, invitation, owner-session service, repository, HTTP route, and dashboard modules (read their public contracts and transaction boundaries) - extend rather than duplicate owner administration.
- The migration introduced in Phase 4 - follow the canonical migration discipline from Phase 1.

**Current client metadata surfaces:**

- `app/components/session.ts` - expose the user's own teams only if useful to global navigation; avoid loading full membership data on every auth check.
- `app/threads/inbox-view.tsx` (loading and header state) - this will later consume team filters, but sharing remains private until Phase 7.

**Tests:**

- PostgreSQL invitation transaction tests from Phase 4 - extend them to initial memberships.
- `internal/agentbox/httpapi/server_test.go` owner-session authorization tests introduced in Phase 4 - apply the same boundary to teams/users.

#### What to do

Add `teams` and `team_memberships` with stable IDs, globally unique slugs, and uniqueness constraints preventing duplicate memberships. Only an owner browser session can create/rename teams or add/remove users. Users may belong to zero or several teams.

Extend invitations with `signup_invitation_teams`. Owner invitation creation may select zero or several existing teams. Registration consumes the invitation and creates all requested memberships in the same transaction as user/session creation. A team deleted or otherwise unavailable before consumption must produce a deliberate, test-covered outcome; prefer preventing destructive team deletion and allowing rename/member changes instead.

Add owner dashboard views for all teams, members, and each user's team list. Add a normal authenticated endpoint that returns only the caller's available teams (ID, slug, name) for later visibility controls. Users cannot mutate memberships.

This phase changes team administration and registration only. It must not make any existing thread team-visible yet.

#### Validation strategy

- One user must be assignable to Team ABC and Team ADE; another may have no teams.
- A registration invitation with two initial teams must atomically create both memberships; a forced failure must create neither the user nor a partial membership set.
- Duplicate membership and duplicate team-slug races must be resolved by database constraints and idempotent owner APIs.
- A normal user/API key can list only its own available teams and cannot create teams or alter memberships.
- Removing a membership must not delete the user, team, messages, or any future share rows.

#### What must not break

- Existing private thread visibility remains owner-only.
- Invitation links without teams continue to work.
- Team display-name changes must not break CLI/MCP identifiers; stable IDs and slugs remain distinct.

### Phase 6: Build resumable onboarding and independent ChatGPT, Claude, and local setup

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Section 6 and Section 7) - exact three-step order, click-to-create behavior, MCP URL requirements, and local-agent prompt contents.

**Credential creation:**

- `internal/agentbox/service/service.go` (credential creation and scope helpers after Phase 2) - create purpose-specific user credentials with one-time secrets.
- `internal/agentbox/httpapi/server.go` (user credential routes after Phase 2) - provide purpose-specific creation without adding owner authority.
- `internal/agentbox/cli/bootstrap.go` (`runConnect`, `createTenantAPIKeyForProfile`, ChatGPT setup text) - extract reusable connection metadata and add Claude without preserving tenant terminology.
- `internal/agentbox/profiles/profiles.go` (`Profile` final shape) - the generated local prompt must write a valid current profile.

**Dashboard:**

- `app/keys/keys-view.tsx` (one-time secret display and MCP URL construction) - reuse the secure reveal pattern.
- `app/login/login-view.tsx` and the Phase 4 registration redirect - establish onboarding entry/resume behavior.
- `app/components/copy-button.tsx` (read in full) - copy URLs and the generated local-agent prompt.
- `npm/agentbox/package.json` and `npm/agentbox/README.md` (install command/profile locations) - keep generated instructions compatible with the published package.

**CLI validation:**

- `internal/agentbox/cli/cli.go` (`runList`, `runtimeConfig`, `request`) - the generated prompt's final test must exercise the real list path.
- `internal/agentbox/cli/cli_test.go` (`TestCLIConnectChatGPTPrintsMCPInstructions`, profile tests) - extend for Claude and generated local setup.

#### What to do

Add `/onboarding` as a resumable authenticated experience and make it the first destination after invite registration. Present three numbered cards in the approved order: ChatGPT, Claude, Local coding agent. Record completion state without requiring all steps before the user can enter the inbox, and make onboarding available later from settings.

Each card creates a credential only on explicit action. ChatGPT and Claude receive separate purpose labels, secrets, MCP URLs, attribution, and revocation. The full URL is shown only in the creation response; revisiting a completed card shows metadata and a rotate/recreate action, not the old secret.

The local card creates one machine credential and returns a copyable prompt written to a local coding agent. It must explain AgentBox briefly, include the dashboard/base URL and secret, install `@amxv/agentbox`, save an active profile using the current CLI contract, run `agentbox list`, and instruct the agent to report success/failure. Avoid embedding the deployment owner secret or any tenant identifier.

Keep browser-assisted `agentbox login` as an alternative for later machines, but do not make the onboarding prompt depend on a browser callback.

Raycast setup remains absent.

#### Validation strategy

- A new user must land on onboarding, skip it if desired, and resume it later without duplicate credentials.
- Creating ChatGPT and Claude connections must return distinct secrets/URLs and produce distinct actor attribution in a thread.
- Revisiting a completed card must never retrieve the original secret.
- A generated local prompt executed in a clean temporary config environment must install/use the CLI profile shape and successfully list that user's accessible threads.
- Rotating one credential must invalidate only that credential and leave the other onboarding connections functional.

#### What must not break

- General credential create/list/revoke remains available from settings.
- MCP URLs retain query-string key authentication.
- Users who choose no connection can still use the web dashboard normally.

### Phase 7: Add team sharing and widen the centralized effective-access rule

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 3.2, 3.4, 8, 9.3, and 10) - full participant permissions, multi-team sharing, self-access removal, and unified accessibility.

**Access foundation from Phase 3:**

- The `ThreadAccess` repository/service contracts and their SQL tests - widen the one existing predicate; do not create parallel owner/team paths.
- The final thread, message, asset, and pending-upload repository modules - apply team access to every operation already covered by private ownership.

**Team contracts from Phase 5:**

- `Team`, `TeamMembership`, owner administration, and normal available-team methods - team additions must validate current caller membership.

**HTTP and web:**

- `internal/agentbox/httpapi/server.go` thread subroute dispatch pattern (`threadSubroutes`) or its refactored replacement - add one visibility endpoint shared by clients.
- `app/threads/[threadId]/thread-view.tsx` (load, header, reply, and state) - add visibility controls without duplicating thread fetching.
- `app/components/agentbox-write.ts` - add a typed visibility request helper using the same endpoint as MCP/CLI later.
- `app/api/threads/[threadId]/route.ts` and sibling thin proxies - add the corresponding visibility proxy route.

**Tests:**

- PostgreSQL user-private access tests from Phase 3 - extend every path to team access and membership removal.
- HTTP direct-upload/finalize/asset tests - prove team participants receive exactly the same capabilities.

#### What to do

Add `thread_team_shares` and widen `ThreadAccess` to owner OR active team membership. Apply the widened predicate inside list, search, get, post, direct uploads, pending-upload finalization, and asset signing. Preserve SQL-side filtering and indexed query plans; do not fetch the user's teams once per thread.

Add one atomic backend visibility operation and web endpoint. A read-only call returns owner metadata, current team shares, public state placeholder, and the caller's available teams. Mutation supports adding and removing several teams in one transaction. Additions validate that the acting user is currently a member of each target team. Any currently authorized participant may remove any existing share. Repeated adds/removes are idempotent.

The authorization check and mutation must occur in one transaction to avoid a time-of-check/time-of-use race. If the caller removes their final team-derived access, return the committed result and deny subsequent reads.

Add the dashboard visibility control to every normal accessible thread page. It displays Private when no shares exist, lists current shares, allows eligible additions/removals, and warns when the selected removal will likely remove the caller's own access. Public controls remain disabled/absent until Phase 8.

#### Validation strategy

- A thread owned by User A and shared with Team ABC must become visible and writable to B and C on browser, API, MCP, and CLI list/get/post/upload/download paths, while D remains denied.
- The same thread shared with ABC and ADE must appear once in A/D's unified list, not once per matching team.
- Adding a team the caller does not belong to must fail atomically without applying other requested additions/removals.
- Removing the caller's final share must return success for the mutation and make the next read fail.
- Removing a user from a team must immediately remove access that depended only on that membership while preserving messages and shares.
- Search snippets and attachment IDs from inaccessible team threads must not leak.

#### What must not break

- Thread owners always retain access.
- Existing private threads remain private until explicitly shared.
- Team sharing never copies or rewrites threads, messages, attachments, or R2 objects.
- API-key scopes continue to apply after team qualification.

### Phase 8: Add revocable public sharing and the public thread page

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 3.3, 9.3, 14, and 16) - public URL, live rendering, attachment security, regeneration, and concurrency.

**Visibility and access:**

- The atomic visibility operation from Phase 7 - extend the same transaction/result rather than adding a separate publish service.
- The `ThreadAccess` and asset-signing boundaries from Phases 3 and 7 - public access is a separate token path scoped to one thread, not a fake user.

**Rendering:**

- `app/threads/[threadId]/message-content.tsx` (read in full) - reuse plain/Markdown selection and large-message behavior.
- `app/threads/[threadId]/markdown-message.tsx` (`MarkdownMessage`, `CodeBlock`, `Table`) - reuse the exact GFM, highlighting, and Mermaid renderer.
- `app/threads/[threadId]/thread-view.tsx` (message/attachment card rendering) - extract reusable presentation without importing authenticated navigation or reply controls.

**Routes and headers:**

- `app/threads/[threadId]/page.tsx` and `app/layout.tsx` - follow Next.js page/metadata patterns.
- `app/api/_proxy/proxy.ts` and current dynamic API route files - public API proxy must forward no user credential and rely only on the share token.
- `internal/agentbox/httpapi/server.go` (`withViewerAssetURLs`, error mapping) - create a safe public DTO rather than returning internal thread types wholesale.

**R2:**

- `internal/agentbox/assets/assets.go` (`CreateSignedAssetDownloadURL`) - preserve short expiry and attachment disposition.

#### What to do

Add `public_thread_shares` with a high-entropy token and a uniqueness constraint guaranteeing one active share per thread. Extend the Phase 7 visibility operation with publish, unpublish, and regenerate behavior. Concurrent publish requests must converge on one active URL; regeneration atomically invalidates the old token.

Add public backend read and attachment-signing endpoints that resolve exactly one active token and return a deliberately safe DTO: title, messages, immutable safe attribution snapshots, timestamps, non-purged attachment metadata, and short-lived preview/download URLs. Never expose emails, team memberships, user IDs, credential IDs, storage keys, internal owner controls, or other threads.

Add `/share/[token]` on the Next.js dashboard origin. It is read-only, live on refresh, visually reuses the current Markdown/message components, excludes authenticated navigation/composer, and emits `noindex` metadata. Public missing/revoked tokens use a non-disclosing not-found state.

Add publish/unpublish/copy/regenerate controls to the Phase 7 visibility UI. Public share state does not grant the acting user access; normal authorization remains owner/team-based.

#### Validation strategy

- Publishing a thread must create one opaque URL; anonymous fetch renders Markdown, code, tables, Mermaid, author labels, and attachment previews.
- A message posted after publishing must appear on refresh without regenerating the URL.
- Unpublish and regenerate must immediately invalidate the prior token.
- Concurrent publish/regenerate tests must leave one active share and no ambiguous token state.
- A valid token for Thread A must never sign or reveal an asset from Thread B, even when the asset ID is supplied directly.
- Public HTML/API responses must be checked for absence of email, stable user/credential IDs, storage keys, team names unless intentionally part of thread content, and dashboard mutation controls.

#### What must not break

- Public viewers cannot post, upload, or mutate visibility.
- Normal private/team access is unchanged by enabling or disabling a public link.
- R2 remains private; direct `public_url` fields do not bypass the public token check.

### Phase 9: Expose the one visibility operation through MCP and CLI

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 9.1 and 9.2) - exact tool, input, output, command, and flags.

**MCP:**

- `internal/agentbox/mcpserver/mcpserver.go` (`build`, `objectSchema`, `annotations`, existing handlers) - add exactly one tool using existing structured-result/error conventions.
- `internal/agentbox/mcpserver/mcpserver_test.go` (`TestToolsExposeMetadataAndAnnotations`, `TestStreamableHTTPCallTool`) - update tool inventory and end-to-end call coverage.

**CLI:**

- `internal/agentbox/cli/cli.go` (`run`, `printTopLevelHelp`, `printCommandHelp`, `request`, command output patterns) - add one command without visibility flags on create/post.
- `internal/agentbox/cli/cli_test.go` (`TestCLIHelpOutput`, `TestCLIProfilesAndThreadCommands`) - add text/JSON behavior and repeated-flag coverage.
- The visibility API DTO from Phases 7-8 - MCP, CLI, and web must share this contract rather than define translations independently.

#### What to do

Add exactly one MCP tool named `manage_thread_visibility`. Its schema contains `thread_id`, optional `add_teams`, `remove_teams`, `public`, and `regenerate_public_link`. Calling with only the thread ID performs a read. The result contains owner-safe metadata, current team shares, active public URL/state, and the caller's available teams. Use existing MCP structured JSON text plus `StructuredContent`, and choose annotations that accurately signal mutation when mutation fields are supplied; do not split the operation into several tools.

Add `agentbox visibility <thread-id>` with repeatable `--share-team` and `--unshare-team`, plus `--publish`, `--unpublish`, and `--regenerate-public-link`. A single invocation maps to one atomic backend request. Without mutation flags it prints current visibility and available teams; `--json` returns the backend shape.

Keep `create_thread`, `agentbox create`, `post_message`, and `agentbox post` free of visibility flags. Update user-facing help and npm CLI documentation.

#### Validation strategy

- MCP tool metadata tests must contain the one new tool and prove `create_thread` has no visibility properties.
- MCP and CLI must both read visibility and perform a combined add-team/remove-team/publish mutation against the same HTTP/service operation.
- Repeated team flags and conflicting `--publish` / `--unpublish` input must have deterministic validation.
- An MCP/CLI user lacking thread access must receive the normal non-disclosing error.
- JSON output must include available team slugs so an agent can discover valid targets without another tool.

#### What must not break

- Existing MCP tool names and result/error encoding remain stable.
- Existing CLI list/search/create/get/download/post behavior remains stable apart from the new effective-access set.
- The new command must never require the deployment owner secret.

### Phase 10: Complete unified inbox filters and user/actor attribution UX

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 4 and 10) - approved attribution examples and filters.

**Backend query DTOs:**

- `internal/agentbox/types/types.go` (`Thread`, `SearchThreadResult`, `Message`, visibility DTOs after Phase 9) - expose filter/attribution metadata without leaking internal IDs unnecessarily.
- The SQL list/search methods implementing `ThreadAccess` - add indexed filters to the existing query rather than client-side filtering.

**Dashboard:**

- `app/threads/inbox-view.tsx` (thread type, `loadThreads`, list rendering) - add All, Private, Shared with me, per-team, and Public filters.
- `app/threads/[threadId]/thread-view.tsx` (message author rendering and visibility state) - render `User · Actor` and safe legacy fallback.
- `app/components/session.ts` - avoid using session actor name as the only display source.

**CLI/MCP output consumers:**

- `internal/agentbox/cli/cli.go` (`thread`, `message`, `printThread`, list/search output) - preserve concise output while showing useful attribution/visibility metadata.
- `internal/agentbox/mcpserver/mcpserver.go` list/get result shapes - include the backend DTO unchanged where possible.

#### What to do

Add explicit visibility summary metadata to thread list/search DTOs: owner/private status for the caller, shared-team summaries, and public state. Implement server-side filters for All, Private, Shared with me, one team, and Public. Keep the default as All and retain bounded query limits. Ensure a thread matching several team memberships is returned once.

Update dashboard thread cards and filter controls. Update message rendering to show the stable user snapshot and actor snapshot, for example `Ashray · ChatGPT`, while falling back to the legacy `author` string when new references are null. The public page uses the same safe display label.

Update CLI/MCP DTO consumers so attribution and visibility metadata are available without breaking plain default output. Avoid exposing emails or secrets.

#### Validation strategy

- A fixture containing owned private, team-shared, multi-team, and public threads must produce correct non-duplicated results for every filter.
- Filter tests must execute SQL predicates and remain bounded; a large fixture must not trigger one membership query per row.
- Browser, CLI JSON, and MCP `get_thread` must distinguish Web dashboard, ChatGPT, Claude, and Local CLI contributions by one user.
- Legacy rows with only `author` must render exactly that snapshot and not appear blank.

#### What must not break

- Default list/search remains the complete effective accessible set.
- Public status does not by itself make a thread appear in another authenticated user's inbox.
- Filtering never reveals inaccessible team names or thread counts.

### Phase 11: Add owner user administration, credential metadata/revocation, and disablement

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 2.2, 7, 11, and 12) - owner authority, metadata-only secrets, and disable-not-delete lifecycle.

**Identity/auth:**

- User/session/credential repository and service modules after Phase 2 - add owner methods separately from normal user credential methods.
- `internal/agentbox/httpapi/server.go` owner-session guard introduced in Phase 4 - reuse it; do not accept owner API keys.
- `internal/agentbox/service/service.go` authentication methods - disabled users must stop producing auth contexts immediately.

**Teams/content:**

- Team membership methods from Phase 5 - disablement removes/deactivates memberships transactionally.
- Thread access tests from Phase 7 - prove team access disappears while owned/shared content remains.

**Dashboard:**

- Owner users/invitations page from Phase 4 and team administration from Phase 5 - extend the existing owner area.
- `app/keys/keys-view.tsx` - normal user surface remains self-only; owner-wide metadata belongs in owner pages.

#### What to do

Add owner-session-only APIs and dashboard views for user status and credential metadata: label, purpose, token prefix/masked value, created/last-used/revoked timestamps. The owner may revoke any credential but never retrieve a secret.

Implement user disablement as one deliberate service operation. It marks the user disabled, revokes every active session and credential, and removes/deactivates every team membership. It preserves user, thread ownership, messages, attachments, shares, and attribution. The permanent owner cannot be disabled.

Authentication must check active user state on both session and API-key resolution. Existing authenticated requests already in flight may finish, but subsequent authentication fails. Login returns the same generic invalid-login behavior rather than revealing disabled status.

Team-shared threads owned by the disabled user remain accessible to still-qualified team members. Private owned threads remain inaccessible to ordinary users and available only through the later owner content viewer.

#### Validation strategy

- The owner browser session must list every user's credential metadata and revoke one without seeing its secret.
- A normal user and an owner API key must be unable to call owner-user administration endpoints.
- Disablement must invalidate all browser sessions/API keys, remove team-derived access, and prevent login in one committed operation.
- Team members must retain access to a disabled owner's already team-shared thread; private threads remain hidden.
- Owner-disable attempts must fail without altering sessions or memberships.

#### What must not break

- Disabling never deletes or transfers thread/message/attachment ownership.
- Historical attribution continues to resolve to the disabled user snapshot.
- Normal users continue to manage only their own credentials.

### Phase 12: Add idempotent disabled-user attachment purge and tombstones

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Section 12 and error/concurrency requirements in Section 16) - purge scope, tombstones, and retry safety.

**Asset storage:**

- `internal/agentbox/assets/assets.go` (`AssetStore`, `R2Store`) - add delete/head behavior with exact-key operations.
- `internal/agentbox/assets/fake.go` - simulate present/missing/deleted objects and capture delete calls.
- Asset repository/types after Phase 3 (`uploader_user_id`, purge fields) - select by stable uploader identity, not thread owner or author string.

**Owner administration:**

- Disablement service/HTTP/dashboard from Phase 11 - expose purge only for disabled users and keep it separate from disable.
- `app/threads/[threadId]/thread-view.tsx` and public message renderer - render tombstones rather than broken links.

#### What to do

Extend `AssetStore` with the minimum object-maintenance operations required to delete exact keys and tolerate already-missing objects. Add purge metadata to asset records. Implement an owner-session-only purge operation that is allowed only for disabled target users and selects assets by stable uploader user ID.

For each selected asset, delete the exact R2 object and record a tombstone with purge timestamp and owner action metadata. Design the workflow so retries continue incomplete rows and treat already-missing objects as completed tombstones, while never deleting unrelated keys. For large user histories, process bounded batches and return progress rather than holding one database transaction open across every R2 network call.

Authenticated and public thread DTOs render `Attachment deleted by deployment owner`, retain filename/attribution, and omit download/preview URLs for tombstones.

#### Validation strategy

- A disabled user with attachments in owned and other users' shared threads must have only their uploaded objects deleted.
- Attachments uploaded by other users in the disabled user's threads must remain.
- Repeating a completed or partially completed purge must be safe and must not issue deletes for unrelated keys.
- A simulated R2 failure must leave resumable database state and an accurate progress/error result.
- Dashboard and public pages must render tombstones without broken signed URLs.

#### What must not break

- Threads, messages, filenames, attribution, and non-purged attachments remain intact.
- Purge cannot target an active user or the permanent owner.
- Object deletion never derives keys from filenames or prefixes; it uses stored exact `storage_key` values.

### Phase 13: Add the separate owner-only web content viewer

#### Files to read before starting

**Specification:**

- `docs/user-team-sharing-spec.md` (Sections 3.1 and 11) - web-only view-all scope and explicit exclusion from other surfaces.

**Authorization:**

- `OwnerWebContext` guard and owner routes from Phases 4, 5, and 11 - require an owner user session, not merely `AuthContext.IsOwner` on a generic caller.
- Normal `ThreadAccess` service/repository methods - owner viewer must not weaken or special-case them.

**Viewer/rendering:**

- `internal/agentbox/httpapi/server.go` legacy `viewerThreads`, `viewerThread`, and `withViewerAssetURLs` if still present - replace ambiguous viewer semantics with explicit owner-web methods.
- `app/threads/inbox-view.tsx`, `app/threads/[threadId]/thread-view.tsx`, and shared message components - reuse display components while keeping owner browsing visually distinct and read-only.
- Owner user/team dashboard modules - add navigation and user/team filters.

#### What to do

Add separate service/repository methods and HTTP routes for owner-wide thread listing, search, detail, and attachment signing. They accept only `OwnerWebContext`, which can be created only from a valid permanent-owner browser session. Do not add an API-key scope, query parameter, MCP tool, or CLI command for this capability.

Build a clearly labeled owner dashboard content viewer with filters by user/team and links to thread detail. It is read-only for thread content and visibility. Attachment previews/downloads use the owner-web signing path and still respect purge tombstones.

Keep normal `/api/threads`, MCP, CLI, and user dashboard routes on `ThreadAccess`. Even credentials owned by the deployment owner see only the owner's normal owned/team-shared set.

#### Validation strategy

- An owner browser session must browse private threads owned by another user and sign their non-purged attachments.
- The owner's API key, CLI profile, and MCP URL must fail to read that same private thread through normal endpoints.
- A normal user session and forged owner-like request fields must fail owner routes.
- Owner viewer actions must not post messages, upload, or change visibility.

#### What must not break

- Normal authorization code contains no owner bypass.
- Owner browsing does not expose its special URLs or data through public or normal user DTOs.
- Disabling users and purging attachments remains auditable in the viewer.

### Phase 14: Complete the code cutover, remove the tenant path, and prepare production rollout

#### Files to read before starting

**Specification and plan amendments:**

- `docs/user-team-sharing-spec.md` (Sections 13, 17, and 18) - final preservation and out-of-scope contract.
- This plan's `Amendments` section - incorporate every execution discovery before cutover.

**Migration and rollout tooling:**

- Canonical migrations and migration runner from Phase 1 - identify the point-of-no-return migration and exact rollback restoration process.
- Backup/preflight command and credential-free fixtures - prove counts, object coverage reporting, missing-object failure, and owner backfill behavior without production access.
- `cmd/api/main.go`, `cmd/migrate/main.go`, deployment config under `deploy/`, and `AGENTS.md` - update operational sequencing and smoke checks.

**Legacy paths to remove:**

- `internal/agentbox/types/types.go` (`DefaultTenantID`, `Tenant`, every `TenantID`/`TenantSlug` field).
- `internal/agentbox/service/service.go` legacy tenant provisioning, tenant helper functions, and any remaining tenant signatures.
- `internal/agentbox/db/repository.go` / migrations remaining tenant columns, indexes, tables, and predicates.
- `internal/agentbox/httpapi/server.go` `/api/admin/tenants`, `/api/admin/keys` compatibility routes, tenant admin helpers, and ambiguous legacy viewer routes.
- `internal/agentbox/cli/bootstrap.go` `runInit`, `runProvision tenant`, admin-key key creation, Raycast setup if it depends on old contracts.
- `internal/agentbox/cli/login.go` and `internal/agentbox/profiles/profiles.go` tenant response/profile fields.
- `app/login/login-view.tsx`, `app/keys/keys-view.tsx`, dashboard copy, README/docs, and tests containing tenant terminology.
- `internal/agentbox/config/config.go` `R2PublicBaseURL` and any obsolete auth/session compatibility config.

#### What to do

Complete the feature branch's final code cutover. Implement the final content migration, owner-bootstrap/recovery path, production preflight command, production runbook, rollback procedure, and every smoke-check command so they can be exercised against credential-free test infrastructure. Assign representative legacy fixtures to the permanent owner privately and prove preservation of IDs, order, attachment references, and author snapshots.

Delete all legacy tenant authorization and compatibility code from the feature branch once the replacement path is proven. Remove tenant selectors, tenant DTO fields, tenant provisioning/admin endpoints, `agentbox init`, `agentbox provision tenant`, admin-key key creation, old profile metadata, direct `R2_PUBLIC_BASE_URL` behavior, and tests/docs that assert the old model.

Retain `AGENTBOX_ADMIN_KEY` only if the final owner-bootstrap/recovery design still requires it; document its narrow role. End with one schema runner, one user/team authorization model, one set of public contracts, and a complete production procedure that Phase 15 can execute without inventing missing commands or decisions.

When this phase is complete, Zodex agents must update the progress ledger, mark Phases 1-14 `Complete`, leave Phase 15 `Reserved for local agent`, push the final code-complete checkpoint, and stop. They must not run the production backup, pause writes, migrate the live database, access live R2, deploy Vercel, or create real production credentials.

#### Validation strategy

- Credential-free tests on the feature branch must prove the final legacy-fixture migration, tenant removal, owner backfill, backup/preflight behavior, and generated cutover/rollback procedures.
- Every migrated fixture thread must appear as a private owner thread and remain readable with its attachment references intact.
- Legacy fixture API keys, sessions, and tenant-shaped CLI profiles must fail after the final migration; newly created user-scoped test credentials must work.
- Repository-wide searches and schema inspection must find no runtime tenant authorization fields/routes/queries or request-path `EnsureSchema` calls.
- The full Go suite, PostgreSQL migration/access suite, Next.js typecheck/lint/build, CLI builds, and credential-free smoke matrix must pass.
- A security review must explicitly exercise cross-user thread IDs, asset IDs, team removal, public token revocation, and owner API-key non-bypass.

#### What must not break

- No legacy fixture thread, message, attachment row, historical attribution snapshot, or stored R2 key may be lost.
- The branch must remain deployable at every commit and must be code-complete before the local agent begins Phase 15.
- The final system must not retain two authorization models.

### Phase 15: Credentialed local production backup, deployment, and cutover

**Execution environment: local machine with production PostgreSQL, Cloudflare R2, Vercel, and AgentBox credentials only. Zodex agents must not start this phase.**

#### Files to read before starting

**Authoritative scope and execution history:**

- `docs/user-team-sharing-spec.md` (read in full) - the production acceptance contract and non-negotiable preservation requirements.
- This blueprint (read in full, especially `Implementation Progress`, `Amendments`, Phase 1, and Phase 14) - confirm every code phase is complete and incorporate discoveries recorded by previous agents.
- The production runbook and rollback procedure committed by Phase 14 - use the exact reviewed sequence rather than improvising a new cutover.

**Credentialed commands and deployment:**

- Canonical migrations, preflight/backup tooling, owner bootstrap/recovery tooling, and smoke-check commands completed in Phases 1-14 - inspect their help and dry-run modes before using production credentials.
- `cmd/api/main.go`, `cmd/migrate/main.go`, deployment configuration under `deploy/`, and `AGENTS.md` - verify the exact backend/dashboard deployment order and required environment variables.
- Latest branch history and outgoing diff - ensure the local checkout includes the pushed code-complete Phase 14 checkpoint before any live action.

#### What to do

Run the production preflight and verified backup before applying any authorization migration. Store the PostgreSQL backup, R2 backup/inventory, manifests, and restoration instructions outside the deployment being migrated. Resolve every missing-object, orphan-row, or count mismatch before continuing.

Pause writes for the planned maintenance window. Bootstrap the permanent owner using the reviewed recovery path, apply the canonical migrations, assign every legacy thread privately to that owner, and verify IDs, timestamps, message order, bodies, content types, attachment rows, storage keys, object existence, and historical attribution against the pre-cutover manifests.

Deploy the completed Go backend and Next.js dashboard with the reviewed production configuration. Recreate the owner's browser account/session and separate ChatGPT, Claude, and local credentials. Existing sessions, API keys, and tenant-shaped CLI profiles are intentionally invalidated.

Exercise the full production smoke matrix: login, private thread creation, list/search/get/post, direct uploads, pending-upload finalization, downloads, invitation registration, zero-team users, overlapping teams, visibility changes, public links and revocation, onboarding, disablement, purge behavior, and owner-browser-only content access. Confirm that the owner's API keys, MCP URLs, and CLI credentials cannot use the web-only view-all capability.

Reopen writes only after every preservation and security check passes. If production execution exposes a code defect, make the narrowest correct fix on `feat/user-team-sharing`, update `Implementation Progress` and `Amendments`, run the relevant checks, commit, and push before resuming the runbook. When production verification is complete, mark Phase 15 `Complete` and push the final progress update to the same branch.

#### Validation strategy

- Pre- and post-cutover production manifests must match for threads, messages, asset rows, stable IDs, ordering, storage keys, and referenced R2 objects; any unexplained mismatch blocks reopening writes.
- Every migrated thread must be private to the permanent owner until explicitly shared and must remain readable with its attachments.
- Old API keys, sessions, and tenant-shaped CLI profiles must fail; newly created owner credentials must work with independent actor attribution.
- Production checks must prove cross-user privacy, overlapping team access, removal of access after membership/share changes, public-token revocation, disabled-user invalidation, and owner API-key non-bypass.
- Backend/dashboard health, logs, migration ledger, and R2 signing behavior must remain clean through the maintenance window and after writes reopen.

#### What must not break

- No production thread, message, attachment row, historical attribution snapshot, or referenced R2 object may be lost or silently reassigned.
- Writes must not reopen while backup, manifest, migration, security, or smoke checks are incomplete or failing.
- Do not force-push or bypass the shared progress ledger when production fixes are required; the final branch must contain the exact code and runbook state that reached production.

## Amendments

None yet.
