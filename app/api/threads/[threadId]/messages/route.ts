import { requireActor } from "@/src/core/auth";
import { errorJson, json, parseJson } from "@/src/core/http";
import { handlePostMessage, handlePostMessageWithAsset } from "@/src/core/handlers";
import { fileReferenceSchema } from "@/src/core/schemas";

export const runtime = "nodejs";

type Params = { params: Promise<{ threadId: string }> };

type JsonInput = {
  body?: string;
  file?: unknown;
};

export async function POST(request: Request, { params }: Params) {
  try {
    const actor = requireActor(request);
    const { threadId } = await params;
    const contentType = request.headers.get("content-type") ?? "";

    if (contentType.includes("multipart/form-data")) {
      const form = await request.formData();
      const body = String(form.get("body") ?? "");
      const asset = form.get("asset");
      return json(await handlePostMessageWithAsset(actor, {
        threadId,
        body,
        assetFile: asset instanceof File ? asset : null
      }), { status: 201 });
    }

    const input = await parseJson<JsonInput>(request);
    return json(await handlePostMessage(actor, {
      threadId,
      body: input.body ?? "",
      file: input.file ? fileReferenceSchema.parse(input.file) : null
    }), { status: 201 });
  } catch (error) {
    if (error instanceof Response) return error;
    return errorJson(error instanceof Error ? error.message : "Failed to post message.", 400);
  }
}
