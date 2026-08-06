# Permanent owner setup and recovery

AgentBox has exactly one permanent deployment owner. The owner cannot be
transferred, demoted, disabled, or deleted through normal product flows.

General public password recovery is intentionally out of scope. Instead, a
trusted deployment operator uses `AGENTBOX_ADMIN_KEY` to issue a short-lived,
single-use browser token. The deployment secret never enters the browser.

## Backend configuration

Set `AGENTBOX_APP_PUBLIC_URL` on the Go backend to the public dashboard origin. This is
required when the backend and Next.js dashboard are separate Vercel projects:

```bash
printf 'https://agentbox.example.com' | vercel env add AGENTBOX_APP_PUBLIC_URL production
```

The backend uses that origin when it returns the browser setup link. For local
development or unusual routing, `agentbox owner setup-token --app-url ...` can
override a relative link.

## Create the first owner

From a trusted operator shell with the deployment secret available:

```bash
agentbox owner setup-token \
  --base-url https://api.agentbox.example.com \
  --admin-key "$AGENTBOX_ADMIN_KEY" \
  --expires 30m
```

The command prints a URL such as:

```text
https://agentbox.example.com/owner/setup?token=agos_...
```

Open it once in a trusted browser. The browser removes the token from the
address bar immediately, submits it directly to the backend through the
dashboard proxy, and receives an HTTP-only owner session only after the owner
account update commits successfully.

## Recover the same owner

Run the same command again. Once an owner exists, the issued token is marked as
`recovery` rather than `bootstrap`.

Recovery must use the permanent owner's existing email address. It may update
the display name and password, but it cannot create a replacement owner or
transfer ownership to another email.

## Security properties

- Only `AGENTBOX_ADMIN_KEY` can issue a setup/recovery token.
- Browser sessions and API keys, including those owned by the owner, cannot
  issue these tokens.
- Only the token hash is stored in PostgreSQL.
- Issuing a new token revokes the previous active token.
- Tokens expire, are consumed once, and reject replay.
- A failed recovery, including an email mismatch, rolls back token consumption.
- The deployment secret is never embedded in the setup URL or browser request.
- API keys owned by the owner do not inherit browser-only owner authority.

Treat the printed URL as sensitive until it is consumed or expires. Do not send
it through public chat, issue trackers, shell tracing, or shared logs.
