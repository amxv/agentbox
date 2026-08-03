# AgentBox for Raycast

AgentBox for Raycast is a **developer-mode extension** for one AgentBox user and one local Raycast installation. It is not a public Raycast Store package.

The extension uses the same deployment-global user/team authorization model as the dashboard, CLI, and other normal clients. A Raycast credential acts for one user, is attributed as `Raycast`, and can only use ordinary thread and attachment APIs. It cannot call MCP or owner-browser-only routes.

## Supported workflows

- Browse and search every thread currently accessible to your user.
- Filter by All, Private to Me, Shared with Me, one of your teams, or Public.
- Inspect chronological messages with stable `User · Actor` attribution.
- Create a private thread with an optional first message and attachments.
- Select an accessible thread and post a reply with ordered attachments.
- Download authorized attachments and see explicit deleted or unavailable states.
- Share or unshare a thread with teams and manage its revocable read-only public URL.
- Check health, authenticated identity, team access, and thread API access.

## Create a dedicated installation credential

Use the web dashboard as the primary setup path:

1. Sign in as the user who will use this Raycast installation.
2. Open **Onboarding** or **Credentials**.
3. Open the **Raycast** connector card and create a connection.
4. Copy the one-time setup material immediately. The full API key is not stored or shown again.

Each Raycast installation must have its **own** credential. Do not reuse a ChatGPT, Claude, CLI, or another Raycast installation's key. Do not send the generated key to another person or commit it to a repository.

The generated Raycast key has only these scopes:

```text
threads:read
threads:write
assets:read
assets:write
```

`agentbox raycast-key` remains available as an alternative user-scoped setup path, but dashboard onboarding is the canonical flow.

## Install in Raycast developer mode

On the macOS machine running Raycast, use the exact repository revision deployed for AgentBox:

```bash
cd raycast/agentbox
npm ci
npm run verify
npm run dev
```

`npm run dev` invokes `ray develop` and imports the local extension into Raycast. Keep the development process running while testing changes.

Set these Raycast extension preferences from the one-time onboarding material:

| Preference | Required | Value |
| --- | --- | --- |
| `baseUrl` | Yes | The AgentBox dashboard origin, without an `/api` suffix. |
| `apiKey` | Yes | The dedicated one-time Raycast installation key. |
| `downloadDirectory` | No | A local directory for downloaded attachments. |

The dashboard origin is intentional: the extension calls the normal `/api/*` proxy routes exposed by the dashboard deployment.

## Credential-free verification

These checks use fake HTTP responses and local package tooling. They do not require AgentBox, PostgreSQL, R2, Vercel, Raycast account, or production credentials:

```bash
npm ci
npm run test
npm run typecheck
CI=1 NO_COLOR=1 npm run lint
CI=1 NO_COLOR=1 npm run build
```

`npm run test` verifies page traversal, all five visibility filters, user/team metadata, visibility mutations, self-revocation rules, ordered uploads, duplicate attachment names, signed and tombstoned attachments, coded errors, and guards against tenant-era, storage-key, direct attachment-public-URL, or MCP-key assumptions.

## Local macOS smoke checklist

Run this only with a dedicated non-production test user or during the credentialed production cutover described in `docs/user-team-sharing-production-cutover.md`:

1. Launch **Check Connection** and verify health, user identity, teams, and authenticated thread access.
2. Open **Browse Threads** and traverse more than one page under All, Private, Shared, one team, and Public filters.
3. Search for a thread beyond the first page and confirm visibility plus `User · Actor` attribution.
4. Create a private thread, then post a reply with two attachments, including duplicate basenames from different directories.
5. Download an attachment and verify a purged or missing object shows an unavailable state instead of a download action.
6. Add and remove team shares, enable/disable the public URL, and regenerate it once. Confirm the previous URL stops working.
7. With a non-owner test user, remove the final team access path only after the warning, then confirm the thread disappears and direct read/post/download calls fail.
8. Rotate only this Raycast connection, replace the preference with the new key, and confirm the old key fails while other user credentials continue to work.
9. Disable the non-owner test user from the owner dashboard and confirm every credential for that user, including Raycast, stops authenticating.

## Rotation, revocation, and recovery

- Reopen **Onboarding** or **Credentials**, reconnect the Raycast connector, and copy the new one-time key.
- Replace `apiKey` in this installation's Raycast preferences.
- The prior Raycast key is revoked immediately; other installations and connector keys are unaffected.
- Disabling the user revokes effective access for every credential belonging to that user.
- If a key was lost before it was entered, create a new Raycast connection. AgentBox cannot redisplay the old secret.

Do not publish this extension as part of the user/team migration. Public, private, or centrally managed Store distribution remains deferred; production import and smoke verification are reserved for the credentialed local Phase 20 handoff.
