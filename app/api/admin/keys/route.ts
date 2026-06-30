import { proxyToGoBackend } from "../../_proxy/proxy";

export async function GET(request: Request) {
  return proxyToGoBackend({ path: "/api/admin/keys", request });
}

export async function POST(request: Request) {
  return proxyToGoBackend({ path: "/api/admin/keys", request });
}
