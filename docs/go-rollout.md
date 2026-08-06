# Go backend rollout

The Go backend is the canonical AgentBox service. Follow [`public/setup-self-host.md`](../public/setup-self-host.md) for a fresh deployment and [`user-team-sharing-backup-preflight.md`](user-team-sharing-backup-preflight.md) for migration readiness.

The rollout order is:

1. Produce and verify the PostgreSQL/R2 backup manifest.
2. Deploy code containing the canonical checked-in migrations.
3. Run `bun run db:migrate` explicitly.
4. Create or recover the permanent owner with `agentbox owner setup-token`.
5. Invite additional users from `/owner/users`.
6. Sign in with `agentbox login` and create separate user-owned credentials for ChatGPT, Claude, local agents, Raycast, scripts, and CI.
7. Execute the smoke checks documented in the production cutover runbook.

The dashboard and Go backend may be separate Vercel projects. Set `AGENTBOX_BACKEND_URL` on the dashboard and `AGENTBOX_APP_PUBLIC_URL` on the backend so browser flows are routed correctly. Attachment objects remain private in R2; authenticated and public-link downloads use bounded signed URLs.
