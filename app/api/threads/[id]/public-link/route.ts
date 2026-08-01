import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

type Context = { params: Promise<{ id: string }> };

async function proxy(request: Request, context: Context) {
  const { id } = await context.params;
  return proxyToGoBackend({
    path: `/api/threads/${encodeURIComponent(id)}/public-link`,
    request
  });
}

export async function GET(request: Request, context: Context) {
  return proxy(request, context);
}

export async function POST(request: Request, context: Context) {
  return proxy(request, context);
}

export async function DELETE(request: Request, context: Context) {
  return proxy(request, context);
}
