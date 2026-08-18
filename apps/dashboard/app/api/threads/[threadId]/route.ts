import { proxyToGoBackend } from "../../_proxy/proxy";

export const runtime = "nodejs";

type Params = { params: Promise<{ threadId: string }> };

export async function GET(request: Request, { params }: Params) {
  const { threadId } = await params;
  return proxyToGoBackend({ path: `/api/threads/${encodeURIComponent(threadId)}`, request });
}
