const ACTOR_KEY_STORAGE = "agentbox_actor_key";
const ACTOR_NAME = "user";

type CreatedAPIKey = {
  key: {
    key: string;
  };
};

type UploadIntent = {
  upload_id: string;
  upload_url: string;
  required_headers?: Record<string, string>;
};

type UploadIntentResponse = {
  uploads?: UploadIntent[];
};

type MessageResponse = {
  message?: unknown;
};

async function parseError(response: Response) {
  try {
    const data = await response.json();
    return data.error ?? `HTTP ${response.status}`;
  } catch {
    return `HTTP ${response.status}`;
  }
}

async function actorKeyWorks(actorKey: string) {
  const response = await fetch(`/api/threads?key=${encodeURIComponent(actorKey)}&limit=1`);
  return response.ok;
}

export async function ensureDashboardActorKey(adminKey: string) {
  const existing = window.localStorage.getItem(ACTOR_KEY_STORAGE);
  if (existing && await actorKeyWorks(existing)) return existing;

  const response = await fetch("/api/admin/keys", {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-agentbox-admin-key": adminKey
    },
    body: JSON.stringify({ name: ACTOR_NAME })
  });
  const data = await response.json() as CreatedAPIKey;
  if (!response.ok || !data.key?.key) throw new Error((data as { error?: string }).error ?? `HTTP ${response.status}`);
  window.localStorage.setItem(ACTOR_KEY_STORAGE, data.key.key);
  return data.key.key;
}

export async function createDashboardThread(actorKey: string, title: string) {
  const response = await fetch(`/api/threads?key=${encodeURIComponent(actorKey)}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ title })
  });
  const data = await response.json() as { thread?: { id: string }; error?: string };
  if (!response.ok || !data.thread?.id) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data.thread;
}

export async function uploadDashboardFiles(actorKey: string, threadId: string, files: File[]) {
  if (files.length === 0) return [];
  const intentResponse = await fetch(`/api/threads/${encodeURIComponent(threadId)}/uploads?key=${encodeURIComponent(actorKey)}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      files: files.map((file) => ({
        file_name: file.name,
        mime_type: file.type || null,
        size_bytes: file.size
      }))
    })
  });
  const intentData = await intentResponse.json() as UploadIntentResponse & { error?: string };
  if (!intentResponse.ok) throw new Error(intentData.error ?? `HTTP ${intentResponse.status}`);
  const uploads = intentData.uploads ?? [];
  if (uploads.length !== files.length) throw new Error("Upload preparation returned the wrong number of files.");

  for (const [index, upload] of uploads.entries()) {
    const uploadResponse = await fetch(upload.upload_url, {
      method: "PUT",
      headers: upload.required_headers ?? {},
      body: files[index]
    });
    if (!uploadResponse.ok) throw new Error(await parseError(uploadResponse));
  }

  return uploads.map((upload) => ({ upload_id: upload.upload_id }));
}

export async function postDashboardMessage(actorKey: string, threadId: string, body: string, files: File[]) {
  const uploadedAssets = await uploadDashboardFiles(actorKey, threadId, files);
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/messages?key=${encodeURIComponent(actorKey)}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      body,
      uploaded_assets: uploadedAssets
    })
  });
  const data = await response.json() as MessageResponse & { error?: string };
  if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data.message;
}
