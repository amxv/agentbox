# Agentbox

Agentbox is a shared threaded inbox for humans, ChatGPT, local coding agents, Raycast, and automation. A Go backend owns the inbox, authentication, sharing, Postgres state, R2 attachments, and MCP surface; the CLI and web clients all speak to that same service.

```text
ChatGPT creates a thread → local agent works it → results return as messages/files → humans and agents share one history
```

## Install the CLI

```bash
npm install -g @amxv/agentbox
agentbox --version
```

Windows x64 and Windows ARM64 are both shipped as native executables. The npm installer selects the native binary automatically.

Create a reusable profile:

```bash
agentbox profiles add prod \
  --base-url https://your-agentbox.example \
  --api-key LOCAL_KEY \
  --activate
```

Common commands:

```bash
agentbox doctor
agentbox list
agentbox search "design"
agentbox create "Design thread" --message "Please implement this."
agentbox get thr_xxx
agentbox post thr_xxx "done" --asset result.md
agentbox download thr_xxx --output ./downloads
agentbox visibility thr_xxx --share-team engineering --publish
```

Published threads expose both the normal read-only web URL and an agent-friendly Markdown URL ending in `.md`. The Markdown view preserves message content and includes short-lived presigned Cloudflare R2 links for attachments, so attachment bytes download directly from R2 rather than being proxied through the Agentbox/Vercel backend.

## Connect ChatGPT

Create a dedicated user-owned key and add Agentbox as a custom MCP server:

```text
https://your-agentbox.example/api/mcp?key=CHATGPT_KEY
```

The MCP server is mounted by the Go backend and exposes thread listing/search, reads, creation/posting, visibility management, and explicit attachment read/download tools. ChatGPT can also pass an authorized file artifact to `post_message`; Agentbox fetches it through the guarded downloader and stores the resulting attachment in private R2.

## Connect Raycast

The extension is an independent app in `apps/raycast`:

```bash
git clone https://github.com/amxv/agentbox.git
cd agentbox/apps/raycast
npm ci
npm run verify
npm run dev
```

Create a dedicated Raycast credential from the signed-in dashboard and enter its `baseUrl` and `apiKey` preferences in Raycast.

## Web dashboard

The Next.js application in `apps/dashboard` provides the user inbox, thread views, attachment previews/downloads, onboarding and credential management, plus permanent-owner administration. Its `/api/*` handlers are thin same-origin proxies to the Go backend.

The permanent owner can generate a **password reset link** for an active non-owner account from **Owner → Users**. Copy the link and share it privately; Agentbox does not send email. Links expire after one hour, are single-use, and are replaced when the owner generates another link for the same user. The recipient can set a new password at `/reset-password` and then sign in normally. The reset logs out existing dashboard sessions but preserves connected agent/CLI API keys and team memberships.

The current app also still serves a few public setup/landing surfaces. A future lightweight site can take those over without changing the backend or dashboard architecture.

## Repository

```text
cmd/api/             Go backend executable + backend deployment config
cmd/agentbox/        native CLI executable
cmd/migrate/         database migration command
internal/agentbox/   backend/CLI implementation
migrations/          embedded PostgreSQL schema history
apps/dashboard/      Next.js dashboard
apps/raycast/        Raycast extension
packaging/cli/       npm wrapper and platform packaging
tests/integration/   cross-component contracts
```

`docs/` is reserved for the future site and is intentionally absent today.

## Development

Install dependencies:

```bash
make setup
```

For the normal backend + dashboard + CLI iteration loop:

```bash
make quick
```

Useful targeted gates:

```bash
make check-backend
make check-cli
make check-mcp
make check-dashboard-fast
make check-dashboard
make check-integration
make check-raycast
```

Run the complete repository gate before shipping cross-cutting changes:

```bash
TEST_DATABASE_URL='postgres://...' make check
```

The full gate requires PostgreSQL and rejects skipped Go tests. Run `make help` for the complete command surface.

Local services:

```bash
make dev-backend
make dev-dashboard
make migrate
make build-cli
```

## Installed web app links

The production dashboard is `https://agentbox.ashray.xyz`. Share ordinary HTTPS
thread URLs (`https://agentbox.ashray.xyz/threads/<thread-id>`), not custom schemes.
The dashboard manifest keeps its existing `id: /threads`, `start_url: /threads`,
and `scope: /` so existing installations and all thread URLs stay in scope.
Its `launch_handler.client_mode: navigate-existing` asks supporting Chromium
browsers to navigate an existing PWA window to the captured URL instead of
just focusing the inbox. **The browser and OS decide whether a link is captured.**

- **Android:** A Chrome-installed **WebAPK** can register in-scope HTTPS links
  with Android. Link handling still depends on browser/OS defaults, the user's
  preferences, and the source app. Ordinary browser home-screen shortcuts do
  not gain the same URL handling. Check Android Settings > Apps > Agentbox >
  Open by default / Open supported links if it opens in the browser.
- **iOS/iPadOS:** Home Screen web apps do **not** register as Universal Link
  handlers. Links tapped in Messages or other apps open in the browser, not
  automatically in the installed web app. In-scope navigation *from within*
  the Home Screen app stays in the app. The optional `webapp://` launcher in
  the Copy link menu is undocumented and may open the app at its inbox or fail;
  it is **not a thread deep link**. The HTTPS URL is the reliable way to reach
  a specific thread in the browser.
- **Desktop Chromium:** Recent Chrome versions can capture in-scope links to
  an installed PWA unless the user opts out. Other browsers may ignore the
  launch handler.

To verify on a device: install the HTTPS dashboard as an app, sign in, open an
existing `/threads/<thread-id>` link **from a different app**, then check both
whether the PWA launched and whether the intended thread opened. Repeat with
the PWA already running. On Android test both the WebAPK and a plain shortcut,
where available. On iOS test a Messages link separately from a same-origin
link clicked inside the Home Screen app. A desktop browser or device emulator
can validate the manifest and route but cannot prove native OS URL capture.

References: [Chrome navigation management](https://developer.chrome.com/docs/capabilities/pwa-navigation-management),
[web.dev OS integration](https://web.dev/learn/pwa/os-integration),
[Apple's web app scope behavior](https://developer.apple.com/videos/play/wwdc2023/10120/).

## Deployment

The backend is deployed from the root Go module with `cmd/api/vercel.json`. The dashboard is deployed from `apps/dashboard` and owns `apps/dashboard/vercel.json`.

```bash
# backend, from repo root
vercel --prod --yes -A cmd/api/vercel.json
go run ./cmd/migrate

# dashboard
cd apps/dashboard
vercel --prod --yes
```

Set `AGENTBOX_BACKEND_URL` on the dashboard deployment to the backend origin. Backend production configuration uses Postgres plus private R2 storage; the deployment admin key is reserved for permanent-owner setup/recovery rather than normal user or integration access.
