import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

type Context = { params: Promise<{ threadId: string }> };

async function proxy(request: Request, context: Context) {
  const { threadId } = await context.params;
  return proxyToGoBackend({
    path: `/api/threads/${encodeURIComponent(threadId)}/visibility`,
    request
  });
}

export async function GET(request: Request, context: Context) {
  return proxy(request, context);
}

export async function PATCH(request: Request, context: Context) {
  return proxy(request, context);
}
