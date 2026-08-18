import { proxyToGoBackend } from "../../_proxy/proxy";

export const runtime = "nodejs";

type Params = { params: Promise<{ messageId: string }> };

export async function GET(request: Request, { params }: Params) {
  const { messageId } = await params;
  return proxyToGoBackend({ path: `/api/messages/${encodeURIComponent(messageId)}`, request });
}
