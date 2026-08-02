import { proxyToGoBackend } from "../../../../../../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(
  request: Request,
  context: { params: Promise<{ token: string; assetId: string }> }
) {
  const { token, assetId } = await context.params;
  return proxyToGoBackend({
    path: `/api/public/threads/${encodeURIComponent(token)}/assets/${encodeURIComponent(assetId)}/preview`,
    request
  });
}
