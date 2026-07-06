import { Clipboard, Toast, openExtensionPreferences, showHUD, showToast } from "@raycast/api";
import { mcpUrl } from "./api";

export default async function CopyMcpUrl() {
  try {
    await Clipboard.copy(mcpUrl(), { concealed: true });
    await showHUD("Copied Agentbox MCP URL");
  } catch (error) {
    const normalized = normalizeError(error);
    await showToast({
      style: Toast.Style.Failure,
      title: "Could not copy MCP URL",
      message: normalized.message,
      primaryAction: {
        title: "Open Preferences",
        onAction: () => {
          void openExtensionPreferences();
        },
      },
    });
  }
}

function normalizeError(error: unknown): Error {
  if (error instanceof Error) {
    return error;
  }
  return new Error(String(error));
}
