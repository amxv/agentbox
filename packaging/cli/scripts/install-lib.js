import { chmodSync, copyFileSync, existsSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";

export function installTargets(packageRoot) {
  const binDir = join(packageRoot, "bin");
  return {
    "darwin-arm64": { source: join(packageRoot, "vendor", "darwin-arm64", "agentbox"), dest: join(binDir, "agentbox") },
    "darwin-x64": { source: join(packageRoot, "vendor", "darwin-amd64", "agentbox"), dest: join(binDir, "agentbox") },
    "linux-arm64": { source: join(packageRoot, "vendor", "linux-arm64", "agentbox"), dest: join(binDir, "agentbox") },
    "linux-x64": { source: join(packageRoot, "vendor", "linux-amd64", "agentbox"), dest: join(binDir, "agentbox") },
    "win32-x64": {
      source: join(packageRoot, "vendor", "windows-amd64", "agentbox.exe"),
      dest: join(binDir, "agentbox.exe")
    },
    "win32-arm64": {
      source: join(packageRoot, "vendor", "windows-arm64", "agentbox.exe"),
      dest: join(binDir, "agentbox.exe")
    }
  };
}

export function installNativeBinary(packageRoot, platform, arch) {
  const targets = installTargets(packageRoot);
  const key = `${platform}-${arch}`;
  const target = targets[key];

  if (!target) {
    throw new Error(
      [
        `Unsupported platform for @amxv/agentbox: ${platform}/${arch}.`,
        "Supported targets: darwin/arm64, darwin/x64, linux/arm64, linux/x64, windows/arm64, windows/x64."
      ].join(" ")
    );
  }

  if (!existsSync(target.source)) {
    throw new Error(`Missing packaged binary: ${target.source}. The release artifacts are incomplete.`);
  }

  const binDir = dirname(target.dest);
  mkdirSync(binDir, { recursive: true });
  rmSync(join(binDir, "agentbox"), { force: true });
  rmSync(join(binDir, "agentbox.exe"), { force: true });
  copyFileSync(target.source, target.dest);

  // Keep only this machine's native payload after install. The published npm
  // tarball remains self-contained for every platform, while an installed
  // package no longer wastes disk space on binaries for other operating
  // systems and CPU architectures.
  for (const [candidateKey, candidate] of Object.entries(targets)) {
    if (candidateKey === key) continue;
    rmSync(dirname(candidate.source), { recursive: true, force: true });
  }

  if (platform !== "win32") {
    chmodSync(target.dest, 0o755);
  }

  return { key, ...target };
}
