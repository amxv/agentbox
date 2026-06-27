import { requireActor } from "@/src/core/auth";
import { createSignedAssetDownloadUrl } from "@/src/core/assets";
import { getAsset } from "@/src/core/db";
import { errorJson, json } from "@/src/core/http";

export const runtime = "nodejs";

type Params = { params: Promise<{ assetId: string }> };

export async function GET(request: Request, { params }: Params) {
  try {
    requireActor(request);
    const { assetId } = await params;
    const asset = await getAsset(assetId);
    if (!asset) return errorJson("Asset not found.", 404);

    const url = new URL(request.url);
    const expiresIn = Number(url.searchParams.get("expires_in") ?? "300");
    const safeExpiresIn = Number.isFinite(expiresIn) ? Math.min(Math.max(expiresIn, 60), 3600) : 300;

    const download_url = await createSignedAssetDownloadUrl({
      storageKey: asset.storage_key,
      fileName: asset.file_name,
      mimeType: asset.mime_type,
      expiresInSeconds: safeExpiresIn
    });

    return json({
      asset_id: asset.id,
      file_name: asset.file_name,
      mime_type: asset.mime_type,
      size_bytes: asset.size_bytes,
      expires_in: safeExpiresIn,
      download_url
    });
  } catch (error) {
    if (error instanceof Response) return error;
    return errorJson(error instanceof Error ? error.message : "Failed to create download URL.", 500);
  }
}
