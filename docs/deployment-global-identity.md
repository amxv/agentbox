# Deployment-global identity

AgentBox uses one deployment-global user directory. An email address identifies
exactly one user, and every
browser session, CLI credential, ChatGPT credential, Claude credential, and
other API key acts for that user with its own actor attribution.

## Account creation paths

There are only two account-establishment paths:

1. A trusted deployment operator issues a one-time owner bootstrap or recovery
   token with `agentbox owner setup-token`.
2. The permanent owner creates a one-time signup invitation from
   `/owner/users` for every non-owner account.

The removed deployment-secret account-provisioning API and compatibility CLI
commands are unavailable. `AGENTBOX_ADMIN_KEY` cannot
create normal users, daily API credentials, or access the owner browser API.

## Login

Browser login accepts only:

- email;
- password.

The backend performs a deployment-wide case-insensitive email lookup. Unknown
legacy request fields are ignored by JSON decoding and cannot change which
account is selected.

## CLI profiles

Saved CLI profiles contain the backend URL, user/actor metadata, and the active
user-owned credential. They contain no account-partition metadata.

Existing accounts, sessions, credentials, and local profiles are explicitly
resettable during this feature cutover. Operators should remove or recreate any
profile that predates deployment-global identity.

## Final authorization boundary

Content access is determined only by stable thread ownership and current team
membership. Public links are separate revocable read-only capabilities. The
permanent owner's deployment-wide content viewer is browser-session-only and is
not inherited by API keys.
