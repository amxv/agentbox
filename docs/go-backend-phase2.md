# Go backend architecture

The Go backend is the canonical AgentBox service. It owns authentication, REST,
MCP, PostgreSQL persistence, embedded migrations, R2 access, backup/preflight,
and the native CLI contracts. The Next.js application is the browser dashboard
and proxies its `/api/*` requests to the Go service.

Use these maintained references:

- [`user-team-sharing-spec.md`](user-team-sharing-spec.md) for the product and authorization contract;
- [`../public/setup-self-host.md`](../public/setup-self-host.md) for a fresh deployment;
- [`user-team-sharing-production-cutover.md`](user-team-sharing-production-cutover.md) for an existing deployment;
- [`user-team-sharing-backup-preflight.md`](user-team-sharing-backup-preflight.md) for PostgreSQL/R2 preservation evidence.

The ordered SQL files under `migrations/` are the only schema history. Run them
explicitly with `bun run db:migrate`; production should keep
`AGENTBOX_AUTO_MIGRATE=false`. Vercel multipart requests remain bounded by the
platform payload limit, while larger attachments use the direct-to-R2 pending
upload/finalization path.
