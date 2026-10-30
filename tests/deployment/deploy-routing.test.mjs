import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');

function workflow(name) {
  return readFileSync(join(root, '.github', 'workflows', name), 'utf8');
}

function pathFilters(source, event) {
  const block = source.match(new RegExp(`^  ${event}:\\n([\\s\\S]*?)(?=^  (?:push|pull_request|workflow_dispatch):|^permissions:)`, 'm'))?.[1];
  assert.ok(block, `missing ${event} event`);
  return [...block.matchAll(/^      - ['"]([^'"]+)['"]$/gm)].map(m => m[1]);
}

test('backend deployment is API-specific, and frontend deployment is dashboard-specific', () => {
  const api = workflow('deploy-api.yml');
  const dash = workflow('deploy-dashboard.yml');
  for (const event of ['push', 'pull_request']) {
    const apiPaths = pathFilters(api, event);
    const dashPaths = pathFilters(dash, event);
    for (const entry of ['cmd/api/**', 'internal/**', 'migrations/**', 'go.mod', 'go.sum']) {
      assert.ok(apiPaths.includes(entry), `missing backend input ${entry}`);
    }
    assert.ok(!apiPaths.some(p => p.startsWith('apps/dashboard')));
    assert.ok(dashPaths.includes('apps/dashboard/**'));
    assert.ok(!dashPaths.includes('cmd/**') && !dashPaths.includes('internal/**'));
  }
  for (const source of [api, dash]) {
    assert.match(source, /github\.event\.pull_request\.head\.repo\.full_name == github\.repository/);
    assert.match(source, /vercel@65\.1\.0 deploy/);
    assert.match(source, /--prod/);
  }
});

test('both Vercel projects disable automatic Git deploys and ignored-build scripts', () => {
  for (const file of ['vercel.json', 'cmd/api/vercel.json', 'apps/dashboard/vercel.json']) {
    const config = JSON.parse(readFileSync(join(root, file), 'utf8'));
    assert.equal(config.git.deploymentEnabled, false, `${file} must not auto-deploy`);
    assert.equal(Object.hasOwn(config, 'ignoreCommand'), false, `${file} must not ignore an already-created deployment`);
  }
});
