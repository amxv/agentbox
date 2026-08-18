import { proxyToGoBackend } from "../../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(request: Request) {
  const url = new URL(request.url);
  return proxyToGoBackend({ path: `/api/owner/teams${url.search}`, request });
}

export async function POST(request: Request) {
  return proxyToGoBackend({ path: "/api/owner/teams", request });
}
