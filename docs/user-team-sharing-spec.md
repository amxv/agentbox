# AgentBox User, Team, and Thread Sharing Specification

Status: Approved product specification

Branch: `feat/user-team-sharing`

## 1. Product Summary

AgentBox is deployed once by a permanent deployment owner. One deployment consists of:

- one Go API and MCP backend;
- one Next.js dashboard;
- one PostgreSQL database;
- one Cloudflare R2 bucket;
- many registered users;
- many overlapping teams;
- private, team-shared, and publicly shared threads.

A deployment is an infrastructure and administration boundary. It is not a thread-visibility boundary.

A user represents one person together with all of that person's AgentBox clients and agents. ChatGPT, Claude, the web dashboard, a local CLI installation, and a future Raycast installation are credentials or actors belonging to that user; they are not separate AgentBox users.

Each user gets the same private AgentBox experience that the original single-player product provides. Threads are private by default and are visible only to the owning user and credentials that act for that user. A user may explicitly share a thread with one or more teams, or publish it through an opaque public read-only URL.

## 2. Goals

The implementation must provide:

1. One deployment with multiple user accounts.
2. One permanent deployment owner.
3. Invite-only user registration.
4. Per-user browser sessions and credentials.
5. Private-by-default user-owned threads.
6. Many-to-many teams and memberships.
7. Canonical threads shared with one or more teams without copying data.
8. Full participation rights for every user who can access a non-public thread.
9. Public read-only thread links with rendered Markdown and attachments.
10. Consistent authorization across the web API, MCP, CLI, uploads, downloads, and search.
11. Agent-level attribution within a user account.
12. Preservation of all existing threads, messages, attachment metadata, and R2 objects during migration.

## 3. Non-Goals for the Initial Release

The initial implementation does not require:

- granular per-thread read/write roles;
- per-message visibility;
- guest accounts with write access;
- public comments or public posting;
- ownership transfer between users;
- deletion or editing of individual messages;
- a published Raycast extension;
- multiple deployment owners;
- OAuth or third-party identity providers;
- team-scoped credentials;
- cross-deployment federation;
- billing, quotas, or usage metering;
- live realtime updates beyond normal refresh/revalidation behavior.

## 4. Core Concepts

### 4.1 Deployment

A deployment is one self-hosted AgentBox installation and all infrastructure behind it.

The deployment has exactly one permanent owner. The owner is the only user who can:

- create invitations;
- revoke invitations;
- browse and manage all users;
- disable users;
- create, rename, archive, and delete teams;
- add or remove users from teams;
- inspect credential metadata for every user;
- revoke another user's credential;
- view all deployment content through owner-only web dashboard routes;
- purge attachment objects associated with a disabled user.

The deployment owner cannot retrieve an existing credential secret. Secrets are shown only when created.

Owner-only view-all-content access is a web-dashboard administration feature. It must not be inherited by the owner's MCP URLs, CLI keys, Raycast keys, or ordinary API keys. Those credentials receive only the same content access as a normal user credential.

### 4.2 User

A user is one person and their collection of AgentBox surfaces.

A user owns:

- one account identity;
- password authentication;
- browser sessions;
- API credentials;
- private threads;
- messages and attachments created through their credentials;
- team memberships.

A user's email address is unique within the deployment.

A user may belong to zero, one, or many teams.

A disabled user cannot sign in or authenticate through any existing credential. Their sessions and credentials are revoked. Their threads, messages, attachments, authorship, and team contributions remain stored.

### 4.3 Credential / Actor

A credential authenticates one client acting for one user.

Initial recommended credential purposes are:

1. ChatGPT
2. Claude
3. Local

Additional credentials may be created later for another machine, agent, CI job, or future Raycast installation.

Each credential has:

- a stable ID;
- an owning user ID;
- a user-visible label;
- a purpose/type where useful;
- a secret shown once;
- a hashed secret stored in PostgreSQL;
- creation, last-used, revocation, and optional expiry metadata;
- scopes required by the client surface.

Credential labels are unique only within one user account. Different users may each have credentials named `chatgpt`, `claude`, or `local`.

Messages preserve both the human account and the acting credential when available. The dashboard should render attribution such as:

- `Ashray · ChatGPT`
- `Ashray · Claude`
- `Ashray · Local`
- `Ashray · Web`

A browser-authenticated message is attributed to the user and a built-in web actor rather than an API-key secret.

### 4.4 Team

A team is an admin-managed group of registered users in the same deployment.

Only the deployment owner can create teams or change membership.

A user may belong to multiple teams. Teams may overlap. A user may also belong to no teams.

Team membership does not automatically expose a user's private threads. A thread becomes visible to a team only when an authorized participant explicitly shares that thread with the team.

### 4.5 Thread

Every thread has exactly one owning user.

A newly created thread is private regardless of whether it was created through the dashboard, API, MCP, CLI, or another client.

The thread owner and every credential owned by that user can access the private thread.

A thread remains one canonical object when shared. Sharing never creates a copy.

### 4.6 Team Share

A thread may be shared with zero, one, or many teams.

A user can access a team-shared thread when they are an active member of at least one team that currently has access to the thread.

Every authenticated user who can access a thread has full thread participation rights. They may:

- read all messages;
- post messages;
- upload attachments;
- download attachments;
- share the thread with another team they belong to;
- remove any existing team share;
- enable public sharing;
- disable public sharing;
- regenerate the public link.

Adding a new team share is allowed only when the acting user is an active member of that target team. This prevents a participant from sharing into a team they cannot access.

Removing a team share is allowed to any authenticated participant who can currently access the thread, even when the participant is not a member of the team being removed.

When a team share is removed:

- members of that team immediately lose access unless another access path remains;
- existing messages and attachments remain in the canonical thread;
- previous authorship remains unchanged;
- no content is copied or deleted.

### 4.7 Public Share

A thread may have at most one active public share URL at a time.

The public URL uses a high-entropy opaque identifier, for example:

```text
https://agentbox.example.com/share/shr_<opaque-random-value>
```

The public page is:

- read-only;
- accessible without authentication;
- unlisted;
- marked `noindex` and `nofollow`;
- backed by the live canonical thread rather than a frozen snapshot;
- rendered using the existing Markdown, code, table, Mermaid, plain-text, and attachment UI;
- stripped of private dashboard navigation and management controls.

The public page displays:

- thread title;
- messages in order;
- safe author display labels;
- timestamps;
- rendered Markdown or plain text;
- attachment names, previews, and download actions.

It must not expose:

- user email addresses;
- API-key IDs or secrets;
- session IDs;
- internal authorization metadata;
- private inbox links;
- team membership lists;
- admin controls.

Public attachments are accessible through short-lived signed R2 download URLs created only after validating the active public share. R2 remains private.

Any authenticated participant with thread access may disable the public URL or regenerate it. Regeneration invalidates the previous URL immediately.

## 5. Authorization Rules

### 5.1 Effective Thread Access

An active user can access a thread when any of the following is true:

1. `thread.owner_user_id == user.id`; or
2. the user is an active member of a team with an active share for the thread.

Public URLs use a separate read-only authorization path based on the active public-share identifier.

The deployment owner does not bypass this rule through normal API, MCP, CLI, or user-facing dashboard endpoints.

### 5.2 Owner Web View-All

Owner-only administration endpoints may list and read every thread for debugging and deployment management.

These endpoints must require:

- a valid first-party browser session;
- the permanent owner role;
- the owner-only web route namespace.

Owner view-all must not be activated by API-key scopes and must not be exposed as an MCP tool or CLI command.

### 5.3 Centralized Enforcement

Authorization must be enforced below individual client adapters. HTTP, MCP, CLI, dashboard proxy routes, attachment signing, upload preparation, thread search, and thread listing must all call the same service/repository authorization boundary.

No adapter may authorize access based only on a thread ID, attachment ID, creator display name, credential label, or old tenant ID.

A missing or inaccessible thread should normally be returned as not found to avoid leaking its existence.

## 6. Inbox and Search Behavior

The default inbox and `list_threads` result contain every thread the current user can access:

- user-owned private threads;
- user-owned team-shared threads;
- threads owned by other users and shared with one of the current user's teams;
- publicly shared threads only when the user also has authenticated access through ownership/team membership. Public status alone does not add a thread to every authenticated inbox.

The dashboard should provide lightweight filters:

- All
- Private
- Shared with me
- Public
- one filter per team membership

Search uses the same effective-access set. A search must never return a title, snippet, count, attachment, or author from an inaccessible thread.

## 7. Thread Visibility Management Surface

Thread creation remains private and unchanged in spirit. Visibility is managed separately.

### 7.1 MCP

Add one MCP tool:

```text
manage_thread_visibility
```

It accepts a thread ID and any combination of:

- team IDs/slugs to add;
- team IDs/slugs to remove;
- `public: true` to enable public sharing;
- `public: false` to disable public sharing;
- `regenerate_public_link: true` to replace an active public URL.

It returns the complete resulting visibility state, including active team shares and the public path/URL when public sharing is active.

The existing `create_thread` tool always creates a private thread and does not accept visibility arguments.

### 7.2 CLI

Add one top-level visibility-management command:

```text
agentbox visibility <thread-id>
```

Supported behavior should include:

```bash
agentbox visibility <thread-id> --show
agentbox visibility <thread-id> --add-team <team-id-or-slug>
agentbox visibility <thread-id> --remove-team <team-id-or-slug>
agentbox visibility <thread-id> --public
agentbox visibility <thread-id> --unpublic
agentbox visibility <thread-id> --regenerate-public-link
```

Flags may be combined where unambiguous. JSON output must be available.

The CLI uses the selected user's credential and cannot invoke owner view-all behavior.

### 7.3 Web Dashboard

The authenticated thread page includes a visibility control that shows:

- Private/Shared/Public status;
- all teams currently sharing the thread;
- teams the current user belongs to and can add;
- the current public URL when active;
- copy, disable, and regenerate actions.

Every authenticated participant with thread access sees and can use these controls.

## 8. Invitations and Registration

Only the deployment owner can create invitation links.

An invitation is:

- cryptographically random;
- single-use;
- revocable before use;
- expiring at an owner-selected time;
- not required to be pre-bound to an email address;
- optionally associated with one or more initial team memberships.

Opening a valid invitation displays a registration page. The invited person enters:

- display name;
- email address;
- password.

On successful registration:

1. a user account is created;
2. the invitation is consumed;
3. requested initial team memberships are created;
4. a browser session is established;
5. the user appears immediately in the owner admin dashboard;
6. the user is redirected to onboarding.

The owner may also create an invitation with no initial team. The user then receives a fully private AgentBox account and can still create public links.

Expired, revoked, consumed, or unknown invitation links must not reveal account or team information.

## 9. Onboarding

A newly registered user enters an onboarding flow before or alongside the normal inbox.

The recommended setup is numbered and encourages completion of all three integrations:

1. Connect ChatGPT
2. Connect Claude
3. Connect Local Agent

### 9.1 ChatGPT

The user clicks to create a dedicated ChatGPT credential. AgentBox shows the secret only once and presents a full remote MCP URL with the key in the query string:

```text
https://deployment.example.com/api/mcp?key=<chatgpt-secret>
```

The URL is configured as a no-auth custom MCP connection because the credential is embedded in the URL.

### 9.2 Claude

The user clicks to create a separate Claude credential and receives a separate MCP URL. ChatGPT and Claude never share a credential because messages must retain independent attribution and revocation.

### 9.3 Local Agent

The user clicks to create one local-machine credential. The first version assumes one machine and creates one key for that machine.

AgentBox produces a pasteable prompt written for a local coding agent such as Codex or Claude Code. The prompt must:

- briefly explain AgentBox as a shared thread inbox between the user and their agents;
- include the deployment base URL;
- include the newly generated local credential secret;
- install the CLI with `npm install -g @amxv/agentbox`;
- save a named CLI profile using the provided URL and key;
- run `agentbox doctor` where appropriate;
- run `agentbox list` as the final connection test;
- tell the local agent not to echo or commit the secret.

Additional machine credentials can be created later from settings.

Credential creation is click-to-create. Registration does not silently create unused credentials.

## 10. Credential Management

Users can:

- list their own credentials;
- create a credential;
- rotate/recreate a credential;
- revoke their own credential;
- inspect its label, purpose, created time, last-used time, and masked value.

Users cannot see another user's credential metadata.

The deployment owner can inspect metadata for every credential and revoke any credential, but cannot retrieve its secret.

Credential names are scoped to the owning user.

Disabling a user revokes all active sessions and credentials immediately.

## 11. User and Team Administration

The owner web dashboard includes:

### Users

- active and disabled users;
- display name and email;
- signup/invite time;
- team memberships;
- session/credential metadata;
- last activity where available;
- disable action;
- attachment purge action for disabled users.

### Invitations

- create invite;
- choose expiration;
- optionally select initial teams;
- copy invite URL;
- see created, expires, consumed, or revoked status;
- revoke unused invite.

### Teams

- create team;
- rename team;
- archive/delete team;
- list members;
- add existing users;
- remove members.

Deleting or archiving a team removes that team's access path from shared threads but does not delete thread content.

## 12. Disabled Users and Attachment Purging

Disabling a user:

- prevents password login;
- revokes browser sessions;
- revokes API credentials;
- removes active team memberships;
- retains all owned threads, messages, and attachments;
- retains all contributions and attribution in other users' team-shared threads;
- does not transfer ownership.

The owner may later purge attachment objects associated with the disabled user's account to save storage.

Attachment purging must be a separate explicit destructive action with confirmation and a result summary. It may delete:

- R2 objects created by the disabled user or their credentials;
- corresponding attachment database rows or mark them purged;
- pending uploads owned by the user.

It must not delete thread or message text. The UI should preserve a tombstone such as `Attachment removed by deployment owner` when an attachment referenced by a retained message has been purged.

Ownership transfer is not supported.

## 13. Data Model

The intended logical model is:

### `users`

- `id`
- `email` (deployment-unique, case-insensitive)
- `display_name`
- `password_hash`
- `role` (`owner` or `member`)
- `created_at`
- `updated_at`
- `disabled_at`

Exactly one active user has role `owner`.

### `user_sessions`

- `id`
- `user_id`
- `secret_hash`
- `created_at`
- `last_used_at`
- `expires_at`
- `revoked_at`

### `api_keys`

- `id`
- `user_id`
- `name`
- `purpose`
- `token_prefix`
- `token_hash`
- `scopes`
- `created_at`
- `updated_at`
- `last_used_at`
- `expires_at`
- `revoked_at`

Active key names are unique by `(user_id, lower(name))`.

### `teams`

- `id`
- `slug`
- `name`
- `created_by_user_id`
- `created_at`
- `updated_at`
- `archived_at`

### `team_memberships`

- `team_id`
- `user_id`
- `added_by_user_id`
- `created_at`

Primary key: `(team_id, user_id)`.

### `invitations`

- `id`
- `token_hash`
- `token_prefix`
- `created_by_user_id`
- `created_at`
- `expires_at`
- `consumed_at`
- `consumed_by_user_id`
- `revoked_at`

### `invitation_teams`

- `invitation_id`
- `team_id`

Primary key: `(invitation_id, team_id)`.

### `threads`

Existing thread fields plus:

- `owner_user_id` (required)

Legacy tenant columns may remain temporarily during migration but are not part of the final authorization model.

### `messages`

Existing message fields plus stable attribution:

- `created_by_user_id`
- `created_by_key_id`
- built-in web actor metadata when no API key is used.

### `assets`

Existing asset fields plus:

- `created_by_user_id`
- `created_by_key_id`
- optional `purged_at`
- optional `purged_by_user_id`

### `thread_team_shares`

- `thread_id`
- `team_id`
- `shared_by_user_id`
- `created_at`

Primary key: `(thread_id, team_id)`.

### `thread_public_shares`

- `id` (opaque public identifier)
- `thread_id`
- `created_by_user_id`
- `created_at`
- `revoked_at`

Only one active public share may exist per thread.

## 14. API Surface

The exact endpoint naming may evolve, but the backend must support these capabilities:

### Authenticated user routes

- session login/logout/me;
- list/create/read threads using effective access;
- post messages and upload/download attachments using effective access;
- read/update thread visibility;
- list the current user's teams;
- list/create/revoke the current user's credentials;
- invitation registration;
- onboarding state/data.

### Public routes

- fetch public thread by opaque share ID;
- fetch short-lived public attachment URL after validating the share.

### Owner-only browser-session routes

- users and user status;
- invitations;
- teams and memberships;
- all credential metadata and revocation;
- view-all thread listing/detail;
- disabled-user attachment purge.

Owner-only administration routes must reject API-key authentication.

## 15. Migration and Data Preservation

No existing thread, message, attachment metadata, or attachment object may be lost.

Before applying the new authorization migration, production content must be backed up and verified.

The backup must include:

1. PostgreSQL rows for `threads`, `messages`, `assets`, and relevant upload/object metadata.
2. Every R2 object referenced by an attachment row.
3. A manifest containing row counts, object counts, object sizes, and checksums where feasible.
4. A restore procedure tested against a non-production database and bucket or prefix.

Accounts, sessions, API keys, login codes, and the half-built tenant model do not need to be preserved as product identities. They may be reset.

Migration behavior:

1. Create or provision the new permanent owner account.
2. Assign every existing thread to that owner.
3. Preserve all thread IDs, message IDs, asset IDs, timestamps, bodies, content types, storage keys, filenames, MIME types, and sizes.
4. Preserve current author strings for display/history.
5. Map historical stable creator IDs when possible; otherwise retain legacy attribution without inventing a new agent identity.
6. Keep existing R2 object keys valid. Objects do not need to be moved merely to satisfy a new prefix convention.
7. Reissue all user-facing credentials after migration.
8. Validate counts and sample attachment downloads before declaring the migration complete.

The migration must be staged and reversible until validation is complete. Destructive cleanup of legacy auth/tenant columns happens only after the new system is operating correctly.

## 16. Security Requirements

- Store passwords using the existing strong password hash mechanism.
- Store session, API-key, invitation, CLI-login, and similar secrets only as cryptographic hashes.
- Use high-entropy opaque public share IDs.
- Keep R2 private and issue short-lived signed URLs.
- Validate thread access before creating upload or download URLs.
- Validate public-share activity before signing public attachment URLs.
- Prevent disabled users from authenticating through sessions or keys.
- Do not put user emails or private metadata into public pages.
- Mark public pages `noindex` and `nofollow`.
- Prevent normal API keys from calling owner administration or view-all routes.
- Return not-found semantics for inaccessible private resources where appropriate.
- Log administrative destructive actions without logging secrets.

## 17. Testing Requirements

The automated suite must cover:

- private-thread isolation between users;
- access inherited through each overlapping team membership;
- one thread shared with multiple teams;
- duplicate membership and share idempotency;
- removal of one access path while another remains;
- immediate loss of access after the final team share is removed;
- full posting/upload rights for a team participant;
- inability to share into a team the actor is not a member of;
- visibility management through API, MCP, and CLI;
- public enable, disable, and regenerate behavior;
- old public URL invalidation after regeneration;
- public Markdown rendering and attachment signing;
- private attachment download denial;
- search/list isolation;
- credentials scoped to their owning user;
- same credential label used by different users;
- disabled-user authentication failure;
- owner web view-all success;
- owner API-key/MCP/CLI view-all denial;
- invite expiry, revocation, single use, and initial teams;
- content backup/restore count and checksum verification;
- legacy content migration to the permanent owner.

## 18. Rollout Sequence

1. Add and test content backup/export tooling.
2. Back up production PostgreSQL content and R2 objects.
3. Verify restore in a non-production environment.
4. Add the new schema alongside legacy fields.
5. Introduce user-owned credentials and centralized effective-access queries.
6. Backfill existing threads to the new permanent owner.
7. Add team administration and membership.
8. Add thread visibility management to API, MCP, CLI, and web.
9. Add public read-only routes and pages.
10. Add invitation registration and onboarding.
11. Add owner view-all and administration dashboard.
12. Add disable and attachment-purge workflows.
13. Reissue production credentials and validate all client surfaces.
14. Remove or quarantine obsolete tenant/auth paths only after production validation.

## 19. Approved Product Decisions

The following decisions are final for this implementation:

- Existing content must be preserved and backed up.
- Existing accounts and credentials may be reset.
- There is one permanent deployment owner.
- Only the owner manages invitations, users, teams, and memberships.
- Owner view-all exists only in the authenticated web administration surface.
- Users may belong to multiple or zero teams.
- Invitations may include zero or more initial team memberships.
- Threads are private by default.
- Thread creation does not accept visibility parameters.
- Visibility is managed through one dedicated MCP tool and one dedicated CLI command.
- Any authenticated participant with thread access has full thread participation and visibility-management rights.
- A thread may be shared with multiple teams.
- Removing access does not delete or copy prior contributions.
- Public pages are live, read-only, revocable, opaque, rendered, and include attachment access.
- ChatGPT, Claude, and Local use separate credentials for attribution and revocation.
- Onboarding credentials are click-to-create.
- The initial local setup assumes one machine and one key.
- Users manage only their own credential secrets and metadata.
- The owner may inspect all credential metadata and revoke credentials, but cannot recover secrets.
- Users are disabled rather than automatically deleted.
- Ownership transfer is not supported.
- Attachment purging for disabled users is a separate owner-only destructive action.
- Raycast onboarding is deferred.
