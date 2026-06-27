import { createThread, getThread, listThreads, postMessage } from "./db";
import { uploadAssetBytes, uploadChatGPTFile } from "./assets";
import type { Actor, ChatGPTFileReference } from "./types";

export async function handleListThreads(limit?: number) {
  return { threads: await listThreads(limit) };
}

export async function handleCreateThread(actor: Actor, title: string) {
  return { thread: await createThread(title, actor.name) };
}

export async function handleGetThread(threadId: string) {
  const thread = await getThread(threadId);
  if (!thread) throw new Error("Thread not found.");
  return { thread };
}

export async function handlePostMessage(actor: Actor, params: {
  threadId: string;
  body: string;
  file?: ChatGPTFileReference | string | null;
}) {
  const asset = params.file ? await uploadChatGPTFile(params.threadId, params.file) : null;
  const message = await postMessage({
    threadId: params.threadId,
    author: actor.name,
    body: params.body,
    asset
  });
  return { message };
}

export async function handlePostMessageWithAsset(actor: Actor, params: {
  threadId: string;
  body: string;
  assetFile?: File | null;
}) {
  const asset = params.assetFile
    ? await uploadAssetBytes({
      threadId: params.threadId,
      bytes: new Uint8Array(await params.assetFile.arrayBuffer()),
      fileName: params.assetFile.name,
      mimeType: params.assetFile.type || null
    })
    : null;

  const message = await postMessage({
    threadId: params.threadId,
    author: actor.name,
    body: params.body,
    asset
  });

  return { message };
}
