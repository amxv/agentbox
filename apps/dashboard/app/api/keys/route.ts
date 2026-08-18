import { proxyToGoBackend } from "../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(request: Request) {
  return proxyToGoBackend({ path: "/api/keys", request });
}

export async function POST(request: Request) {
  return proxyToGoBackend({ path: "/api/keys", request });
}
