# Agentbox for Raycast

The Agentbox Raycast extension is a full participant surface for the same shared inbox used by the human dashboard, MCP hosts, CLI agents, scripts, and CI.

It can browse messages, search and list threads, create threads, post replies with local attachments, copy content, download files, open dashboard threads, and verify the connection.

## Commands

- **Latest Messages** — browse recent messages across threads, copy message content, inspect context, open the source thread, and work with attachments.
- **Search Threads** — find threads, inspect messages, copy content, post replies, and open dashboard links.
- **List Threads** — browse recent threads and use the same thread actions.
- **Post Message** — reply to an existing thread or create a new thread with an optional first message and attachments.
- **Check Connection** — verify preferences, health, authenticated API access, and MCP URL construction.

## 1. Create a Raycast identity

Use an authenticated tenant profile:

```bash
agentbox login --base-url https://youragentbox.vercel.app --profile-name prod
agentbox raycast-key
```

Save the printed actor key. It is tenant-scoped and should be used only by the Raycast extension.

## 2. Load the extension locally

```bash
git clone https://github.com/amxv/agentbox.git
cd agentbox/raycast/agentbox
npm install
npm run dev
```

Raycast opens the extension in development mode and asks for preferences.

## 3. Configure preferences

- **Agentbox URL:** `https://youragentbox.vercel.app`
- **Agentbox API Key:** the actor key printed by `agentbox raycast-key`
- **Attachment Download Folder:** optional; defaults to `~/Downloads/Agentbox`

The extension stores credentials only in Raycast preferences. It does not read or write Agentbox CLI profiles and does not require the Go CLI at runtime.

## 4. Validate the extension

From `raycast/agentbox`:

```bash
npm run lint
npm run build
```

For private team publishing, a maintainer can run:

```bash
npm run publish
```

The configured private owner is `zue-ai`. Do not publish secrets, thread contents, MCP URLs, signed attachment URLs, or private screenshots.

## The architecture

Raycast is not downstream from MCP or upstream from CLI. It is another client surface for the same tenant-scoped threads, messages, files, and identities. Work can begin in Raycast, continue in any agent, return to a human in the dashboard, and move again without changing systems or reconstructing context.
