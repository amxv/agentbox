import { Clipboard, Toast, open, showToast } from "@raycast/api";
import { stat } from "node:fs/promises";
import {
  BodyContentType,
  Thread,
  UploadedAssetReference,
  createUploadIntents,
  dashboardThreadUrl,
  uploadFileToPresignedUrl,
  uploadIntentFileFromPath,
} from "./api";

export const BODY_FORMATS: Array<{ value: BodyContentType; title: string }> = [
  { value: "auto", title: "Auto" },
  { value: "text/markdown", title: "Markdown" },
  { value: "text/plain", title: "Plain Text" },
];

export type FormValuesBase = {
  bodyFormat: BodyContentType;
  files?: string[];
};

export async function uploadFilesForThread(
  threadId: string,
  filePaths: string[] | undefined,
): Promise<UploadedAssetReference[]> {
  const paths = (filePaths ?? []).filter(Boolean);
  if (paths.length === 0) {
    return [];
  }

  const files = await Promise.all(
    paths.map(async (filePath) => {
      const info = await stat(filePath);
      if (!info.isFile()) {
        throw new Error(`${filePath} is not a file.`);
      }
      return uploadIntentFileFromPath(filePath, info.size);
    }),
  );

  const uploads = await createUploadIntents(threadId, files);
  if (uploads.length !== paths.length) {
    throw new Error("Upload preparation returned the wrong number of files.");
  }

  await Promise.all(uploads.map((upload, index) => uploadFileToPresignedUrl(upload, paths[index])));
  return uploads.map((upload) => ({ upload_id: upload.upload_id }));
}

export async function showThreadSuccessToast({
  title,
  message,
  thread,
}: {
  title: string;
  message?: string;
  thread: Thread;
}) {
  await showToast({
    style: Toast.Style.Success,
    title,
    message: message ?? thread.title,
    primaryAction: {
      title: "Open Thread",
      onAction: () => {
        void open(dashboardThreadUrl(thread.id));
      },
    },
    secondaryAction: {
      title: "Copy Thread ID",
      onAction: () => {
        void Clipboard.copy(thread.id);
      },
    },
  });
}

export function normalizeFormError(error: unknown): Error {
  if (error instanceof Error) {
    return error;
  }
  return new Error(String(error));
}
