import { Asset, Message, ThreadWithMessages } from "./api";

export type MessageWithThread = Message & {
  threadTitle: string;
};

export type MessageBodyMarkdownOptions = {
  imagePreviewUrls?: Record<string, string>;
  imagePreviewError?: string | null;
};

export function messageMarkdown(message: MessageWithThread): string {
  const lines = [
    `# ${escapeMarkdown(message.threadTitle || message.thread_id)}`,
    "",
    `## ${escapeMarkdown(message.author || "Unknown")}`,
    "",
    `- Time: ${escapeMarkdown(formatDate(message.created_at))}`,
    `- Thread ID: \`${message.thread_id}\``,
    `- Message ID: \`${message.id}\``,
    `- Format: ${escapeMarkdown(message.body_content_type || "auto")}`,
    "",
    message.body.trim() ? message.body : "_Empty message_",
  ];

  if (message.assets.length > 0) {
    lines.push("", "## Attachments");
    for (const asset of message.assets) {
      lines.push(
        `- ${escapeMarkdown(asset.file_name || asset.filename || asset.id)} (${escapeMarkdown(asset.mime_type || "unknown type")}, ${formatBytes(asset.size_bytes)}) - \`${asset.id}\``,
      );
    }
  }

  return lines.join("\n");
}

export function messageBodyMarkdown(message: MessageWithThread, options: MessageBodyMarkdownOptions = {}): string {
  const lines = [
    `# ${escapeMarkdown(message.threadTitle || message.thread_id)}`,
    "",
    `## ${escapeMarkdown(message.author || "Unknown")}`,
    "",
    message.body.trim() ? message.body : "_Empty message_",
  ];

  if (message.assets.length > 0) {
    lines.push("", ...attachmentPreviewMarkdown(message.assets, options));
  }

  return lines.join("\n");
}

export function threadMessagesMarkdown(thread: ThreadWithMessages): string {
  const lines = [`# ${escapeMarkdown(thread.title || thread.id)}`, "", `\`${thread.id}\``];
  if (thread.messages.length === 0) {
    lines.push("", "No messages yet.");
    return lines.join("\n");
  }

  thread.messages.forEach((message, index) => {
    lines.push(
      "",
      "---",
      "",
      `## #${index + 1} ${escapeMarkdown(message.author || "Unknown")}`,
      "",
      `- Time: ${escapeMarkdown(formatDate(message.created_at))}`,
      `- Message ID: \`${message.id}\``,
      `- Format: ${escapeMarkdown(message.body_content_type || "auto")}`,
      "",
      message.body.trim() ? message.body : "_Empty message_",
    );
    if (message.assets.length > 0) {
      lines.push("", "### Attachments");
      for (const asset of message.assets) {
        lines.push(
          `- ${escapeMarkdown(asset.file_name || asset.filename || asset.id)} (${escapeMarkdown(asset.mime_type || "unknown type")}, ${formatBytes(asset.size_bytes)}) - \`${asset.id}\``,
        );
      }
    }
  });

  return lines.join("\n");
}

export function formatDate(value: string): string {
  if (!value) {
    return "Unknown";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** index;
  return `${value.toFixed(value >= 10 || index === 0 ? 0 : 1)} ${units[index]}`;
}

export function isImageAttachment(asset: Asset): boolean {
  const mimeType = asset.mime_type?.toLowerCase() ?? "";
  const fileName = assetName(asset).toLowerCase();
  return (
    mimeType.startsWith("image/") ||
    [".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".tiff", ".bmp"].some((extension) =>
      fileName.endsWith(extension),
    )
  );
}

function attachmentPreviewMarkdown(assets: Asset[], options: MessageBodyMarkdownOptions): string[] {
  const imageAssets = assets.filter(isImageAttachment);
  const otherAssets = assets.filter((asset) => !isImageAttachment(asset));
  const lines: string[] = [];

  if (imageAssets.length === 1) {
    const asset = imageAssets[0];
    lines.push("## Image Attachment", "", ...imageAttachmentMarkdown(asset, options));
  } else if (imageAssets.length > 1) {
    lines.push("## Image Attachments");
    for (const asset of imageAssets) {
      lines.push("", ...imageAttachmentMarkdown(asset, options));
    }
  }

  if (otherAssets.length > 0) {
    if (lines.length > 0) {
      lines.push("");
    }
    lines.push("## Attachments");
    for (const asset of otherAssets) {
      lines.push(attachmentMetadataLine(asset));
    }
  }

  return lines;
}

function imageAttachmentMarkdown(asset: Asset, options: MessageBodyMarkdownOptions): string[] {
  const imageUrl = assetPreviewUrl(asset, options.imagePreviewUrls);
  const lines: string[] = [];

  if (imageUrl) {
    lines.push(`![${escapeMarkdown(assetName(asset))}](<${imageUrl}>)`, "");
  } else if (options.imagePreviewError) {
    lines.push(`_Image preview unavailable: ${escapeMarkdown(options.imagePreviewError)}_`, "");
  } else {
    lines.push("_Loading image preview..._", "");
  }

  lines.push(attachmentMetadataLine(asset, imageUrl));
  return lines;
}

function attachmentMetadataLine(asset: Asset, linkUrl?: string): string {
  const name = escapeMarkdown(assetName(asset));
  const label = linkUrl ? `[${name}](<${linkUrl}>)` : name;
  return `- ${label} (${escapeMarkdown(asset.mime_type || "unknown type")}, ${formatBytes(asset.size_bytes)}) - \`${asset.id}\``;
}

function assetPreviewUrl(asset: Asset, imagePreviewUrls?: Record<string, string>): string | undefined {
  return imagePreviewUrls?.[asset.id] || asset.download_url || asset.public_url || undefined;
}

function assetName(asset: Asset): string {
  return asset.file_name || asset.filename || asset.id;
}

export function escapeMarkdown(value: string): string {
  return value.replace(/[\\`*_{}[\]()#+\-.!|>]/g, "\\$&");
}

export function escapeBlockquote(value: string): string {
  return escapeMarkdown(value).replace(/\n/g, "\n> ");
}
