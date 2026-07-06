import { Clipboard, getPreferenceValues, showHUD } from "@raycast/api";

type Preferences = {
  baseUrl: string;
  apiKey: string;
};

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, "");
}

export default async function CopyMcpUrl() {
  const preferences = getPreferenceValues<Preferences>();
  const url = new URL("/api/mcp", trimTrailingSlash(preferences.baseUrl));
  url.searchParams.set("key", preferences.apiKey);

  await Clipboard.copy(url.toString(), { concealed: true });
  await showHUD("Copied Agentbox MCP URL");
}
