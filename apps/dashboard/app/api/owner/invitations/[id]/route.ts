import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

export async function DELETE(request: Request, context: { params: Promise<{ id: string }> }) {
  const { id } = await context.params;
  return proxyToGoBackend({ path: `/api/owner/invitations/${encodeURIComponent(id)}`, request });
}
