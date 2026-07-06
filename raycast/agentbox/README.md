# Agentbox Raycast Extension

Raycast commands for daily Agentbox workflows: browse latest messages, search threads, inspect messages, create threads, post replies with attachments, copy content to the clipboard, and check the configured connection.

This package is intentionally self-contained under `raycast/agentbox`. It uses npm and the Raycast extension CLI, talks to Agentbox through the existing HTTP API, and does not require the Go CLI or any bundled native binary at runtime.

## Setup

1. Install dependencies from this directory:

   ```bash
   npm install
   ```

2. Open the extension in Raycast development mode:

   ```bash
   npm run dev
   ```

3. Configure the Raycast extension preferences:

   - `Agentbox URL`: the Agentbox dashboard or API proxy URL. For production, use `https://agentbox-black.vercel.app`.
   - `Agentbox API Key`: an actor API key that can list, create, and update threads.
   - `Attachment Download Folder`: optional folder for attachment download actions. If unset, downloads go to `~/Downloads/Agentbox`.

Each user needs to configure their own Agentbox URL and API key in Raycast preferences. The extension stores credentials only in Raycast preferences. It does not read or write Agentbox CLI profiles, does not need an admin key for daily use, and does not call the Go CLI.

## Sharing and Distribution

For local or development installs, share the repository checkout. From `raycast/agentbox`, each user runs:

```bash
npm install
npm run dev
```

Raycast will load the extension in development mode and prompt for that user's preferences.

For private team distribution, this package is configured with Raycast owner `zue-ai`. After the branch is integrated and checks pass, a maintainer can publish it to the `zue-ai` private Raycast Store from `raycast/agentbox`:

```bash
npm run publish
```

Do not publish from feature branches. Before publishing, verify the Raycast organization handle is still `zue-ai`, the `owner` field is present in `package.json`, the extension metadata is acceptable for the private Store, and no API keys, MCP URLs, thread contents, or signed attachment URLs are present in code, docs, screenshots, or release assets.

The Raycast free team plan allows 5 commands across organization extensions. This private package intentionally publishes the five daily commands and leaves MCP URL generation to the Agentbox CLI:

```bash
agentbox mcp-url
```

Public Raycast Store distribution can happen later through Raycast's public publish pull request flow. Keep private/team publishing separate from the public Store metadata and review process.

## Commands

- `Latest Messages`: browse recent messages across threads, press Enter to copy message content, inspect context, open the source thread, and work with attachments.
- `Search Threads`: search recent threads, press Enter to copy the visible thread/message content, inspect messages, open dashboard links, copy thread details, post replies, and work with attachments.
- `Create Thread`: create a thread with an optional first message and optional local attachments.
- `Post Message`: post a message or local attachments to an existing thread.
- `Check Connection`: verify preferences, `/api/health`, authenticated `/api/threads?limit=1`, and MCP URL construction.

## Local Checks

Run these from `raycast/agentbox` before handing off extension changes:

```bash
npm run lint
npm run build
```

For cross-repo validation, run the root checks required by the active rollout plan from the repository root.

## Release Notes

The package is ready for local and private/team installation once preferences are configured. Private `zue-ai` Store publishing should be done manually by the lead after integration. Public Raycast Store submission still needs final decisions outside this implementation phase:

- Replace or confirm the `author` value with the maintainer's Raycast username.
- Replace or approve the current icon as the final production icon.
- Resolve the repo-level Apache-2.0 preference versus Raycast manifest validation, which currently requires the extension manifest `license` field to remain `MIT`.
- Add Store screenshots and review copy, taking care not to expose API keys, MCP URLs, thread contents, or attachment URLs.
