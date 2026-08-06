# Raycast developer-mode production smoke

Run this only from the credentialed local Phase 20 machine after the exact AgentBox backend/dashboard commit is deployed and the production database has migrated through the final checked-in migration. It requires macOS, Raycast, a signed-in ordinary AgentBox test user, and access to the dashboard's Onboarding or Credentials page.

Do not record API keys, setup bundles, signed attachment URLs, cookies, thread bodies, or production user data in logs or screenshots.

## 1. Pin and verify the exact source

```bash
git switch feat/user-team-sharing
git pull --ff-only origin feat/user-team-sharing
git status --short --branch
git log -1 --oneline

cd raycast/agentbox
npm ci
CI=1 NO_COLOR=1 npm run verify
```

The commit must match the deployed backend/dashboard and the final Phase 19 checkpoint. Stop on any local change or failed package check.

## 2. Create one installation credential

1. Sign in to the dashboard as an ordinary disposable test user.
2. Open **Onboarding** or **Credentials** and choose **Connect Raycast**.
3. Record only the credential label/purpose and creation timestamp in the evidence log.
4. Copy the one-time `baseUrl` and `apiKey` directly into Raycast preferences.
5. Confirm this installation does not reuse a browser, ChatGPT, Claude, CLI, or another Raycast credential.

The credential must have only `threads:read`, `threads:write`, `assets:read`, and `assets:write` scopes.

## 3. Import in developer mode

From `raycast/agentbox`:

```bash
npm run dev
```

Raycast should import four commands:

```text
Browse Threads
Create Thread
Post Message
Check Connection
```

Configure:

```text
baseUrl=<dashboard origin from setup bundle>
apiKey=<dedicated Raycast installation key>
downloadDirectory=<optional test folder>
```

Keep `npm run dev` active while executing the smoke matrix.

## 4. Identity and access diagnostics

Run **Check Connection** and verify:

- health succeeds through the dashboard `/api/*` proxy;
- the expected deployment-global user is returned;
- actor attribution is `Raycast`;
- caller teams match the dashboard;
- ordinary thread access succeeds;
- no tenant metadata or MCP URL is shown;
- the credential cannot call owner content or owner administration routes.

## 5. Complete inbox traversal

Seed enough accessible threads to require more than one page. In **Browse Threads**:

1. Traverse All, Private to Me, Shared with Me, each caller team, and Public.
2. Compare the ordered thread IDs with the dashboard/API for the same user.
3. Search for a result beyond the first page.
4. Verify no duplicate/omitted thread appears when one thread matches overlapping teams.
5. Confirm cards show message count, latest preview, visibility, matched teams, and stable `User · Actor` attribution.

Public status alone must not make another user's inaccessible thread appear.

## 6. Create, reply, and attachments

1. Create a private thread with an initial Markdown message.
2. Attach two files and verify message/attachment order.
3. Reply through the standalone accessible-thread picker and from a Browse Threads contextual action.
4. Attach two files from different directories with the same basename and verify the displayed names are deterministically disambiguated without changing byte/order association.
5. Preview/download a healthy attachment through a short-lived signed URL.
6. Verify an owner-purged attachment shows `Attachment deleted by deployment owner` with no preview/download action.
7. Verify a missing object shows `Attachment unavailable` with no fallback direct URL.
8. Attempt a known foreign thread/asset ID through the expert path and require the normal non-disclosing denial.

## 7. Visibility and self-revocation

For an accessible team-shared thread:

1. Add a team the caller currently belongs to.
2. Remove another existing team share.
3. Enable the public URL, copy/open it, regenerate it, and prove the old URL fails.
4. Disable public sharing and prove the active URL fails.
5. Remove the caller's final team-derived access path only after the warning.
6. Confirm the mutation succeeds, the thread disappears immediately from Raycast, and the next direct read/post/download is denied.

Thread owners must never receive a self-revocation warning for team removal because ownership remains an access path.

## 8. Rotation, isolation, and disablement

1. Reconnect/rotate only this Raycast installation from Onboarding or Credentials.
2. Replace `apiKey` in Raycast preferences and confirm the new key works.
3. Confirm the old Raycast key fails immediately.
4. Confirm the user's browser, ChatGPT, Claude, CLI, and any second Raycast installation continue working.
5. With the deployment owner, disable the disposable test user.
6. Confirm this Raycast installation and every other credential for that user fail authentication while preserved shared history remains visible to still-qualified users.
7. Confirm an owner-owned Raycast credential still cannot read another user's private thread through the owner-browser-only bypass.

## 9. Evidence and cleanup

Record:

- exact Git commit;
- Raycast and Node versions;
- package verification result;
- sanitized command/filter/result counts;
- thread/asset test IDs only when they contain no secret material;
- rotation, self-revocation, owner non-bypass, and disablement outcomes;
- any defect and its exact reproduction steps.

Remove disposable download files, stop `npm run dev`, revoke the test Raycast credential, and delete local notes containing one-time setup material. If a code defect is found, keep production maintenance mode enabled where applicable, make the narrowest fix on `feat/user-team-sharing`, update the blueprint, push normally, wait for exact-head CI, redeploy, and repeat the blocked checks.
