import { requireAdminRequest } from "@/src/core/admin";
import { createSignedAssetDownloadUrl } from "@/src/core/assets";
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

    const messages = await Promise.all(thread.messages.map(async (message) => ({
      ...message,
      assets: await Promise.all(message.assets.map(async (asset) => {
        const isImage = asset.mime_type?.startsWith("image/") ?? false;
        const download_url = await createSignedAssetDownloadUrl({
          storageKey: asset.storage_key,
          fileName: asset.file_name,
          mimeType: asset.mime_type,
          expiresInSeconds: isImage ? 900 : 300
        });

        return {
          ...asset,
          download_url,
          preview_url: isImage ? download_url : null
        };
      }))
    })));

    return json({ thread: { ...thread, messages } });
  } catch (error) {
    if (error instanceof Response) return error;
    const message = error instanceof Error ? error.message : "Failed to get thread.";
    return errorJson(message, message === "Unauthorized" ? 401 : 500);
  }
}
