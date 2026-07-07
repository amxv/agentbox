import { Message, ThreadWithMessages } from "./api";

export type MessageWithThread = Message & {
  threadTitle: string;
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

export function messageBodyMarkdown(message: MessageWithThread): string {
  const lines = [
    `# ${escapeMarkdown(message.threadTitle || message.thread_id)}`,
    "",
    `## ${escapeMarkdown(message.author || "Unknown")}`,
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

export function escapeMarkdown(value: string): string {
  return value.replace(/[\\`*_{}[\]()#+\-.!|>]/g, "\\$&");
}

export function escapeBlockquote(value: string): string {
  return escapeMarkdown(value).replace(/\n/g, "\n> ");
}
