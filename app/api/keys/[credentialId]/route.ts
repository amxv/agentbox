import { proxyToGoBackend } from "../../_proxy/proxy";

export const runtime = "nodejs";

type Params = { params: Promise<{ credentialId: string }> };

async function proxy(request: Request, { params }: Params) {
  const { credentialId } = await params;
  return proxyToGoBackend({ path: `/api/keys/${encodeURIComponent(credentialId)}`, request });
}

export async function PATCH(request: Request, context: Params) {
  return proxy(request, context);
}

export async function DELETE(request: Request, context: Params) {
  return proxy(request, context);
}
