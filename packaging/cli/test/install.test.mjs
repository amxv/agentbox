import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { installNativeBinary, installTargets } from "../scripts/install-lib.js";

function fixture() {
  const root = mkdtempSync(join(tmpdir(), "agentbox-install-test-"));
  const targets = installTargets(root);
  for (const [key, target] of Object.entries(targets)) {
    mkdirSync(join(target.source, ".."), { recursive: true });
    writeFileSync(target.source, `binary:${key}`);
  }
  writeFileSync(join(root, "vendor", "metadata.json"), "{}\n");
  return { root, targets };
}

test("Windows x64 install selects the native executable and prunes other payloads", () => {
  const { root, targets } = fixture();
  const installed = installNativeBinary(root, "win32", "x64");
  assert.equal(installed.key, "win32-x64");
  assert.equal(readFileSync(join(root, "bin", "agentbox.exe"), "utf8"), "binary:win32-x64");
  assert.equal(existsSync(targets["win32-x64"].source), true);
  assert.equal(existsSync(targets["win32-arm64"].source), false);
  assert.equal(existsSync(targets["linux-x64"].source), false);
  assert.equal(existsSync(join(root, "vendor", "metadata.json")), true);
});

test("Windows ARM64 install selects the ARM64 executable", () => {
  const { root } = fixture();
  installNativeBinary(root, "win32", "arm64");
  assert.equal(readFileSync(join(root, "bin", "agentbox.exe"), "utf8"), "binary:win32-arm64");
});

test("unsupported Windows CPU fails clearly", () => {
  const { root } = fixture();
  assert.throws(
    () => installNativeBinary(root, "win32", "ia32"),
    /Unsupported platform for @amxv\/agentbox: win32\/ia32/
  );
});
