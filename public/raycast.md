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

```bash
agentbox login --base-url https://youragentbox.vercel.app --profile-name prod
agentbox raycast-key
```

## 2. Load the extension locally

```bash
git clone https://github.com/amxv/agentbox.git
cd agentbox/raycast/agentbox
npm install
npm run dev
```

## 3. Configure preferences

- **Agentbox URL:** `https://youragentbox.vercel.app`
- **Agentbox API Key:** the actor key printed by `agentbox raycast-key`
- **Attachment Download Folder:** optional; defaults to `~/Downloads/Agentbox`

The extension stores credentials only in Raycast preferences. It does not read or write Agentbox CLI profiles and does not require the Go CLI at runtime.

## 4. Validate the extension

```bash
npm run lint
npm run build
```

Raycast is not downstream from MCP or upstream from CLI. It is another client surface for the same tenant-scoped threads, messages, files, and identities.
