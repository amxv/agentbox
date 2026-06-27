import { requireActor } from "@/src/core/auth";
import { errorJson, json } from "@/src/core/http";
import { handleGetThread } from "@/src/core/handlers";

export const runtime = "nodejs";

type Params = { params: Promise<{ threadId: string }> };

export async function GET(request: Request, { params }: Params) {
  try {
    requireActor(request);
    const { threadId } = await params;
    return json(await handleGetThread(threadId));
  } catch (error) {
    if (error instanceof Response) return error;
    const message = error instanceof Error ? error.message : "Failed to get thread.";
    return errorJson(message, message === "Thread not found." ? 404 : 500);
  }
}
