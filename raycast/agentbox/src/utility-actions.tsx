import { Action, ActionPanel, Icon, openExtensionPreferences } from "@raycast/api";
import { getPreferences, mcpUrl } from "./api";

export function AgentboxUtilityActions({ includeDashboard = true }: { includeDashboard?: boolean }) {
  const baseUrl = safeBaseUrl();
  const endpoint = safeMcpUrl();

  return (
    <ActionPanel.Section title="Agentbox">
      {includeDashboard && baseUrl && <Action.OpenInBrowser title="Open Dashboard" icon={Icon.Globe} url={baseUrl} />}
      {baseUrl && <Action.CopyToClipboard title="Copy API Base URL" icon={Icon.Link} content={baseUrl} />}
      {endpoint && <Action.CopyToClipboard title="Copy MCP URL" icon={Icon.Clipboard} content={endpoint} concealed />}
      <Action title="Open Extension Preferences" icon={Icon.Gear} onAction={() => void openExtensionPreferences()} />
    </ActionPanel.Section>
  );
}

export function safeBaseUrl(): string {
  try {
    return getPreferences().baseUrl;
  } catch {
    return "";
  }
}

export function safeMcpUrl(): string {
  try {
    return mcpUrl();
  } catch {
    return "";
  }
}
