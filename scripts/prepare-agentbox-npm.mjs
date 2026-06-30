import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, unlinkSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, "..");
const packageRoot = join(repoRoot, "npm", "agentbox");
const versionFile = join(repoRoot, "internal", "agentbox", "version", "version.go");
const npmPackageFile = join(packageRoot, "package.json");
const licenseFile = join(repoRoot, "LICENSE");
const vendorRoot = join(packageRoot, "vendor");

const targets = [
  { goos: "darwin", goarch: "arm64", dir: "darwin-arm64", binary: "agentbox" },
  { goos: "darwin", goarch: "amd64", dir: "darwin-amd64", binary: "agentbox" },
  { goos: "linux", goarch: "arm64", dir: "linux-arm64", binary: "agentbox" },
  { goos: "linux", goarch: "amd64", dir: "linux-amd64", binary: "agentbox" },
  { goos: "windows", goarch: "amd64", dir: "windows-amd64", binary: "agentbox.exe" }
];

function fail(message) {
  console.error(message);
  process.exit(1);
}

function readVersion() {
  const source = readFileSync(versionFile, "utf8");
  const match = source.match(/const Version = "([^"]+)"/);
  if (!match) fail(`Unable to parse CLI version from ${versionFile}.`);
  return match[1];
}

function verifyPackageVersion(expectedVersion) {
  const pkg = JSON.parse(readFileSync(npmPackageFile, "utf8"));
  if (pkg.name !== "@amxv/agentbox") {
    fail(`Unexpected npm package name ${pkg.name}. Expected @amxv/agentbox.`);
  }
  if (pkg.version !== expectedVersion) {
    fail(
      `npm/agentbox/package.json version ${pkg.version} does not match CLI version ${expectedVersion}. Update them together before publishing.`
    );
  }
}

function run(cmd, args, env) {
  const result = spawnSync(cmd, args, {
    cwd: repoRoot,
    env,
    stdio: "inherit"
  });
  if (result.status !== 0) {
    fail(`Command failed: ${cmd} ${args.join(" ")}`);
  }
}

function cleanGeneratedOutputs() {
  rmSync(vendorRoot, { recursive: true, force: true });
  unlinkIfExists(join(packageRoot, "bin", "agentbox"));
  unlinkIfExists(join(packageRoot, "bin", "agentbox.exe"));
}

function unlinkIfExists(file) {
  if (existsSync(file)) {
    unlinkSync(file);
  }
}

function buildArtifacts(version) {
  mkdirSync(vendorRoot, { recursive: true });
  for (const target of targets) {
    const targetDir = join(vendorRoot, target.dir);
    mkdirSync(targetDir, { recursive: true });
    const output = join(targetDir, target.binary);
    console.log(`Building ${target.goos}/${target.goarch} -> ${output}`);
    run(
      "go",
      ["build", "-trimpath", "-ldflags", "-s -w", "-o", output, "./cmd/agentbox"],
      {
        ...process.env,
        CGO_ENABLED: "0",
        GOOS: target.goos,
        GOARCH: target.goarch,
        AGENTBOX_VERSION: version
      }
    );
  }
}

function copyLicense() {
  cpSync(licenseFile, join(packageRoot, "LICENSE"));
}

function writeMetadata(version) {
  const metadataFile = join(packageRoot, "vendor", "metadata.json");
  writeFileSync(
    metadataFile,
    `${JSON.stringify(
      {
        package: "@amxv/agentbox",
        version,
        targets: targets.map(({ dir, goos, goarch, binary }) => ({ dir, goos, goarch, binary }))
      },
      null,
      2
    )}\n`
  );
}

const version = readVersion();
verifyPackageVersion(version);
cleanGeneratedOutputs();
buildArtifacts(version);
copyLicense();
writeMetadata(version);
console.log(`Prepared npm package in ${packageRoot}`);
