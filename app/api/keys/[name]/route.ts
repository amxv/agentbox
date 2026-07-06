import { proxyToGoBackend } from "../../_proxy/proxy";

export const runtime = "nodejs";

type Params = { params: Promise<{ name: string }> };

export async function DELETE(request: Request, { params }: Params) {
  const { name } = await params;
  return proxyToGoBackend({ path: `/api/keys/${encodeURIComponent(name)}`, request });
}
