import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(
  request: Request,
  context: { params: Promise<{ token: string }> }
) {
  const { token } = await context.params;
  return proxyToGoBackend({
    path: `/api/public/threads/${encodeURIComponent(token)}`,
    request
  });
}
