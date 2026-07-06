import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

export async function POST(request: Request) {
  return proxyToGoBackend({ path: "/api/auth/cli/authorize", request });
}
