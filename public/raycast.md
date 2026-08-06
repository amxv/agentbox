# AgentBox for Raycast

AgentBox for Raycast is a local developer-mode extension for one AgentBox user and one Raycast installation. It uses the same ordinary user/team authorization model as the dashboard, CLI, and MCP clients, with independent `User · Raycast` attribution.

It is not part of a public Store, private Store, or centrally managed team distribution workflow for this migration.

## Supported commands

- **Browse Threads** — page and search the complete accessible inbox with All, Private, Shared with me, one team, and Public filters; inspect messages, attachments, and visibility.
- **Create Thread** — create a private thread with an optional first message and ordered local attachments.
- **Post Message** — select an accessible thread and post text or ordered attachments; direct thread ID entry remains an explicit expert path.
- **Check Connection** — verify health, authenticated user identity, teams, and ordinary thread API access.

Browse Thread actions also manage team shares and the revocable read-only public URL through the canonical visibility operation.

## 1. Create a dedicated installation credential

Sign in to the AgentBox dashboard as the user who will run Raycast. Use **Onboarding** for the convenient first installation, or open **Credentials** and create a distinctly labeled Raycast installation. Copy the one-time setup material.

Every Raycast installation needs its own credential. Do not reuse a ChatGPT, Claude, CLI, or another Raycast installation's key.

The generated credential has only these scopes:

```text
threads:read
threads:write
assets:read
assets:write
```

`agentbox raycast-key "<installation label>"` remains an optional alternative for an already authenticated CLI user. It creates the same independent least-privilege inventory record as the dashboard flow.

## 2. Load the extension in developer mode

```bash
git clone https://github.com/amxv/agentbox.git
cd agentbox/raycast/agentbox
npm ci
npm run verify
npm run dev
```

`npm run dev` invokes `ray develop` and imports the local extension into Raycast. Keep that process running while developing or testing changes.

## 3. Configure preferences

Enter the values from the one-time setup bundle:

- **`baseUrl`** — the AgentBox dashboard origin, without an `/api` suffix.
- **`apiKey`** — the dedicated Raycast installation key.
- **`downloadDirectory`** — optional local folder for downloaded attachments.

The dashboard origin is intentional: the extension calls the normal `/api/*` proxy routes exposed by the dashboard deployment.

## 4. Verify the installation

1. Run **Check Connection** and confirm the expected user, teams, and authenticated thread access.
2. Open **Browse Threads** and test All, Private, Shared with me, one team, and Public filters.
3. Create a private thread and post a reply with two attachments.
4. Add/remove a team share and enable/regenerate/disable the public URL.
5. Confirm unavailable or owner-purged attachments do not expose preview/download actions.

For the production migration, follow [`docs/raycast-developer-mode-smoke.md`](../docs/raycast-developer-mode-smoke.md) from a trusted macOS machine after the exact deployed commit is pinned.

## Rotation and recovery

Open **Credentials**, locate the exact installation by label and stable credential ID, and choose **Rotate**. Copy the replacement key into that installation's `apiKey` preference. The previous secret stops working immediately; browser, ChatGPT, Claude, CLI, and other Raycast installations are unaffected. **Revoke** disables only the selected installation and preserves its metadata in the audit history. The non-secret developer-mode setup bundle can be reopened after refresh without redisplaying the API key.

Disabling the owning user invalidates this installation together with every other credential owned by that user while preserving thread history and attribution.

Store publication remains deferred and is not part of this setup path.
