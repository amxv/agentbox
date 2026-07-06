import { getPreferenceValues } from "@raycast/api";
import { readFile } from "node:fs/promises";
import path from "node:path";

export type BodyContentType = "auto" | "text/markdown" | "text/plain";

export type AgentboxPreferences = {
  baseUrl: string;
  apiKey: string;
  downloadDirectory?: string;
};

export type BackendErrorPayload = {
  error?: string;
  code?: string;
};

export class AgentboxAPIError extends Error {
  readonly status: number;
  readonly code?: string;
  readonly backendError?: string;
  readonly payload?: unknown;

  constructor(status: number, payload?: BackendErrorPayload, fallback = `Request failed with HTTP ${status}`) {
    const message = payload?.error ? (payload.code ? `${payload.code}: ${payload.error}` : payload.error) : fallback;
    super(message);
    this.name = "AgentboxAPIError";
    this.status = status;
    this.code = payload?.code;
    this.backendError = payload?.error;
    this.payload = payload;
  }
}

export type Thread = {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  created_by: string;
};

export type Asset = {
  id: string;
  message_id: string;
  storage_key: string;
  file_name: string;
  filename: string;
  mime_type: string | null;
  size_bytes: number;
  public_url: string | null;
  download_url?: string | null;
  created_at: string;
  created_by: string;
};

export type Message = {
  id: string;
  thread_id: string;
  author: string;
  body: string;
  body_content_type: string | null;
  created_at: string;
  assets: Asset[];
};

export type ThreadWithMessages = Thread & {
  messages: Message[];
};

export type SearchThreadResult = {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  message_count: number;
  last_message_preview: string;
  matched_snippets: string[];
};

export type UploadIntentFile = {
  file_name: string;
  mime_type?: string | null;
  size_bytes: number;
};

export type PresignedUpload = {
  upload_id: string;
  storage_key: string;
  file_name: string;
  mime_type: string | null;
  size_bytes: number;
  public_url: string | null;
  upload_url: string;
  expires_in: number;
  required_headers: Record<string, string>;
};

export type UploadedAssetReference = {
  upload_id: string;
};

export type AssetDownloadURL = {
  asset_id: string;
  file_name: string;
  mime_type: string | null;
  size_bytes: number;
  expires_in: number;
  download_url: string;
};

export type HealthResponse = {
  ok: boolean;
  service: string;
};

export type SearchThreadsParams = {
  query: string;
  limit?: number;
  createdBy?: string;
  updatedAfter?: string;
};

export type CreateThreadInput = {
  title: string;
  initialMessage?: string;
  bodyContentType?: BodyContentType | string;
};

export type CreateThreadResponse = {
  thread: Thread;
  message?: Message;
};

export type PostMessageInput = {
  threadId: string;
  body: string;
  bodyContentType?: BodyContentType | string;
  uploadedAssets?: UploadedAssetReference[];
};

type AgentboxFetchInit = RequestInit & {
  authenticated?: boolean;
};

type ThreadsResponse<T> = {
  threads: T[];
};

type ThreadResponse = {
  thread: ThreadWithMessages;
};

type MessageResponse = {
  message: Message;
};

type UploadsResponse = {
  uploads: PresignedUpload[];
};

export function getPreferences(): AgentboxPreferences {
  const preferences = getPreferenceValues<Preferences>();
  return {
    baseUrl: trimTrailingSlashes(preferences.baseUrl),
    apiKey: preferences.apiKey.trim(),
    downloadDirectory: preferences.downloadDirectory?.trim(),
  };
}

export async function agentboxFetch<T>(requestPath: string, init: AgentboxFetchInit = {}): Promise<T> {
  const { authenticated = true, headers, ...requestInit } = init;
  const url = agentboxUrl(requestPath, { authenticated });
  const response = await fetch(url, {
    ...requestInit,
    headers,
  });
  return parseResponse<T>(response);
}

export function mcpUrl(): string {
  return agentboxUrl("/api/mcp", { authenticated: true });
}

export function dashboardThreadUrl(threadId: string): string {
  const { baseUrl } = getPreferences();
  return new URL(`/threads/${encodeURIComponent(threadId)}`, ensureTrailingSlash(baseUrl)).toString();
}

export async function health(): Promise<HealthResponse> {
  return agentboxFetch<HealthResponse>("/api/health", { authenticated: false });
}

export async function listThreads(limit = 50): Promise<Thread[]> {
  const query = new URLSearchParams({ limit: String(limit) });
  const data = await agentboxFetch<ThreadsResponse<Thread>>(`/api/threads?${query.toString()}`);
  return data.threads;
}

export async function searchThreads(params: SearchThreadsParams): Promise<SearchThreadResult[]> {
  const query = new URLSearchParams();
  query.set("query", params.query);
  query.set("limit", String(params.limit ?? 20));
  if (params.createdBy?.trim()) {
    query.set("created_by", params.createdBy.trim());
  }
  if (params.updatedAfter?.trim()) {
    query.set("updated_after", params.updatedAfter.trim());
  }
  const data = await agentboxFetch<ThreadsResponse<SearchThreadResult>>(`/api/threads?${query.toString()}`);
  return data.threads;
}

export async function getThread(threadId: string): Promise<ThreadWithMessages> {
  const data = await agentboxFetch<ThreadResponse>(`/api/threads/${encodeURIComponent(threadId)}`);
  return data.thread;
}

export async function createThread(input: CreateThreadInput): Promise<CreateThreadResponse> {
  const body: Record<string, string> = {
    title: input.title,
  };
  if (input.initialMessage !== undefined) {
    body.initial_message = input.initialMessage;
  }
  if (input.bodyContentType !== undefined) {
    body.body_content_type = input.bodyContentType;
  }
  return agentboxFetch<CreateThreadResponse>("/api/threads", {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify(body),
  });
}

export async function postMessage(input: PostMessageInput): Promise<Message> {
  const body: {
    body: string;
    body_content_type?: string;
    uploaded_assets?: UploadedAssetReference[];
  } = {
    body: input.body,
  };
  if (input.bodyContentType !== undefined) {
    body.body_content_type = input.bodyContentType;
  }
  if (input.uploadedAssets !== undefined) {
    body.uploaded_assets = input.uploadedAssets;
  }
  const data = await agentboxFetch<MessageResponse>(`/api/threads/${encodeURIComponent(input.threadId)}/messages`, {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify(body),
  });
  return data.message;
}

export async function createUploadIntents(threadId: string, files: UploadIntentFile[]): Promise<PresignedUpload[]> {
  const data = await agentboxFetch<UploadsResponse>(`/api/threads/${encodeURIComponent(threadId)}/uploads`, {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify({ files }),
  });
  return data.uploads;
}

export async function uploadFileToPresignedUrl(upload: PresignedUpload, filePath: string): Promise<void> {
  const bytes = await readFile(filePath);
  const response = await fetch(upload.upload_url, {
    method: "PUT",
    headers: upload.required_headers,
    body: new Uint8Array(bytes),
  });
  if (!response.ok) {
    throw await responseError(response);
  }
}

export async function getAssetDownloadUrl(assetId: string, expiresIn?: number): Promise<AssetDownloadURL> {
  const query = new URLSearchParams();
  if (expiresIn !== undefined) {
    query.set("expires_in", String(expiresIn));
  }
  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  return agentboxFetch<AssetDownloadURL>(`/api/assets/${encodeURIComponent(assetId)}/download-url${suffix}`);
}

export function uploadIntentFileFromPath(
  filePath: string,
  sizeBytes: number,
  mimeType?: string | null,
): UploadIntentFile {
  return {
    file_name: path.basename(filePath),
    mime_type: mimeType ?? mimeTypeForPath(filePath),
    size_bytes: sizeBytes,
  };
}

function agentboxUrl(requestPath: string, options: { authenticated: boolean }): string {
  const { baseUrl, apiKey } = getPreferences();
  const url = new URL(trimLeadingSlashes(requestPath), ensureTrailingSlash(baseUrl));
  if (options.authenticated) {
    url.searchParams.set("key", apiKey);
  }
  return url.toString();
}

function trimTrailingSlashes(value: string): string {
  return value.trim().replace(/\/+$/, "");
}

function ensureTrailingSlash(value: string): string {
  return `${trimTrailingSlashes(value)}/`;
}

function trimLeadingSlashes(value: string): string {
  return value.replace(/^\/+/, "");
}

function jsonHeaders(): Record<string, string> {
  return { "content-type": "application/json" };
}

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    throw await responseError(response);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

async function responseError(response: Response): Promise<AgentboxAPIError> {
  const payload = await parseErrorPayload(response);
  return new AgentboxAPIError(response.status, payload, `Request failed with HTTP ${response.status}`);
}

async function parseErrorPayload(response: Response): Promise<BackendErrorPayload | undefined> {
  const text = await response.text();
  if (text.trim() === "") {
    return undefined;
  }
  try {
    const parsed = JSON.parse(text) as BackendErrorPayload;
    return parsed;
  } catch {
    return { error: text };
  }
}

function mimeTypeForPath(filePath: string): string {
  switch (path.extname(filePath).toLowerCase()) {
    case ".md":
    case ".markdown":
      return "text/markdown";
    case ".txt":
      return "text/plain";
    case ".json":
      return "application/json";
    case ".png":
      return "image/png";
    case ".jpg":
    case ".jpeg":
      return "image/jpeg";
    case ".gif":
      return "image/gif";
    case ".pdf":
      return "application/pdf";
    default:
      return "application/octet-stream";
  }
}
