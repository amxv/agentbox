SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

.PHONY: help setup setup-dashboard setup-raycast quick check check-go check-go-full check-backend check-cli check-mcp check-dashboard-fast check-dashboard check-integration check-raycast check-hygiene dev-backend dev-dashboard build-cli package-cli migrate

help:
	@printf '%s\n' \
	  'Agentbox development commands' \
	  '' \
	  '  make setup                 install dashboard + Raycast dependencies' \
	  '  make quick                 fast backend + dashboard + CLI integration loop' \
	  '  make check                 complete repository validation (requires TEST_DATABASE_URL)' \
	  '' \
	  '  make check-go              Go tests, vet, and builds; DB tests may skip without TEST_DATABASE_URL' \
	  '  make check-backend         focused backend/MCP/storage Go packages' \
	  '  make check-cli             focused CLI tests + local CLI build' \
	  '  make check-mcp             focused MCP and attachment contract tests' \
	  '  make check-dashboard-fast  dashboard typecheck only' \
	  '  make check-dashboard       dashboard typecheck, lint, production build' \
	  '  make check-integration     dashboard <-> Go contract tests' \
	  '  make check-raycast         Raycast tests, typecheck, lint, build' \
	  '' \
	  '  make dev-backend           run the Go API' \
	  '  make dev-dashboard         run the Next.js dashboard' \
	  '  make build-cli             build dist/agentbox' \
	  '  make package-cli           prepare all npm CLI platform binaries' \
	  '  make migrate               apply checked-in database migrations'

setup: setup-dashboard setup-raycast

setup-dashboard:
	cd apps/dashboard && bun install --frozen-lockfile --linker=isolated

setup-raycast:
	cd apps/raycast && npm ci

quick: check-go check-dashboard-fast check-integration

check: check-go-full check-dashboard check-integration check-raycast check-deploy check-hygiene

check-go:
	go test ./...
	go vet ./...
	go build ./...

check-go-full:
	@if [[ -z "$${TEST_DATABASE_URL:-}" ]]; then echo 'TEST_DATABASE_URL is required for make check / check-go-full.' >&2; exit 1; fi
	@log="$$(mktemp)"; trap 'rm -f "$$log"' EXIT; \
	  AGENTBOX_REQUIRE_POSTGRES_TESTS=1 go test -count=1 -v ./... | tee "$$log"; \
	  if grep -E -- '--- SKIP:|^SKIP$$' "$$log"; then echo 'Full Go validation contained a skipped test.' >&2; exit 1; fi
	go vet ./...
	go build ./...

check-backend:
	go test ./cmd/api ./internal/agentbox/assets ./internal/agentbox/auth ./internal/agentbox/config ./internal/agentbox/db ./internal/agentbox/httpapi ./internal/agentbox/identity ./internal/agentbox/mcpserver ./internal/agentbox/service ./internal/agentbox/types ./internal/agentbox/validate

check-cli:
	go test ./cmd/agentbox ./internal/agentbox/cli ./internal/agentbox/profiles
	@mkdir -p dist
	go build -o dist/agentbox ./cmd/agentbox

check-mcp:
	go test ./internal/agentbox/assets ./internal/agentbox/mcpserver ./internal/agentbox/service ./internal/agentbox/httpapi

check-dashboard-fast:
	cd apps/dashboard && bun run typecheck

check-dashboard:
	cd apps/dashboard && bun run typecheck
	cd apps/dashboard && bun run lint
	cd apps/dashboard && bun run build

check-integration:
	bun test tests/integration/dashboard-backend/visibility-proxy.test.mjs
	bun test tests/integration/dashboard-backend/attachment-proxy.test.mjs

check-deploy:
	node --test tests/deployment/vercel-ignore.test.mjs

check-raycast:
	cd apps/raycast && CI=1 NO_COLOR=1 npm run verify

check-hygiene:
	git diff HEAD --check

dev-backend:
	go run ./cmd/api

dev-dashboard:
	cd apps/dashboard && bun run dev

build-cli:
	@mkdir -p dist
	go build -o dist/agentbox ./cmd/agentbox

package-cli:
	node ./packaging/cli/prepare.mjs

migrate:
	go run ./cmd/migrate
