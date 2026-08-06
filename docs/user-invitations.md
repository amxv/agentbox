# Invitation-backed users

AgentBox account creation is invitation-only. The permanent deployment owner
creates and revokes invitations from `/owner/users`. New users register through
`/signup`, receive an HTTP-only browser session, and begin with zero team
memberships.

## Owner workflow

1. Sign in as the permanent owner in a browser.
2. Open `/owner/users`.
3. Choose an expiration and create an invitation.
4. Copy the one-time signup URL and send it to the intended person over a
   private channel.
5. Revoke an unused invitation when it is no longer needed.

The signup URL is returned only when the invitation is created. Invitation
history exposes status and metadata, never the token or its hash.

## Registration guarantees

- PostgreSQL stores only the invitation token hash.
- Expired, revoked, or consumed tokens return the same generic invalid state.
- The token is row-locked during registration, so concurrent double redemption
  can create at most one account.
- User creation, password storage, browser-session creation, and invitation
  consumption commit in one transaction.
- A duplicate email or other failed registration rolls the transaction back and
  leaves an otherwise valid invitation unused.
- The signup page removes the token from the browser address bar immediately.
- Successful registration redirects to `/onboarding`; the account initially has
  no teams and its new threads will become private-by-default when the Phase 3
  authorization model is enabled.

## Owner-only user management

The `/api/owner/*` user and invitation endpoints require a permanent-owner
browser session. They reject:

- normal user browser sessions;
- all API keys, including API keys owned by the owner;
- the deployment admin secret by itself.

The owner can list users and disable or re-enable any non-owner account. The
permanent owner cannot be disabled.

Disabling a user preserves the user row, content, and attribution snapshots but
immediately invalidates:

- browser sessions;
- API credentials;
- pending CLI login codes.

Re-enabling an account does not restore those old credentials or sessions. The
user must sign in again and create new credentials as needed.

## Production configuration

Set `AGENTBOX_APP_PUBLIC_URL` on the Go backend to the dashboard origin so invitation
responses contain complete public signup URLs in split Vercel deployments. The
owner dashboard also converts relative signup paths to its current origin as a
safe fallback.

No production invitation or user account is created from the shared Zodex
implementation machine. The credentialed local cutover operator creates the
first owner, opens `/owner/users`, and performs the real invitation smoke tests.
