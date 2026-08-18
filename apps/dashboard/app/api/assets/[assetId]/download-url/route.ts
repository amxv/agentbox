import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

type Params = { params: Promise<{ assetId: string }> };

export async function GET(request: Request, { params }: Params) {
  const { assetId } = await params;
  const url = new URL(request.url);
  return proxyToGoBackend({ path: `/api/assets/${encodeURIComponent(assetId)}/download-url${url.search}`, request });
}
