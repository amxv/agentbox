import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { createThreadSchema, fileParamInputSchema, postMessageSchema } from "./schemas";
import { handleCreateThread, handleGetThread, handleListThreads, handlePostMessage } from "./handlers";
import type { Actor } from "./types";

function mcpResult(text: string, structuredContent: Record<string, unknown>) {
  return {
    content: [{ type: "text" as const, text }],
    structuredContent
  };
}


export function createAgentboxMcpServer(actor: Actor): McpServer {
  const server = new McpServer({
    name: "agentbox",
    version: "0.1.0"
  });

  server.registerTool(
    "list_threads",
    {
      title: "List threads",
      description: "List recent Agentbox threads.",
      inputSchema: { limit: z.number().int().positive().max(100).optional() },
      outputSchema: { threads: z.array(z.any()) },
      annotations: { readOnlyHint: true, destructiveHint: false, openWorldHint: false }
    },
    async ({ limit }) => mcpResult("Listed Agentbox threads.", await handleListThreads(limit))
  );

  server.registerTool(
    "get_thread",
    {
      title: "Get thread",
      description: "Read an Agentbox thread and its messages.",
      inputSchema: { thread_id: z.string().min(1) },
      outputSchema: { thread: z.any() },
      annotations: { readOnlyHint: true, destructiveHint: false, openWorldHint: false }
    },
    async ({ thread_id }) => mcpResult("Fetched Agentbox thread.", await handleGetThread(thread_id))
  );

  server.registerTool(
    "create_thread",
    {
      title: "Create thread",
      description: "Create a new Agentbox thread.",
      inputSchema: createThreadSchema.shape,
      outputSchema: { thread: z.any() },
      annotations: { readOnlyHint: false, destructiveHint: false, openWorldHint: true }
    },
    async ({ title }) => mcpResult("Created Agentbox thread.", await handleCreateThread(actor, title))
  );

  server.registerTool(
    "post_message",
    {
      title: "Post message",
      description: "Post a Markdown message to an Agentbox thread. To attach a file from ChatGPT, pass the uploaded conversation file ID, for example file_abc123. Do not pass a local filesystem path or plain filename.",
      inputSchema: postMessageSchema.shape,
      outputSchema: { message: z.any() },
      annotations: { readOnlyHint: false, destructiveHint: false, openWorldHint: true },
      _meta: {
        "openai/fileParams": ["file"],
        "openai/toolInvocation/invoking": "Posting to Agentbox…",
        "openai/toolInvocation/invoked": "Posted to Agentbox"
      }
    },
    async ({ thread_id, body, file }) => mcpResult(
      "Posted message to Agentbox.",
      await handlePostMessage(actor, {
        threadId: thread_id,
        body,
        file: file ? fileParamInputSchema.parse(file) : null
      })
    )
  );

  return server;
}
