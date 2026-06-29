export function json(data: unknown, init?: ResponseInit): Response {
  return Response.json(data, init);
}

export function errorJson(message: string, status = 400): Response {
  return json({ error: message }, { status });
}

export async function parseJson<T = unknown>(request: Request): Promise<T> {
  try {
    return (await request.json()) as T;
  } catch {
    throw new Error("Expected a JSON request body.");
  }
}
