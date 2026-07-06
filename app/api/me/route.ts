import { proxyToGoBackend } from "../_proxy/proxy";

export async function GET(request: Request) {
  return proxyToGoBackend({ path: "/api/me", request });
}
