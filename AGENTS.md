# Agentbox Agent Guide

Agentbox is a Go service and CLI with independent web and Raycast clients. Keep component boundaries obvious and use the smallest relevant verification loop while editing, then run the complete suite before shipping cross-cutting work.

## Repository map

```text
cmd/api/             Go backend executable and backend deployment config
cmd/agentbox/        native CLI executable
cmd/migrate/         explicit database migration command
internal/agentbox/   canonical product implementation shared by backend and CLI
migrations/          embedded ordered PostgreSQL migrations
apps/dashboard/      Next.js user/admin dashboard and current public web surfaces
apps/raycast/        independent Raycast extension
packaging/cli/       @amxv/agentbox npm distribution wrapper
tests/integration/   cross-component dashboard/backend contracts
```

The MCP server is part of the Go backend at `/api/mcp`; it is not a separate service. The dashboard proxies its `/api/*` routes to the Go backend. Raycast and the CLI are API clients of the same backend.

Within the Go implementation, follow the domain filenames instead of starting from a giant catch-all file:

- `internal/agentbox/db/repository_*.go` contains PostgreSQL behavior by domain (`threads`, `uploads`, `credentials`, `accounts`, `teams`); the matching `memory_*.go` files mirror those behaviors for the in-memory repository. `repository_helpers.go` owns shared row scanners/value helpers, while `repository.go` owns the shared repository core.
- `internal/agentbox/service/{threads,uploads,credentials,accounts,sessions}.go` contains product behavior; `service.go` owns the repository contract, shared types, and cross-domain helpers.
- `internal/agentbox/httpapi/server_{threads,credentials,auth,owner}.go` contains HTTP handlers; `server.go` owns routing, middleware, parsing/error helpers, and response shaping.
- Large Go tests are split by the same durable behavior areas. Prefer adding a test beside the behavior it exercises rather than growing a generic test file.

`docs/` is intentionally unused and reserved for a future Astro documentation site. Do not recreate the old repository-docs collection there. Temporary implementation notes may be kept under ignored `tmp/gg/` when useful, but tracked code and agent instructions must not depend on them.

## First setup

```bash
make setup
```

This installs the dashboard with Bun and the Raycast extension with npm. Go uses the root `go.mod` directly.

## Verification

For the common backend + dashboard + CLI change loop:

```bash
make quick
```

`make quick` runs Go tests/vet/builds, dashboard typechecking, and the dashboard/backend integration contracts. Use narrower checks while iterating when appropriate:

```bash
make check-backend
make check-cli
make check-mcp
make check-dashboard-fast
make check-dashboard
make check-integration
make check-raycast
```

Before shipping a cross-cutting change, run:

```bash
TEST_DATABASE_URL='postgres://...' make check
```

The full gate requires PostgreSQL and fails if any Go test skips. CI runs the same component commands in parallel. Do not reintroduce source-text guards or repository-layout tests when a behavioral/protocol test can express the contract.

## Development

```bash
make dev-backend
make dev-dashboard
make build-cli
make migrate
```

Useful direct commands remain valid. Prefer native component tools inside a component and the Makefile only for cross-component orchestration.

## Deployment ownership

Backend deployment files are colocated with the backend executable:

```text
cmd/api/vercel.json
cmd/api/should-build.mjs
cmd/api/r2-cors.json
```

The dashboard owns its own Vercel config:

```text
apps/dashboard/vercel.json
```

Backend deployment from the repository root:

```bash
vercel link --yes --project agentbox-go
vercel --prod --yes -A cmd/api/vercel.json
go run ./cmd/migrate
```

Dashboard deployment from its app directory:

```bash
cd apps/dashboard
vercel link --yes --project agentbox
vercel --prod --yes
```

The dashboard needs `AGENTBOX_BACKEND_URL` pointing to the Go backend. Do not deploy, publish, migrate production, or cut a CLI release unless the user explicitly asks.

## CLI packaging and releases

The Go CLI version is `internal/agentbox/version/version.go`; the npm wrapper is `packaging/cli/package.json`. Build distribution artifacts with:

```bash
make package-cli
npm pack --dry-run ./packaging/cli
```

For an actual release, follow `.agents/skills/agentbox-cli-release/SKILL.md`.

## Change discipline

- Keep the root Go module as the canonical backend/CLI implementation. Do not add a Go workspace or JS monorepo framework without a concrete need.
- Keep dashboard dependencies and scripts inside `apps/dashboard`; installing it must not compile the Go CLI.
- Keep Raycast independent under `apps/raycast`.
- Keep npm distribution mechanics under `packaging/cli`; generated binaries and copied licenses remain ignored.
- Preserve migration history. New schema changes get a new ordered migration rather than editing applied migrations.
- Prefer behavior and protocol contracts over tests that grep source spelling.
- Keep temporary plans and handoffs out of the product tree.
