import { proxyToGoBackend } from "../../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(request: Request) {
  return proxyToGoBackend({ path: "/api/viewer/threads", request });
}
