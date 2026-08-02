#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

legacy_pattern='tenant_id|tenant_slug|DefaultTenantID|TenantID|TenantSlug|type Tenant struct|ProvisionTenant|ProvisionUser|runInit|createTenant|R2_PUBLIC_BASE_URL|R2PublicBaseURL|PublicURLForKey|/api/admin/keys|/api/admin/tenants'

echo 'Checking that runtime code contains no legacy authorization path...'
if rg -n "$legacy_pattern" cmd internal app \
  --glob '!**/*_test.go' \
  --glob '!**/node_modules/**'; then
  echo 'Legacy user/team cutover symbols remain in runtime code.' >&2
  exit 1
fi

echo 'Checking that request paths never execute schema setup...'
if rg -n 'EnsureSchema' cmd internal app \
  --glob '!**/*_test.go' \
  --glob '!**/node_modules/**'; then
  echo 'Runtime schema setup remains outside the canonical migration runner.' >&2
  exit 1
fi

echo 'Running Go tests and static analysis...'
go test ./...
go vet ./...

echo 'Running dashboard checks and builds...'
bun run typecheck
bun run lint
bun run build:api
bun run build:cli
bun run build

echo 'Checking patch hygiene...'
git diff --check

if [[ -n "${TEST_DATABASE_URL:-}" ]]; then
  echo 'PostgreSQL integration tests ran through go test ./... using TEST_DATABASE_URL.'
else
  echo 'TEST_DATABASE_URL is not set; PostgreSQL integration tests were compiled and discovered but require CI or local verification.'
fi

echo 'User/team cutover verification passed.'
