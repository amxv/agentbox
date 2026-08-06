import { proxyToGoBackend } from "../../../../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(
  request: Request,
  context: { params: Promise<{ id: string; action: string }> }
) {
  const { id, action } = await context.params;
  const url = new URL(request.url);
  return proxyToGoBackend({
    path: `/api/owner/users/${encodeURIComponent(id)}/${encodeURIComponent(action)}${url.search}`,
    request
  });
}

export async function POST(
  request: Request,
  context: { params: Promise<{ id: string; action: string }> }
) {
  const { id, action } = await context.params;
  return proxyToGoBackend({
    path: `/api/owner/users/${encodeURIComponent(id)}/${encodeURIComponent(action)}`,
    request
  });
}
