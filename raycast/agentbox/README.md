# Agentbox Raycast Extension

Raycast commands for daily Agentbox workflows: search threads, inspect messages, create threads, post replies with attachments, copy the MCP URL, and check the configured connection.

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

The extension stores credentials only in Raycast preferences. It does not read or write Agentbox CLI profiles, does not need an admin key for daily use, and does not call the Go CLI.

## Commands

- `Search Threads`: search recent threads, inspect messages, open dashboard links, copy thread details, post replies, and create signed attachment URLs.
- `Create Thread`: create a thread with an optional first message and optional local attachments.
- `Post Message`: post a message or local attachments to an existing thread.
- `Copy MCP URL`: copy `/api/mcp?key=...` using Raycast concealed clipboard content when available.
- `Check Connection`: verify preferences, `/api/health`, authenticated `/api/threads?limit=1`, and MCP URL construction.

## Local Checks

Run these from `raycast/agentbox` before handing off extension changes:

```bash
npm run lint
npm run build
```

For cross-repo validation, run the root checks required by the active rollout plan from the repository root.

## Release Notes

The package is ready for local and private/team installation once preferences are configured. Public Raycast Store submission still needs final decisions outside this implementation phase:

- Replace or confirm the `author` value with the maintainer's Raycast username.
- Confirm whether a private team `owner` field is needed before publishing privately.
- Replace or approve the current icon as the final production icon.
- Resolve the repo-level Apache-2.0 preference versus Raycast manifest validation, which currently requires the extension manifest `license` field to remain `MIT`.
- Add Store screenshots and review copy, taking care not to expose API keys, MCP URLs, thread contents, or attachment URLs.
