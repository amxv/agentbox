import { proxyToGoBackend } from "../../../_proxy/proxy";

type Params = { params: Promise<{ threadId: string }> };

export async function POST(request: Request, { params }: Params) {
  const { threadId } = await params;
  return proxyToGoBackend({ path: `/api/threads/${encodeURIComponent(threadId)}/uploads`, request });
}
