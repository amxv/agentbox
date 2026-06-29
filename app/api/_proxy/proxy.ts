const backendUrl = process.env.AGENTBOX_BACKEND_URL ?? process.env.AGENTBOX_GO_BACKEND_URL;

type ProxyContext = {
  path: string;
  request: Request;
};

export async function proxyToGoBackend({ path, request }: ProxyContext) {
  if (!backendUrl) {
    return Response.json({
      error: "AGENTBOX_BACKEND_URL or AGENTBOX_GO_BACKEND_URL must be set to route API requests to the Go backend."
    }, { status: 502 });
  }

  const incoming = new URL(request.url);
  const target = new URL(path, `${backendUrl.replace(/\/+$/, "")}/`);
  target.search = incoming.search;

  const headers = new Headers(request.headers);
  headers.delete("host");
  headers.delete("content-length");

  const init: RequestInit & { duplex?: "half" } = {
    method: request.method,
    headers,
    redirect: "manual"
  };

  if (request.method !== "GET" && request.method !== "HEAD") {
    init.body = request.body;
    init.duplex = "half";
  }

  const response = await fetch(target, init);
  const responseHeaders = new Headers(response.headers);
  responseHeaders.delete("content-encoding");
  responseHeaders.delete("content-length");

  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers: responseHeaders
  });
}
