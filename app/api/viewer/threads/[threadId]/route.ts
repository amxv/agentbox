import { requireAdminRequest } from "@/src/core/admin";
import { getThread } from "@/src/core/db";
import { errorJson, json } from "@/src/core/http";

export const runtime = "nodejs";

type Params = { params: Promise<{ threadId: string }> };

export async function GET(request: Request, { params }: Params) {
  try {
    requireAdminRequest(request);
    const { threadId } = await params;
    const thread = await getThread(threadId);
    if (!thread) return errorJson("Thread not found.", 404);
    return json({ thread });
  } catch (error) {
    if (error instanceof Response) return error;
    const message = error instanceof Error ? error.message : "Failed to get thread.";
    return errorJson(message, message === "Unauthorized" ? 401 : 500);
  }
}
