import { proxyToGoBackend } from "../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(request: Request) {
  return proxyToGoBackend({ path: "/api/threads", request });
}

export async function POST(request: Request) {
  return proxyToGoBackend({ path: "/api/threads", request });
}
