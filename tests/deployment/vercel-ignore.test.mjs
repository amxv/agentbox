import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..", "..");

function git(repo, ...args) {
  return execFileSync("git", args, { cwd: repo, encoding: "utf8" }).trim();
}

function commit(repo, message) {
  git(repo, "add", "-A");
  git(repo, "commit", "-m", message);
  return git(repo, "rev-parse", "HEAD");
}

function runFilter(repo, scriptRelative, cwdRelative, base, head) {
  return spawnSync("node", [join(repo, scriptRelative)], {
    cwd: join(repo, cwdRelative),
    env: {
      ...process.env,
      VERCEL_GIT_PREVIOUS_SHA: base,
      VERCEL_GIT_COMMIT_SHA: head,
      VERCEL_GIT_PULL_REQUEST_BASE_SHA: "",
    },
    stdio: "ignore",
  }).status;
}

function fixture() {
  const repo = mkdtempSync(join(tmpdir(), "agentbox-vercel-ignore-"));
  git(repo, "init", "-q");
  git(repo, "config", "user.email", "test@example.invalid");
  git(repo, "config", "user.name", "Agentbox Test");
  mkdirSync(join(repo, "cmd", "api"), { recursive: true });
  mkdirSync(join(repo, "apps", "dashboard"), { recursive: true });
  mkdirSync(join(repo, "internal"), { recursive: true });
  mkdirSync(join(repo, "packaging", "cli"), { recursive: true });
  copyFileSync(join(repoRoot, "cmd", "api", "should-build.mjs"), join(repo, "cmd", "api", "should-build.mjs"));
  copyFileSync(join(repoRoot, "apps", "dashboard", "should-build.mjs"), join(repo, "apps", "dashboard", "should-build.mjs"));
  writeFileSync(join(repo, "go.mod"), "module example.invalid/agentbox\n\ngo 1.25\n");
  writeFileSync(join(repo, "apps", "dashboard", "package.json"), "{}\n");
  writeFileSync(join(repo, "packaging", "cli", "README.md"), "package\n");
  const base = commit(repo, "base");
  return { repo, base };
}

test("backend and dashboard deploy filters are path scoped and fail open", () => {
  const { repo, base } = fixture();

  writeFileSync(join(repo, "packaging", "cli", "README.md"), "package-only change\n");
  const packageHead = commit(repo, "package only");
  assert.equal(runFilter(repo, "cmd/api/should-build.mjs", ".", base, packageHead), 0, "backend should skip unrelated packaging changes");
  assert.equal(runFilter(repo, "apps/dashboard/should-build.mjs", "apps/dashboard", base, packageHead), 0, "dashboard should skip unrelated packaging changes");

  writeFileSync(join(repo, "internal", "service.go"), "package internal\n");
  const backendHead = commit(repo, "backend");
  assert.equal(runFilter(repo, "cmd/api/should-build.mjs", ".", packageHead, backendHead), 1, "backend should build for core Go changes");
  assert.equal(runFilter(repo, "apps/dashboard/should-build.mjs", "apps/dashboard", packageHead, backendHead), 0, "dashboard should skip backend-only changes");

  writeFileSync(join(repo, "apps", "dashboard", "page.tsx"), "export default function Page() {}\n");
  const dashboardHead = commit(repo, "dashboard");
  assert.equal(runFilter(repo, "cmd/api/should-build.mjs", ".", backendHead, dashboardHead), 0, "backend should skip dashboard-only changes");
  assert.equal(runFilter(repo, "apps/dashboard/should-build.mjs", "apps/dashboard", backendHead, dashboardHead), 1, "dashboard should build for dashboard changes");

  assert.equal(runFilter(repo, "cmd/api/should-build.mjs", ".", "missing-base", dashboardHead), 1, "backend must fail open when base cannot be resolved");
  assert.equal(runFilter(repo, "apps/dashboard/should-build.mjs", "apps/dashboard", "missing-base", dashboardHead), 1, "dashboard must fail open when base cannot be resolved");
});
