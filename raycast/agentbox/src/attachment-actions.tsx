import { Action, ActionPanel, Clipboard, Icon, Keyboard, Toast, open, showToast } from "@raycast/api";
import { execFile } from "node:child_process";
import { mkdir, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import { Asset, getAssetDownloadUrl, getPreferences } from "./api";

export type AssetWithMessage = Asset & {
  messageId?: string;
};

const SIGNED_URL_EXPIRY_SECONDS = 60 * 60;
const execFileAsync = promisify(execFile);

export function AttachmentActions({ assets, title = "Attachments" }: { assets: AssetWithMessage[]; title?: string }) {
  if (assets.length === 0) {
    return null;
  }

  return (
    <ActionPanel.Section title={title}>
      {assets.map((asset) => (
        <ActionPanel.Submenu
          key={asset.id}
          title={`Attachment: ${assetName(asset)}`}
          icon={isImageAsset(asset) ? Icon.Image : Icon.Paperclip}
        >
          <Action
            title={isImageAsset(asset) ? "Download and Open Image" : "Download and Open Attachment"}
            icon={isImageAsset(asset) ? Icon.Image : Icon.Document}
            onAction={() => void downloadAndOpenAsset(asset)}
          />
          <Action
            title="Download and Show in Finder"
            icon={Icon.Finder}
            onAction={() => void downloadAndRevealAsset(asset)}
          />
          <Action
            title="Download and Copy File Path"
            icon={Icon.Clipboard}
            shortcut={Keyboard.Shortcut.Common.Copy}
            onAction={() => void downloadAndCopyAssetPath(asset)}
          />
          <Action
            title="Download and Copy File"
            icon={Icon.Document}
            onAction={() => void downloadAndCopyAssetFile(asset)}
          />
          <Action
            title="Open Signed Download URL"
            icon={Icon.Globe}
            onAction={() => void openSignedDownloadUrl(asset)}
          />
          <Action
            title="Copy Signed Download URL"
            icon={Icon.Link}
            onAction={() => void copySignedDownloadUrl(asset)}
          />
          {asset.public_url && (
            <Action.CopyToClipboard title="Copy Public URL" icon={Icon.Link} content={asset.public_url} />
          )}
          {asset.download_url && (
            <Action.CopyToClipboard title="Copy Asset Download URL" icon={Icon.Link} content={asset.download_url} />
          )}
          <Action.CopyToClipboard title="Copy Asset ID" content={asset.id} />
          {asset.messageId && <Action.CopyToClipboard title="Copy Message ID" content={asset.messageId} />}
        </ActionPanel.Submenu>
      ))}
    </ActionPanel.Section>
  );
}

async function downloadAndOpenAsset(asset: Asset) {
  const filePath = await downloadAsset(asset, "Downloaded attachment");
  if (filePath) {
    await open(filePath);
  }
}

async function downloadAndRevealAsset(asset: Asset) {
  const filePath = await downloadAsset(asset, "Downloaded attachment");
  if (filePath) {
    await execFileAsync("/usr/bin/open", ["-R", filePath]);
  }
}

async function downloadAndCopyAssetPath(asset: Asset) {
  const filePath = await downloadAsset(asset, "Downloaded attachment");
  if (filePath) {
    await Clipboard.copy(filePath);
    await showToast({ style: Toast.Style.Success, title: "Copied file path", message: filePath });
  }
}

async function downloadAndCopyAssetFile(asset: Asset) {
  const filePath = await downloadAsset(asset, "Downloaded attachment");
  if (filePath) {
    await Clipboard.copy({ file: filePath });
    await showToast({ style: Toast.Style.Success, title: "Copied file", message: assetName(asset) });
  }
}

async function openSignedDownloadUrl(asset: Asset) {
  const toast = await showToast({
    style: Toast.Style.Animated,
    title: "Creating signed URL",
    message: assetName(asset),
  });
  try {
    const signed = await getAssetDownloadUrl(asset.id, SIGNED_URL_EXPIRY_SECONDS);
    await open(signed.download_url);
    toast.style = Toast.Style.Success;
    toast.title = "Opened attachment";
    toast.message = signed.file_name;
  } catch (error) {
    toast.style = Toast.Style.Failure;
    toast.title = "Could not open attachment";
    toast.message = normalizeError(error).message;
  }
}

async function copySignedDownloadUrl(asset: Asset) {
  const toast = await showToast({
    style: Toast.Style.Animated,
    title: "Creating signed URL",
    message: assetName(asset),
  });
  try {
    const signed = await getAssetDownloadUrl(asset.id, SIGNED_URL_EXPIRY_SECONDS);
    await Clipboard.copy(signed.download_url, { concealed: true });
    toast.style = Toast.Style.Success;
    toast.title = "Copied signed URL";
    toast.message = signed.file_name;
  } catch (error) {
    toast.style = Toast.Style.Failure;
    toast.title = "Could not copy signed URL";
    toast.message = normalizeError(error).message;
  }
}

async function downloadAsset(asset: Asset, successTitle: string): Promise<string | null> {
  const toast = await showToast({
    style: Toast.Style.Animated,
    title: "Downloading attachment",
    message: assetName(asset),
  });

  try {
    const signed = await getAssetDownloadUrl(asset.id, SIGNED_URL_EXPIRY_SECONDS);
    const response = await fetch(signed.download_url);
    if (!response.ok) {
      throw new Error(`Download failed with HTTP ${response.status}`);
    }

    const directory = await downloadDirectory();
    await mkdir(directory, { recursive: true });
    const filePath = await uniqueFilePath(directory, signed.file_name || assetName(asset));
    await writeFile(filePath, new Uint8Array(await response.arrayBuffer()));

    toast.style = Toast.Style.Success;
    toast.title = successTitle;
    toast.message = filePath;
    return filePath;
  } catch (error) {
    toast.style = Toast.Style.Failure;
    toast.title = "Could not download attachment";
    toast.message = normalizeError(error).message;
    return null;
  }
}

async function downloadDirectory(): Promise<string> {
  try {
    const configured = getPreferences().downloadDirectory;
    if (configured) {
      return expandHome(configured);
    }
  } catch {
    // Fall back to Downloads when preferences are not available yet.
  }
  return path.join(os.homedir(), "Downloads", "Agentbox");
}

async function uniqueFilePath(directory: string, fileName: string): Promise<string> {
  const parsed = path.parse(sanitizeFileName(fileName));
  let candidate = path.join(directory, `${parsed.name}${parsed.ext}`);
  for (let index = 2; await pathExists(candidate); index += 1) {
    candidate = path.join(directory, `${parsed.name} ${index}${parsed.ext}`);
  }
  return candidate;
}

async function pathExists(filePath: string): Promise<boolean> {
  try {
    await stat(filePath);
    return true;
  } catch {
    return false;
  }
}

function assetName(asset: Asset): string {
  return asset.file_name || asset.filename || asset.id;
}

function sanitizeFileName(fileName: string): string {
  const sanitized = fileName.replace(/[/:\\]/g, "-").trim();
  return sanitized || "agentbox-attachment";
}

function expandHome(filePath: string): string {
  if (filePath === "~") {
    return os.homedir();
  }
  if (filePath.startsWith("~/")) {
    return path.join(os.homedir(), filePath.slice(2));
  }
  return filePath;
}

function isImageAsset(asset: Asset): boolean {
  const mimeType = asset.mime_type?.toLowerCase() ?? "";
  const fileName = assetName(asset).toLowerCase();
  return (
    mimeType.startsWith("image/") ||
    [".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".tiff", ".bmp"].some((extension) =>
      fileName.endsWith(extension),
    )
  );
}

function normalizeError(error: unknown): Error {
  if (error instanceof Error) {
    return error;
  }
  return new Error(String(error));
}
