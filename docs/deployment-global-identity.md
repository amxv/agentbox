# Deployment-global identity

AgentBox no longer asks a person or client to select a tenant when signing in.
An email address identifies exactly one deployment-global user, and every
browser session, CLI credential, ChatGPT credential, Claude credential, and
other API key acts for that user with its own actor attribution.

## Account creation paths

There are only two account-establishment paths:

1. A trusted deployment operator issues a one-time owner bootstrap or recovery
   token with `agentbox owner setup-token`.
2. The permanent owner creates a one-time signup invitation from
   `/owner/users` for every non-owner account.

The previous deployment-secret tenant provisioning API and
`agentbox provision tenant` command are retired. `AGENTBOX_ADMIN_KEY` cannot
create normal users, daily API credentials, or access the owner browser API.

## Login

Browser login accepts only:

- email;
- password.

The backend performs a deployment-wide case-insensitive email lookup. A tenant
identifier in an old client request is ignored by JSON decoding and cannot
change which account is selected. Current dashboard and CLI clients do not send
one.

## CLI profiles

Saved CLI profiles contain the backend URL, user/actor metadata, and the active
user-owned credential. They no longer persist `tenant_id` or `tenant_slug`.

Existing accounts, sessions, credentials, and local profiles are explicitly
resettable during this feature cutover. Operators should remove or recreate any
legacy profile that predates deployment-global identity rather than relying on
tenant metadata.

## Transitional database note

Some content tables and internal authorization contexts still carry tenant
columns while Phase 3 replaces tenant-wide content visibility with thread
ownership and team grants. Those columns are migration scaffolding only; they
are no longer a product identity boundary and must not be exposed as an account
selector.

The final cleanup phase deletes the remaining tenant-era schema and internal
code after content ownership, teams, public links, attachments, MCP, CLI, and
dashboard flows all use the canonical user/team authorization predicate.
