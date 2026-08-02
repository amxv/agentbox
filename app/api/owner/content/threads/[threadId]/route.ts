import { proxyToGoBackend } from "../../../../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(
  request: Request,
  context: { params: Promise<{ threadId: string }> }
) {
  const { threadId } = await context.params;
  return proxyToGoBackend({
    path: `/api/owner/content/threads/${encodeURIComponent(threadId)}`,
    request
  });
}
