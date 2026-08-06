import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

export async function PATCH(
  request: Request,
  context: { params: Promise<{ id: string }> }
) {
  const { id } = await context.params;
  return proxyToGoBackend({
    path: `/api/owner/teams/${encodeURIComponent(id)}`,
    request
  });
}
