import { getPreferenceValues } from "@raycast/api";
import { readFile } from "node:fs/promises";
import path from "node:path";
import {
  AgentboxClient,
  AgentboxClientConfig,
  BodyContentType,
  CreateThreadInput,
  CreateThreadResponse,
  HealthResponse,
  ManageThreadVisibilityInput,
  ManagedThreadVisibility,
  PostMessageInput,
  PresignedUpload,
  SearchThreadResult,
  SearchThreadsParams,
  Team,
  Thread,
  ThreadPage,
  ThreadPageParams,
  ThreadWithMessages,
  UploadIntentFile,
  UploadedAssetReference,
  AssetDownloadURL,
  AuthContext,
  Message,
} from "./api-client";

export * from "./api-client";

export type AgentboxPreferences = AgentboxClientConfig & {
  downloadDirectory?: string;
};

export function getPreferences(): AgentboxPreferences {
  const preferences = getPreferenceValues<Preferences>();
  return {
    baseUrl: preferences.baseUrl,
    apiKey: preferences.apiKey,
    downloadDirectory: preferences.downloadDirectory?.trim(),
  };
}

function client(): AgentboxClient {
  return new AgentboxClient(getPreferences());
}

export async function agentboxFetch<T>(
  requestPath: string,
  init: RequestInit & { authenticated?: boolean } = {},
): Promise<T> {
  return client().request<T>(requestPath, init);
}

export function dashboardThreadUrl(threadId: string): string {
  return client().dashboardThreadUrl(threadId);
}

export function health(): Promise<HealthResponse> {
  return client().health();
}

export function authMe(): Promise<AuthContext> {
  return client().authMe();
}

export function listTeams(): Promise<Team[]> {
  return client().listTeams();
}

export function listThreadPage(params: ThreadPageParams = {}): Promise<ThreadPage<Thread>> {
  return client().listThreadPage(params);
}

export async function listThreads(limit = 50): Promise<Thread[]> {
  return (await listThreadPage({ limit })).threads;
}

export function searchThreadPage(params: SearchThreadsParams): Promise<ThreadPage<SearchThreadResult>> {
  return client().searchThreadPage(params);
}

export async function searchThreads(params: SearchThreadsParams): Promise<SearchThreadResult[]> {
  return (await searchThreadPage(params)).threads;
}

export function getThread(threadId: string): Promise<ThreadWithMessages> {
  return client().getThread(threadId);
}

export function getThreadVisibility(threadId: string): Promise<ManagedThreadVisibility> {
  return client().getThreadVisibility(threadId);
}

export function manageThreadVisibility(
  threadId: string,
  input: ManageThreadVisibilityInput,
): Promise<ManagedThreadVisibility> {
  return client().manageThreadVisibility(threadId, input);
}

export function createThread(input: CreateThreadInput): Promise<CreateThreadResponse> {
  return client().createThread(input);
}

export function postMessage(input: PostMessageInput): Promise<Message> {
  return client().postMessage(input);
}

export function createUploadIntents(threadId: string, files: UploadIntentFile[]): Promise<PresignedUpload[]> {
  return client().createUploadIntents(threadId, files);
}

export async function uploadFileToPresignedUrl(upload: PresignedUpload, filePath: string): Promise<void> {
  const bytes = await readFile(filePath);
  await client().uploadBytesToPresignedUrl(upload, new Uint8Array(bytes));
}

export function getAssetDownloadUrl(assetId: string, expiresIn?: number): Promise<AssetDownloadURL> {
  return client().getAssetDownloadUrl(assetId, expiresIn);
}

export async function uploadIntentFileFromPath(
  filePath: string,
  sizeBytes: number,
  mimeType?: string | null,
): Promise<UploadIntentFile> {
  const bytes = await readFile(filePath);
  const digest = await globalThis.crypto.subtle.digest("SHA-256", bytes);
  return {
    file_name: path.basename(filePath),
    mime_type: mimeType ?? mimeTypeForPath(filePath),
    size_bytes: sizeBytes,
    sha256: Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join(""),
  };
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

export type {
  AgentboxClientConfig,
  AssetDownloadURL,
  AuthContext,
  BodyContentType,
  CreateThreadInput,
  CreateThreadResponse,
  HealthResponse,
  ManageThreadVisibilityInput,
  ManagedThreadVisibility,
  Message,
  PostMessageInput,
  PresignedUpload,
  SearchThreadResult,
  SearchThreadsParams,
  Team,
  Thread,
  ThreadPage,
  ThreadPageParams,
  ThreadWithMessages,
  UploadIntentFile,
  UploadedAssetReference,
};
