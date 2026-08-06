---
name: agentbox-cli-release
description: Cut and verify releases of the Agentbox Go CLI and @amxv/agentbox npm package, including version bumps, tag-triggered GitHub Actions, npm/GitHub Release verification, and a safe local fallback when GitHub-hosted runners are unavailable. Use for any Agentbox CLI release, npm publish, release-tag operation, or release troubleshooting.
---

# Agentbox CLI Release

Release the Go CLI and its npm wrapper as one versioned unit. Use GitHub Actions
by default. Use the manual fallback only when the publish job is cancelled before
any step runs because GitHub cannot assign a hosted runner.

## Release contract

- Repository: `https://github.com/amxv/agentbox`
- Go CLI version: `internal/agentbox/version/version.go`
- npm wrapper version: `npm/agentbox/package.json`
- npm package: `@amxv/agentbox`
- Release tag: `agentbox-cli-vX.Y.Z`
- Publish workflow: `.github/workflows/publish-agentbox-npm.yml`
- Release commit: `chore: release agentbox cli X.Y.Z`

Do not bump the root `package.json` version. It belongs to the private dashboard
package. The preparation script requires the Go and npm CLI versions to match.
The generated files under `npm/agentbox/vendor/` are ignored build artifacts and
must not be committed.

The publish workflow runs CLI tests, builds five binaries, packs and publishes
the npm package with the `NPM_TOKEN` repository secret, and creates a GitHub
Release for tag pushes matching `agentbox-cli-v*`. The expected release assets
are:

- `agentbox-X.Y.Z-darwin-arm64.tar.gz`
- `agentbox-X.Y.Z-darwin-amd64.tar.gz`
- `agentbox-X.Y.Z-linux-arm64.tar.gz`
- `agentbox-X.Y.Z-linux-amd64.tar.gz`
- `agentbox-X.Y.Z-windows-amd64.zip`
- `amxv-agentbox-X.Y.Z.tgz`

## Standard GitHub Actions flow

### 1. Preflight

Work from a clean `main` checkout. Preserve unrelated user changes and stop if
the checkout is dirty or has diverged.

```bash
git status --short --branch
git fetch origin --tags
git pull --ff-only origin main
git tag --list 'agentbox-cli-v*' --sort=-version:refname | head -20
gh release list --repo amxv/agentbox --limit 10
node -p "require('./npm/agentbox/package.json').version"
npm view @amxv/agentbox version dist-tags --json
```

Choose the next semantic version from the current source change. Confirm that
the tag and npm version do not already exist. Before changing versions, inspect
the CLI source and tests relevant to the release, then run the release checks:

```bash
go test ./internal/agentbox/cli ./internal/agentbox/profiles
go test ./...
go vet ./...
node ./scripts/prepare-agentbox-npm.mjs
npm pack --dry-run ./npm/agentbox
npm/agentbox/vendor/darwin-arm64/agentbox --version
```

The dry-run must show the shim, installer, README, license, metadata, and all
five platform binaries. If the release includes visibility work, also verify:

```bash
npm/agentbox/vendor/darwin-arm64/agentbox visibility --help
```

### 2. Bump and push `main`

Use `apply_patch` to update exactly these two files to the same version:

```text
internal/agentbox/version/version.go
npm/agentbox/package.json
```

Check the contract and diff, then commit and push `main`:

```bash
node -e 'const fs=require("fs"); const p=JSON.parse(fs.readFileSync("npm/agentbox/package.json","utf8")); const v=fs.readFileSync("internal/agentbox/version/version.go","utf8").match(/Version = "([^"]+)"/)[1]; if (p.version !== v) process.exit(1); console.log(v)'
git diff --check
git add internal/agentbox/version/version.go npm/agentbox/package.json
git commit -m "chore: release agentbox cli X.Y.Z"
git push origin main
```

Replace `X.Y.Z` in the commit message with the actual version.

### 3. Tag and watch the release

Push the tag only after `main` contains the version bump and local checks pass:

```bash
git tag agentbox-cli-vX.Y.Z
git push origin agentbox-cli-vX.Y.Z
gh run list --repo amxv/agentbox --workflow publish-agentbox-npm.yml --limit 3
```

Watch the run through completion with `gh run watch <run-id> --exit-status`.
Use the environment's background process runner for this long-lived watch; do
not poll it in a loop. A successful run is not complete until both npm and the
GitHub Release are verified:

```bash
gh release view agentbox-cli-vX.Y.Z --repo amxv/agentbox --json tagName,url,assets
npm view @amxv/agentbox version dist-tags.latest --json
```

Confirm that npm reports `X.Y.Z` as `latest` and that all six assets have state
`uploaded`. Report the release URL and any warnings to the requester.

## GitHub Actions availability gate

Do not create another version or tag just because a release job is slow. Inspect
the run and all concurrent runs:

```bash
gh run view <run-id> --repo amxv/agentbox --json status,conclusion,jobs,url
gh run list --repo amxv/agentbox --status queued --limit 20
gh run list --repo amxv/agentbox --status in_progress --limit 20
gh api repos/amxv/agentbox/actions/jobs/<job-id>
```

A degraded hosted-runner condition is indicated by a publish job that remains
queued for several minutes with no steps, `runner_id: 0`, and an empty
`runner_name`. A job that has started or has completed steps is a different
case: inspect its logs and side effects before retrying. Never use the manual
fallback to mask a job that may already have published npm or created a release.

A fresh `workflow_dispatch` run against the existing tag is acceptable after a
pre-step cancellation, but it does not solve a runner outage by itself:

```bash
gh workflow run publish-agentbox-npm.yml --repo amxv/agentbox --ref agentbox-cli-vX.Y.Z
```

Do not cancel unrelated verification runs automatically. If a stale run is
blocking the user's requested release and the user explicitly authorizes it,
cancel that run with `gh run cancel <run-id>`, then inspect the fresh release
run. Do not cancel a job that has already begun release side effects.

## Manual fallback for an unavailable runner

Use this path only when all of the following are true:

1. The tag already points to the intended pushed commit.
2. The publish job was cancelled before any workflow step ran.
3. `npm view @amxv/agentbox@X.Y.Z` confirms that the version is not published.
4. Local Go tests, vet, package preparation, and npm dry-run passed.

This fallback publishes the exact local tarball and creates the release assets
from the same prepared binaries. Do not create a replacement tag.

Prepare a temporary, explicit artifact directory:

```bash
node ./scripts/prepare-agentbox-npm.mjs
release_tmp="$(mktemp -d /tmp/agentbox-release.XXXXXX)"
npm pack ./npm/agentbox --pack-destination "$release_tmp"
VERSION=X.Y.Z
tar -C npm/agentbox/vendor/darwin-arm64 -czf "$release_tmp/agentbox-${VERSION}-darwin-arm64.tar.gz" agentbox
tar -C npm/agentbox/vendor/darwin-amd64 -czf "$release_tmp/agentbox-${VERSION}-darwin-amd64.tar.gz" agentbox
tar -C npm/agentbox/vendor/linux-arm64 -czf "$release_tmp/agentbox-${VERSION}-linux-arm64.tar.gz" agentbox
tar -C npm/agentbox/vendor/linux-amd64 -czf "$release_tmp/agentbox-${VERSION}-linux-amd64.tar.gz" agentbox
(cd npm/agentbox/vendor/windows-amd64 && zip -q "$release_tmp/agentbox-${VERSION}-windows-amd64.zip" agentbox.exe)
```

The scoped package tarball is named `amxv-agentbox-X.Y.Z.tgz`. Before publishing
it, verify the package version, a built binary, and npm authentication:

```bash
test "$(node -p "require('./npm/agentbox/package.json').version")" = "$VERSION"
npm/agentbox/vendor/darwin-arm64/agentbox --version
npm/agentbox/vendor/darwin-arm64/agentbox visibility --help
npm whoami
if npm view "@amxv/agentbox@$VERSION" >/dev/null 2>&1; then echo "version already exists" >&2; exit 1; fi
```

Publish the exact tarball, not a separately regenerated package directory:

```bash
npm publish "$release_tmp/amxv-agentbox-${VERSION}.tgz" --access public
npm view @amxv/agentbox version dist-tags.latest --json
```

If npm publish fails, stop. Do not create a GitHub Release until npm confirms
the version. Once npm succeeds, create a release for the existing tag with all
six artifacts:

```bash
gh release create "agentbox-cli-v${VERSION}" \
  --repo amxv/agentbox \
  --verify-tag \
  --title "Agentbox CLI v${VERSION}" \
  --notes "Published @amxv/agentbox@${VERSION} to npm." \
  "$release_tmp/agentbox-${VERSION}-darwin-arm64.tar.gz" \
  "$release_tmp/agentbox-${VERSION}-darwin-amd64.tar.gz" \
  "$release_tmp/agentbox-${VERSION}-linux-arm64.tar.gz" \
  "$release_tmp/agentbox-${VERSION}-linux-amd64.tar.gz" \
  "$release_tmp/agentbox-${VERSION}-windows-amd64.zip" \
  "$release_tmp/amxv-agentbox-${VERSION}.tgz"
gh release view "agentbox-cli-v${VERSION}" --repo amxv/agentbox --json tagName,url,assets
```

If the release already exists because a workflow partially completed, inspect
its assets and upload only missing files with `gh release upload`; never publish
the npm version again. After verification, move the temporary directory to
Trash when available and report that the manual fallback was used.

## Safety and handoff rules

- Never reuse an existing npm version or tag.
- Never expose `NPM_TOKEN` or other secrets in commands, logs, or messages.
- Never commit `npm/agentbox/vendor/` or other generated binaries.
- If npm succeeded but GitHub Release creation failed, reconcile the release
  assets without republishing npm.
- If GitHub Release creation succeeded but an asset is missing, upload the
  missing asset without creating a new tag.
- Notify downstream operators only after npm and all release assets are
  verified. Include the install command, expected version, and release URL.
