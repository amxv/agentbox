import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(request: Request) {
  const url = new URL(request.url);
  return proxyToGoBackend({
    path: `/api/owner/content/search${url.search}`,
    request
  });
}
