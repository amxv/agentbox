import { proxyToGoBackend } from "../../../_proxy/proxy";

export const runtime = "nodejs";

export async function POST(
  request: Request,
  context: { params: Promise<{ connector: string }> }
) {
  const { connector } = await context.params;
  return proxyToGoBackend({
    path: `/api/onboarding/connectors/${encodeURIComponent(connector)}`,
    request
  });
}
