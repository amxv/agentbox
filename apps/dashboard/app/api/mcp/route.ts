import { proxyToGoBackend } from "../_proxy/proxy";

export const runtime = "nodejs";

export async function GET(request: Request) {
  return proxyToGoBackend({ path: "/api/mcp", request });
}

export async function POST(request: Request) {
  return proxyToGoBackend({ path: "/api/mcp", request });
}

export async function DELETE(request: Request) {
  return proxyToGoBackend({ path: "/api/mcp", request });
}
