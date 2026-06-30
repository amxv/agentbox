import { proxyToGoBackend } from "../../../_proxy/proxy";

export async function DELETE(request: Request, context: { params: Promise<{ name: string }> }) {
  const { name } = await context.params;
  return proxyToGoBackend({ path: `/api/admin/keys/${encodeURIComponent(name)}`, request });
}
