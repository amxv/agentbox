type UploadIntent = {
  upload_id: string;
  sha256: string;
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

export async function createDashboardThread(title: string) {
  const response = await fetch("/api/threads", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ title })
  });
  const data = await response.json() as { thread?: { id: string }; error?: string };
  if (!response.ok || !data.thread?.id) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data.thread;
}

export async function uploadDashboardFiles(threadId: string, files: File[]) {
  if (files.length === 0) return [];
  const fileMetadata = await Promise.all(files.map(async (file) => ({
    file_name: file.name,
    mime_type: file.type || null,
    size_bytes: file.size,
    sha256: await sha256Hex(file)
  })));
  const intentResponse = await fetch(`/api/threads/${encodeURIComponent(threadId)}/uploads`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      files: fileMetadata
    })
  });
  const intentData = await intentResponse.json() as UploadIntentResponse & { error?: string };
  if (!intentResponse.ok) throw new Error(intentData.error ?? `HTTP ${intentResponse.status}`);
  const uploads = intentData.uploads ?? [];
  if (uploads.length !== files.length) throw new Error("Upload preparation returned the wrong number of files.");
  uploads.forEach((upload, index) => {
    if (upload.sha256 !== fileMetadata[index].sha256) throw new Error("Upload preparation changed the file checksum.");
  });

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

async function sha256Hex(file: File) {
  const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
  return Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join("");
}

export async function postDashboardMessage(threadId: string, body: string, files: File[]) {
  const uploadedAssets = await uploadDashboardFiles(threadId, files);
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/messages`, {
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
