# AgentBox User, Team, and Public Sharing Specification

Status: Approved for implementation planning

Date: 2026-08-01

Target branch: `feat/user-team-sharing`

## 1. Purpose

AgentBox must evolve from a deployment-wide shared inbox into a multi-user deployment where every person has a private AgentBox experience, can connect several independently attributed agent surfaces, can collaborate through overlapping teams, and can expose selected threads through public read-only URLs.

The deployment remains self-hosted as one Go backend, one Next.js dashboard, one PostgreSQL database, and one Cloudflare R2 bucket. The deployment is the infrastructure and security boundary. It is not the normal content-visibility boundary.

## 2. Product Model

### 2.1 Deployment

One AgentBox installation is one deployment. It owns:

- the Go API and MCP backend;
- the Next.js dashboard and public thread pages;
- one PostgreSQL database;
- one private Cloudflare R2 bucket;
- one permanent deployment owner;
- all registered users, credentials, teams, threads, messages, and attachments.

The existing tenant concept is not part of the final product model. Tenant selectors, tenant-scoped logins, tenant-scoped inboxes, and tenant-scoped credential management must be removed from the finished system.

### 2.2 Deployment owner

There is exactly one permanent deployment owner.

The owner is the only person who can:

- create and revoke signup invitations;
- choose invitation expiry;
- optionally attach initial team memberships to an invitation;
- create, rename, and manage teams;
- add and remove users from teams;
- view all registered users and their status;
- disable users;
- inspect credential metadata for every user;
- revoke any user credential;
- access the owner-only web content viewer;
- permanently purge attachments uploaded by a disabled user.

The deployment owner cannot be demoted, disabled, deleted, or transferred through normal product flows. Additional deployment administrators are out of scope.

### 2.3 User

A user represents one person and all AgentBox surfaces acting for that person.

A user owns:

- their browser account and sessions;
- their private threads;
- their ChatGPT credential and MCP URL;
- their Claude credential and MCP URL;
- their local-machine CLI credential;
- any additional credentials they create later;
- memberships in zero or more teams.

Users are not duplicated per team. One user may belong to several overlapping teams or to no teams at all.

Email addresses are unique within the deployment.

### 2.4 Credential and actor

ChatGPT, Claude, a local CLI installation, and future Raycast installations are credentials acting on behalf of a user. They are not separate users.

Each credential must have:

- one owning user;
- a human-readable label such as `chatgpt`, `claude`, or `local-macbook`;
- a one-time-visible secret;
- independent creation, rotation, revocation, and last-used metadata;
- an attribution identity preserved on every message it creates.

Credential names are unique only within one user's account. Different users may each have credentials named `chatgpt`, `claude`, or `local`.

Browser-authored messages act directly as the signed-in user and display an actor label such as `Web dashboard`. They do not require a hidden browser API key.

### 2.5 Team

A team is an owner-managed, many-to-many group of registered users inside one deployment.

A team has:

- a stable ID;
- a deployment-wide unique slug suitable for CLI and MCP use;
- a display name;
- zero or more active members.

Only the deployment owner manages teams and memberships. Ordinary users cannot create teams, invite users, or alter team membership.

### 2.6 Thread

Every thread has exactly one owning user.

A newly created thread is always private, regardless of whether it was created through the web dashboard, API, MCP, or CLI.

Thread creation must not accept team-sharing or public-sharing parameters. Visibility is changed only through the dedicated thread-visibility operation after creation.

Messages are append-only. A thread remains one canonical object when it is shared; sharing never copies a thread, message, or attachment.

## 3. Authorization and Visibility

### 3.1 Private access

A private thread is accessible only to:

- its owning user;
- browser sessions for that user;
- active credentials owned by that user;
- the deployment owner through the owner-only web content viewer.

The owner's web-only view-all capability must not be inherited by the owner's API keys, CLI profiles, MCP URLs, or other non-web credentials.

### 3.2 Team access

A thread may be shared with any number of teams.

An active user can access a team-shared thread when the user is an active member of at least one team currently attached to the thread. Every active credential owned by that user receives the same effective access.

Anyone who currently has normal access to a thread has full collaboration permissions on that thread. They may:

- read all messages and attachment metadata;
- post messages;
- upload attachments;
- share the thread with another team they currently belong to;
- remove any existing team share;
- enable public sharing;
- disable public sharing;
- regenerate the public URL.

Adding a team share is permitted only when the acting user is currently a member of the target team. Removing a share is permitted for any currently authorized participant, even if that operation removes the participant's own last access path. The operation succeeds atomically; subsequent requests must then be denied if the user no longer qualifies for access.

The thread owner always retains access, regardless of team shares.

### 3.3 Public access

A thread may have at most one active public share URL at a time.

The public URL must:

- contain a cryptographically random, unguessable token;
- use the dashboard origin, for example `/share/<opaque-token>`;
- be read-only;
- be unlisted and marked `noindex`;
- show the live thread, including messages added after publishing;
- render Markdown, GitHub-flavored tables, fenced code, syntax highlighting, and Mermaid using the existing dashboard renderer;
- display safe author attribution and timestamps;
- show attachment previews where supported;
- allow public attachment downloads through short-lived signed R2 URLs;
- omit private dashboard navigation, email addresses, credential IDs, API-key metadata, internal team membership, and other non-public account data.

Disabling public sharing immediately invalidates the current public URL. Regenerating the URL invalidates the previous token and creates a new one.

Public access never permits posting, uploading, changing visibility, or accessing any other thread.

### 3.4 Unified effective-access rule

Normal dashboard, API, MCP, CLI, search, attachment, and upload operations must authorize through one centralized effective-access rule:

```text
thread.owner_user_id = current_user_id
OR current_user_id is an active member of a team attached to the thread
```

No caller may authorize thread access by comparing display names, credential names, legacy `created_by` strings, or deployment-owner status outside the explicit owner-only web routes.

Attachment upload, finalize, preview, and download authorization must use the same thread-access decision as thread reads and writes.

## 4. Attribution

Every new message and attachment must preserve both levels of attribution:

- the owning user who is responsible for the action;
- the concrete actor surface that performed it.

Examples:

```text
Ashray · Web dashboard
Ashray · ChatGPT
Ashray · Claude
Ashray · Local CLI
```

Attribution must remain readable after a credential is revoked or a user is disabled. Persisted display snapshots must therefore accompany stable user and credential references.

Legacy messages and attachments whose original actors cannot be mapped to new credentials must preserve their existing author strings as historical attribution and may have null new user/credential references where necessary.

## 5. Invitations and Registration

Public registration is disabled.

The deployment owner creates invitation links from the owner dashboard. An invitation must be:

- cryptographically random;
- stored as a hash rather than a reusable plaintext secret;
- single-use;
- revocable before use;
- expired after an owner-selected duration;
- optionally associated with one or more initial teams.

Opening a valid invitation displays a registration page. The recipient enters:

- email address;
- display name;
- password.

Successful registration must atomically:

- create the user;
- add the invitation's initial team memberships, if any;
- consume the invitation;
- create an authenticated browser session;
- redirect the user to onboarding.

The new user must immediately appear in the owner's user-management dashboard, whether or not the invitation included a team.

Invalid, expired, revoked, or already-consumed invitations must not reveal deployment user information and must not create partial accounts or memberships.

## 6. Onboarding

The first authenticated destination after signup is a resumable onboarding experience. It remains available later from settings.

Onboarding presents three numbered setup steps in this order:

1. Connect ChatGPT
2. Connect Claude
3. Connect a local coding agent

Credentials are created only when the user clicks the relevant setup action. Unused credentials must not be pre-created.

### 6.1 ChatGPT setup

The ChatGPT card creates a dedicated credential and shows its secret only once as a complete remote MCP URL:

```text
https://<deployment>/api/mcp?key=<chatgpt-secret>
```

The card includes current setup instructions for adding a no-auth remote MCP server in ChatGPT. Losing the URL requires rotating or recreating that credential.

### 6.2 Claude setup

The Claude card creates a separate dedicated credential and a separate query-string-authenticated MCP URL. It includes instructions appropriate for connecting the remote AgentBox MCP server to Claude.

ChatGPT and Claude must never share a credential or URL, because independent attribution and revocation are required.

### 6.3 Local coding-agent setup

The local setup card creates one credential for one machine. Additional machines can be configured later from settings with additional credentials.

The card produces a copyable prompt intended to be pasted into a local coding agent such as Codex or Claude Code. The prompt must:

- briefly explain that AgentBox is a shared threaded inbox between the user and their agents;
- include the deployment base URL;
- include the newly generated local credential secret;
- install the public npm CLI package;
- save an active local profile for this user and deployment;
- avoid requiring the deployment-owner secret;
- list accessible threads as the final connection test;
- tell the local agent to report whether setup and the test succeeded.

The initial generated prompt assumes one machine and one key.

## 7. Credential Management

Users can list, create, rotate, and revoke only credentials owned by their own account.

Users can view:

- credential label;
- masked secret or token prefix;
- creation time;
- last-used time;
- revocation state where useful;
- intended surface or purpose.

Secrets are shown only once at creation or rotation and are never retrievable later.

The deployment owner can view credential metadata for every user and can revoke any credential, but cannot retrieve or reconstruct its secret.

Disabling a user immediately revokes all active sessions and credentials owned by that user.

## 8. Team Management

The owner dashboard must support:

- creating a team;
- assigning its name and unique slug;
- viewing its members;
- adding any active registered user;
- removing a user;
- viewing all teams a user belongs to;
- selecting initial teams while creating an invitation.

Users can view the teams they belong to and the stable slugs required by thread-visibility controls. They cannot change memberships.

Removing a user from a team immediately removes access that depended solely on that membership. Existing messages and attachments remain in their canonical threads with attribution intact.

## 9. Thread Visibility Interfaces

### 9.1 MCP

Add exactly one MCP tool for visibility management:

```text
manage_thread_visibility
```

The tool accepts:

```json
{
  "thread_id": "thr_...",
  "add_teams": ["team-slug-or-id"],
  "remove_teams": ["team-slug-or-id"],
  "public": true,
  "regenerate_public_link": false
}
```

All mutation fields are optional. Calling the tool with only `thread_id` returns the current visibility state and the caller's available teams.

The tool must:

- apply additions, removals, and public-state changes atomically;
- treat repeated requests idempotently;
- reject target teams the acting user is not a member of when adding shares;
- return the resulting team shares;
- return the caller's available teams with IDs, slugs, and names;
- return the public URL when public sharing is active;
- return a normal access-denied result when the caller cannot access the thread.

`create_thread` remains private-only and receives no visibility fields.

Existing read tools must return only threads the owning user can currently access. `get_thread` should include safe visibility metadata useful to authenticated clients.

### 9.2 CLI

Add one CLI subcommand:

```text
agentbox visibility <thread-id>
```

Supported behavior:

```text
agentbox visibility <thread-id>
agentbox visibility <thread-id> --share-team <slug-or-id>
agentbox visibility <thread-id> --unshare-team <slug-or-id>
agentbox visibility <thread-id> --publish
agentbox visibility <thread-id> --unpublish
agentbox visibility <thread-id> --regenerate-public-link
```

`--share-team` and `--unshare-team` may be repeated. A single invocation may combine team and public changes and must map to the same atomic backend operation as MCP and the web dashboard.

Running the command without mutation flags displays:

- the owner;
- current team shares;
- current public status and URL;
- the user's available teams.

`agentbox create` remains private-only and receives no public or team flags.

### 9.3 Web dashboard

Every accessible thread page must expose a visibility control that uses the same backend operation. It must show:

- `Private` when no team or public shares exist;
- every team currently attached to the thread;
- the active public URL, when present;
- teams the acting user may add;
- controls to remove existing team shares;
- controls to publish, unpublish, copy, and regenerate the public URL.

The control must make it clear when an action will remove the user's own team-based access.

## 10. Inbox, Search, and Thread Reads

The default inbox remains one unified inbox. A user and all of their credentials see every thread they currently qualify to access:

- private threads they own;
- threads shared with any team they belong to.

The dashboard should expose lightweight filters:

- All
- Private
- Shared with me
- one filter for each team
- Public

The default MCP and CLI list/search behavior uses the unified accessible set without requiring a workspace or team selector.

Search must never reveal titles, message snippets, counts, attachments, or existence of inaccessible threads.

## 11. Owner-Only Web Administration

The owner web dashboard is intentionally more powerful than normal user surfaces.

It must provide:

- deployment user list and status;
- invitation creation, expiry, revocation, and initial-team selection;
- team and membership management;
- per-user credential metadata and forced revocation;
- user disablement;
- an explicitly owner-only content view that can browse all threads and attachments for debugging and support;
- an irreversible attachment-purge action for disabled users.

The owner-only content view must be reachable only through an authenticated owner browser session. It must use separate owner-only web routes or service entry points and must not be expressible through ordinary API-key scopes.

Normal user API keys, including keys owned by the deployment owner, must receive only that user's normal effective thread access.

## 12. User Disablement and Attachment Purge

Users are disabled, not hard-deleted.

Disabling a user must:

- revoke all browser sessions;
- revoke all credentials;
- remove or deactivate all team memberships;
- prevent new login and API authentication;
- retain the user row for attribution;
- retain all threads and messages;
- retain thread ownership;
- retain team-shared threads and their visibility to still-qualified users;
- retain private threads for owner-only web inspection;
- avoid transferring ownership.

The owner may later invoke one irreversible `Purge attachments` action for a disabled user. The purge applies only to attachments uploaded by that user's browser sessions or credentials, identified by stable uploader user ID. It must not delete attachments uploaded by other users merely because they appear in a thread owned by the disabled user.

Purging must:

- delete the corresponding R2 objects;
- retain thread and message records;
- retain an attachment tombstone with filename, original attribution, and purge timestamp;
- render a clear `Attachment deleted by deployment owner` state instead of a broken link;
- be safe to retry without deleting unrelated objects.

No account-ownership transfer workflow is required.

## 13. Data Preservation and Migration

No existing thread, message, or attachment may be lost.

Before any production schema cutover, the migration process must create and verify:

- a PostgreSQL backup containing all thread, message, asset, and pending attachment metadata required for recovery;
- an R2 object inventory and a recoverable backup or copy of every object referenced by existing attachment rows;
- recorded row and object counts sufficient to prove the backup covers current production content;
- a dry-run migration report that identifies orphaned rows or missing R2 objects before destructive changes are allowed.

Authentication and authorization data may be reset. Existing users, sessions, tenant records, API keys, setup tokens, and CLI credentials do not need to survive.

At cutover:

- a new deployment-owner account is created;
- every existing thread is assigned to that owner as a private thread;
- every existing message remains attached to its original thread in original order;
- every existing attachment row remains attached to its original message;
- existing R2 objects remain usable through their stored opaque storage keys and do not need to be renamed;
- legacy author strings are preserved exactly as historical attribution;
- legacy tenant IDs cease to determine access;
- every human and agent credential is recreated under the new user-owned model.

The migration must be resumable or safely retryable and must not require keeping the old tenant authorization path after cutover. The finished system has one authorization model.

## 14. Storage and Attachment Security

The R2 bucket remains private.

All authenticated and public downloads use short-lived signed URLs generated only after verifying either:

- normal effective thread access;
- explicit owner-only web access; or
- a valid active public share token for the exact containing thread.

New object keys should include stable uploader and thread identity for operational clarity, but authorization must never rely on parsing an object key. Legacy object keys remain opaque values stored in the database.

Direct `R2_PUBLIC_BASE_URL` links must not be used to bypass AgentBox authorization in the finished system.

## 15. Required Data Relationships

The implementation may choose exact table and type names, but the final schema must make these relationships explicit and enforceable:

- one deployment owner;
- globally unique active user email;
- users independent of teams;
- user-owned credentials;
- user-owned browser sessions;
- many-to-many team memberships;
- invitations with zero or more initial team memberships;
- exactly one thread owner;
- many-to-many thread/team shares;
- at most one active public share per thread;
- message and attachment attribution to both user and actor where known;
- disabled users retained for historical references;
- purged attachment tombstones retained after R2 deletion.

Authorization must be expressed through stable IDs and relational constraints, not display strings.

## 16. Error and Concurrency Requirements

- Visibility mutations are atomic and idempotent.
- Concurrent attempts to create the same team share produce one share, not duplicates.
- Concurrent publish requests leave one active public token.
- Regeneration invalidates the previous token exactly once.
- Consuming an invitation and creating a user/memberships is one transaction.
- Reusing, racing, or retrying a consumed invitation cannot create duplicate accounts or memberships.
- Disabling a user and revoking credentials takes effect for subsequent requests immediately.
- A caller losing access because of an unshare receives success for the committed mutation and denial on future reads.
- Public and authenticated asset signing verifies the asset belongs to the authorized thread.
- Missing or purged R2 objects produce explicit attachment states rather than leaking storage errors or crashing thread rendering.

## 17. Explicitly Deferred or Out of Scope

- Publishing or distributing the Raycast extension.
- Granular per-thread read versus write roles.
- Per-message visibility.
- Direct user-to-user sharing outside a team.
- Public comments or public uploads.
- Multiple deployment owners or delegated admins.
- User-created teams.
- Ownership transfer.
- Hard deletion of user, thread, or message history.
- Billing, quotas, external identity providers, email verification, and password recovery.
- Preserving old sessions, API keys, tenant records, or CLI profiles through migration.
- Adding sharing flags to `create_thread` or `agentbox create`.

## 18. Acceptance Criteria

1. Existing production threads, messages, and attachment references are backed up, verified, migrated, and readable after cutover with no content loss.
2. The final product has no tenant selector or tenant-wide content visibility path.
3. The deployment has exactly one permanent owner and supports multiple ordinary users.
4. Signup is possible only through a valid, unexpired, unconsumed, owner-generated invitation.
5. An invitation can add the new user to zero, one, or several initial teams atomically.
6. Every new thread is private and visible only to its owner, that owner's credentials, and the owner's web-only debugging viewer.
7. ChatGPT, Claude, local CLI, and browser messages are independently attributable to one user and one actor surface.
8. Users and their credentials list, search, read, post, upload, and download only threads they own or receive through current team memberships.
9. A user may belong to multiple teams, and one thread may be shared with multiple teams without copying data.
10. Any currently authorized participant can add a share to a team they belong to, remove an existing team share, publish, unpublish, or regenerate the public URL.
11. MCP exposes one `manage_thread_visibility` tool; CLI exposes one `agentbox visibility` subcommand; thread creation remains private-only.
12. The unified inbox and normal list/search operations return the complete effective accessible set without a team/workspace selector.
13. Public URLs are opaque, revocable, live, read-only, `noindex`, and render messages and public attachments without exposing private account metadata.
14. Every user independently creates and revokes their own ChatGPT, Claude, and local credentials; secrets are shown only once.
15. First-run onboarding presents numbered ChatGPT, Claude, and local setup cards and produces a working one-machine local-agent setup prompt whose final test lists accessible threads.
16. Only the deployment owner can invite or disable users, manage teams and memberships, inspect all credential metadata, revoke another user's credential, and use the web-only view-all-content surface.
17. The deployment owner's CLI and MCP credentials do not receive view-all-content privileges.
18. Disabling a user immediately revokes sessions and credentials while preserving ownership, messages, team-shared history, and attribution.
19. The owner can idempotently purge only attachments uploaded by a disabled user, freeing R2 storage while preserving attachment tombstones in message history.
20. The finished codebase has one centralized stable-ID-based authorization model and no permanent fallback to the old tenant-wide path.
