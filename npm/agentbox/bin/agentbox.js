#!/usr/bin/env node

import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const __dirname = dirname(fileURLToPath(import.meta.url));
const nativeBinary = process.platform === "win32" ? "agentbox.exe" : "agentbox";
const nativePath = join(__dirname, nativeBinary);

if (!existsSync(nativePath)) {
  console.error(
    [
      "The Agentbox native binary is missing.",
      "Reinstall @amxv/agentbox so the install script can place the correct binary for this machine."
    ].join(" ")
  );
  process.exit(1);
}

const result = spawnSync(nativePath, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

if (result.signal) {
  process.kill(process.pid, result.signal);
}

process.exit(result.status ?? 1);
