import { chmodSync, copyFileSync, existsSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const packageRoot = join(__dirname, "..");
const binDir = join(packageRoot, "bin");

const targets = {
  "darwin-arm64": { source: join(packageRoot, "vendor", "darwin-arm64", "agentbox"), dest: join(binDir, "agentbox") },
  "darwin-x64": { source: join(packageRoot, "vendor", "darwin-amd64", "agentbox"), dest: join(binDir, "agentbox") },
  "linux-arm64": { source: join(packageRoot, "vendor", "linux-arm64", "agentbox"), dest: join(binDir, "agentbox") },
  "linux-x64": { source: join(packageRoot, "vendor", "linux-amd64", "agentbox"), dest: join(binDir, "agentbox") },
  "win32-x64": {
    source: join(packageRoot, "vendor", "windows-amd64", "agentbox.exe"),
    dest: join(binDir, "agentbox.exe")
  }
};

const key = `${process.platform}-${process.arch}`;
const target = targets[key];

if (!target) {
  console.error(
    [
      `Unsupported platform for @amxv/agentbox: ${process.platform}/${process.arch}.`,
      "Supported targets: darwin/arm64, darwin/x64, linux/arm64, linux/x64, windows/x64."
    ].join(" ")
  );
  process.exit(1);
}

if (!existsSync(target.source)) {
  console.error(`Missing packaged binary: ${target.source}. The release artifacts are incomplete.`);
  process.exit(1);
}

mkdirSync(binDir, { recursive: true });
rmSync(join(binDir, "agentbox"), { force: true });
rmSync(join(binDir, "agentbox.exe"), { force: true });
copyFileSync(target.source, target.dest);

if (process.platform !== "win32") {
  chmodSync(target.dest, 0o755);
}
