import { proxyToGoBackend } from "../../../../../_proxy/proxy";

export const runtime = "nodejs";

type Context = { params: Promise<{ id: string; userId: string }> };

async function proxy(request: Request, context: Context) {
  const { id, userId } = await context.params;
  return proxyToGoBackend({
    path: `/api/owner/teams/${encodeURIComponent(id)}/members/${encodeURIComponent(userId)}`,
    request
  });
}

export async function PUT(request: Request, context: Context) {
  return proxy(request, context);
}

export async function DELETE(request: Request, context: Context) {
  return proxy(request, context);
}
