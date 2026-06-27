import { requireActor } from "@/src/core/auth";
import { errorJson, json, parseJson } from "@/src/core/http";
import { createThreadSchema } from "@/src/core/schemas";
import { handleCreateThread, handleListThreads } from "@/src/core/handlers";

export const runtime = "nodejs";

export async function GET(request: Request) {
  try {
    requireActor(request);
    const url = new URL(request.url);
    const limit = Number(url.searchParams.get("limit") ?? "50");
    return json(await handleListThreads(limit));
  } catch (error) {
    if (error instanceof Response) return error;
    return errorJson(error instanceof Error ? error.message : "Failed to list threads.", 500);
  }
}

export async function POST(request: Request) {
  try {
    const actor = requireActor(request);
    const input = createThreadSchema.parse(await parseJson(request));
    return json(await handleCreateThread(actor, input.title), { status: 201 });
  } catch (error) {
    if (error instanceof Response) return error;
    return errorJson(error instanceof Error ? error.message : "Failed to create thread.", 400);
  }
}
