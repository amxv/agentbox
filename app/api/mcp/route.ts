import { WebStandardStreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/webStandardStreamableHttp.js";
import { requireActor, validateOrigin } from "@/src/core/auth";
import { createAgentboxMcpServer } from "@/src/core/mcp";

export const runtime = "nodejs";

async function handleMcp(request: Request) {
  const originError = validateOrigin(request);
  if (originError) return originError;

  try {
    const actor = requireActor(request);
    const server = createAgentboxMcpServer(actor);
    const transport = new WebStandardStreamableHTTPServerTransport({
      sessionIdGenerator: undefined,
      enableJsonResponse: true
    });

    await server.connect(transport);
    return await transport.handleRequest(request);
  } catch (error) {
    if (error instanceof Response) return error;
    return Response.json({ error: error instanceof Error ? error.message : "MCP request failed." }, { status: 500 });
  }
}

export async function GET(request: Request) {
  return handleMcp(request);
}

export async function POST(request: Request) {
  return handleMcp(request);
}

export async function DELETE(request: Request) {
  return handleMcp(request);
}
