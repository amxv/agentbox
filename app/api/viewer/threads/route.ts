import { requireAdminRequest } from "@/src/core/admin";
import { listThreads } from "@/src/core/db";
import { errorJson, json } from "@/src/core/http";

export const runtime = "nodejs";

export async function GET(request: Request) {
  try {
    requireAdminRequest(request);
    const url = new URL(request.url);
    const limit = Number(url.searchParams.get("limit") ?? "100");
    const safeLimit = Number.isFinite(limit) ? Math.min(Math.max(limit, 1), 200) : 100;
    return json({ threads: await listThreads(safeLimit) });
  } catch (error) {
    if (error instanceof Response) return error;
    const message = error instanceof Error ? error.message : "Failed to list threads.";
    return errorJson(message, message === "Unauthorized" ? 401 : 500);
  }
}
