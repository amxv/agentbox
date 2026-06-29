import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

type Params = { params: Promise<{ assetId: string }> };

export async function GET(request: Request, { params }: Params) {
  const { assetId } = await params;
  return proxyToGoBackend({ path: `/api/assets/${encodeURIComponent(assetId)}/download-url`, request });
}
