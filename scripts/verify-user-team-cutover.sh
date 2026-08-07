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

echo 'Checking maintained Raycast surfaces for retired package assumptions...'
raycast_legacy_pattern='Latest Messages|Search Threads|List Threads|Five commands|5 commands|private team store|npm run publish|ray publish|zue-ai|MCP URL construction'
if rg -n -i "$raycast_legacy_pattern" \
  app/page.tsx \
  app/raycast/page.tsx \
  public/raycast.md \
  public/setup-self-host.md \
  raycast/agentbox/README.md \
  raycast/agentbox/package.json; then
  echo 'Retired Raycast command, Store, or shared-credential assumptions remain in maintained surfaces.' >&2
  exit 1
fi

echo 'Checking the Raycast package contract...'
node <<'NODE'
const fs = require('node:fs');
const manifest = JSON.parse(fs.readFileSync('raycast/agentbox/package.json', 'utf8'));
const expected = ['list-threads', 'create-thread', 'post-message', 'doctor'];
const actual = manifest.commands.map((command) => command.name);
if (JSON.stringify(actual) !== JSON.stringify(expected)) {
  throw new Error(`unexpected Raycast commands: ${JSON.stringify(actual)}`);
}
if (manifest.scripts.publish || !manifest.scripts.verify) {
  throw new Error('Raycast manifest must expose verify and omit Store publishing from the migration path');
}
const baseURL = manifest.preferences.find((preference) => preference.name === 'baseUrl');
const apiKey = manifest.preferences.find((preference) => preference.name === 'apiKey');
if (!baseURL?.required || baseURL.default || !apiKey?.required || apiKey.type !== 'password') {
  throw new Error('Raycast must require a deployment-specific baseUrl and password apiKey without a universal default');
}
NODE

echo 'Checking ChatGPT file-attachment readiness...'
bash scripts/verify-chatgpt-file-attachments.sh

echo 'Checking MCP attachment read/download readiness...'
bash scripts/verify-mcp-attachment-access.sh

echo 'Running Go tests and static analysis...'
if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
  echo 'TEST_DATABASE_URL is required for the user/team cutover verification.' >&2
  exit 1
fi
export AGENTBOX_REQUIRE_POSTGRES_TESTS=1
go_test_log="$(mktemp)"
trap 'rm -f "$go_test_log"' EXIT
go test -count=1 -v ./... | tee "$go_test_log"
if grep -E -- '--- SKIP:|^SKIP$' "$go_test_log"; then
  echo 'User/team cutover verification contained a skipped Go test.' >&2
  exit 1
fi
go vet ./...

echo 'Running dashboard checks and builds...'
bun run typecheck
bun run lint
bun run build:api
bun run build:cli
bun run build

echo 'Running clean Raycast package verification...'
(
  cd raycast/agentbox
  npm ci
  CI=1 NO_COLOR=1 npm run verify
)

echo 'Checking patch hygiene...'
git diff --check

echo 'PostgreSQL integration tests ran with AGENTBOX_REQUIRE_POSTGRES_TESTS=1 and no skips.'

echo 'User/team cutover verification passed.'
