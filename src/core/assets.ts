import { PutObjectCommand, S3Client } from "@aws-sdk/client-s3";
import { lookup } from "mime-types";
import type { ChatGPTFileReference } from "./types";
import type { NewAsset } from "./db";

const MAX_FILE_SIZE_BYTES = Number(process.env.AGENTBOX_MAX_FILE_SIZE_BYTES ?? 25 * 1024 * 1024);

let s3Client: S3Client | null = null;

function getR2Client(): S3Client {
  const accountId = process.env.R2_ACCOUNT_ID;
  const accessKeyId = process.env.R2_ACCESS_KEY_ID;
  const secretAccessKey = process.env.R2_SECRET_ACCESS_KEY;

  if (!accountId || !accessKeyId || !secretAccessKey) {
    throw new Error("R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY are required for asset uploads.");
  }

  s3Client ??= new S3Client({
    region: "auto",
    endpoint: `https://${accountId}.r2.cloudflarestorage.com`,
    credentials: { accessKeyId, secretAccessKey }
  });

  return s3Client;
}

export function sanitizeFilename(name: string): string {
  return name
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 150) || "file.bin";
}

export function inferMimeType(fileName: string, fallback?: string | null): string | null {
  return fallback ?? (lookup(fileName) || null);
}

function makeStorageKey(params: { threadId: string; messageHint: string; fileName: string }): string {
  return [
    "agentbox",
    params.threadId,
    params.messageHint,
    `${crypto.randomUUID()}-${sanitizeFilename(params.fileName)}`
  ].join("/");
}

function publicUrlForKey(key: string): string | null {
  const base = process.env.R2_PUBLIC_BASE_URL?.replace(/\/$/, "");
  return base ? `${base}/${key}` : null;
}

export async function uploadAssetBytes(params: {
  threadId: string;
  messageHint?: string;
  bytes: Uint8Array;
  fileName: string;
  mimeType?: string | null;
}): Promise<NewAsset> {
  if (params.bytes.byteLength > MAX_FILE_SIZE_BYTES) {
    throw new Error(`File is too large. Max size is ${MAX_FILE_SIZE_BYTES} bytes.`);
  }

  const bucket = process.env.R2_BUCKET;
  if (!bucket) throw new Error("R2_BUCKET is required for asset uploads.");

  const fileName = sanitizeFilename(params.fileName);
  const mimeType = inferMimeType(fileName, params.mimeType);
  const storageKey = makeStorageKey({
    threadId: params.threadId,
    messageHint: params.messageHint ?? "message",
    fileName
  });

  await getR2Client().send(new PutObjectCommand({
    Bucket: bucket,
    Key: storageKey,
    Body: params.bytes,
    ContentType: mimeType ?? "application/octet-stream"
  }));

  return {
    storageKey,
    fileName,
    mimeType,
    sizeBytes: params.bytes.byteLength,
    publicUrl: publicUrlForKey(storageKey)
  };
}

function chatGPTFileReferenceFromInput(file: ChatGPTFileReference | string): ChatGPTFileReference {
  if (typeof file !== "string") return file;

  const value = file.trim();
  if (/^https?:\/\//i.test(value)) {
    const url = new URL(value);
    const fileName = decodeURIComponent(url.pathname.split("/").filter(Boolean).at(-1) ?? "download.bin");
    return {
      download_url: value,
      file_id: `url-${crypto.randomUUID()}`,
      file_name: fileName || "download.bin"
    };
  }

  throw new Error(
    "File was received as a plain string. Pass a ChatGPT uploaded file ID like file_... to the MCP tool so ChatGPT expands it into { download_url, file_id, mime_type?, file_name? }. Local filesystem paths and plain filenames cannot be fetched by the remote Agentbox server."
  );
}

export async function uploadChatGPTFile(threadId: string, input: ChatGPTFileReference | string): Promise<NewAsset> {
  const file = chatGPTFileReferenceFromInput(input);
  const response = await fetch(file.download_url);
  if (!response.ok) {
    throw new Error(`Failed to download ChatGPT file: ${response.status} ${response.statusText}`);
  }

  const contentLength = Number(response.headers.get("content-length") ?? 0);
  if (contentLength > MAX_FILE_SIZE_BYTES) {
    throw new Error(`File is too large. Max size is ${MAX_FILE_SIZE_BYTES} bytes.`);
  }

  const bytes = new Uint8Array(await response.arrayBuffer());
  const fileName = file.file_name ?? `${file.file_id}.bin`;

  return uploadAssetBytes({
    threadId,
    messageHint: file.file_id,
    bytes,
    fileName,
    mimeType: file.mime_type
  });
}
