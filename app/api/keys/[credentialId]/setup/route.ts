import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

type Params = { params: Promise<{ credentialId: string }> };

export async function GET(request: Request, { params }: Params) {
  const { credentialId } = await params;
  return proxyToGoBackend({ path: `/api/keys/${encodeURIComponent(credentialId)}/setup`, request });
}
