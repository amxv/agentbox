import {
  Action,
  ActionPanel,
  Color,
  Icon,
  Keyboard,
  List,
  Toast,
  openExtensionPreferences,
  showToast,
} from "@raycast/api";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AgentboxAPIError, getPreferences, health, listThreads, mcpUrl } from "./api";
import { AgentboxUtilityActions, safeBaseUrl } from "./utility-actions";

type CheckStatus = "pending" | "pass" | "fail";

type DoctorCheck = {
  id: string;
  title: string;
  status: CheckStatus;
  detail: string;
  error?: string;
};

const INITIAL_CHECKS: DoctorCheck[] = [
  { id: "preferences", title: "Preferences", status: "pending", detail: "Checking Raycast configuration" },
  { id: "health", title: "Health Endpoint", status: "pending", detail: "Checking /api/health" },
  { id: "authenticated-api", title: "Authenticated API", status: "pending", detail: "Checking /api/threads?limit=1" },
  { id: "mcp-url", title: "MCP URL", status: "pending", detail: "Checking /api/mcp URL construction" },
];

export default function Doctor() {
  const [checks, setChecks] = useState<DoctorCheck[]>(INITIAL_CHECKS);
  const [isLoading, setIsLoading] = useState(true);
  const [refreshKey, setRefreshKey] = useState(0);

  const runChecks = useCallback(async () => {
    setIsLoading(true);
    setChecks(INITIAL_CHECKS);

    const preferencesCheck = checkPreferences();
    setChecks((current) => updateCheck(current, preferencesCheck));

    if (preferencesCheck.status === "fail") {
      const failed = INITIAL_CHECKS.filter((check) => check.id !== "preferences").map((check) => ({
        ...check,
        status: "fail" as const,
        detail: "Fix Agentbox preferences first.",
        error: preferencesCheck.error,
      }));
      setChecks([preferencesCheck, ...failed]);
      setIsLoading(false);
      await showToast({
        style: Toast.Style.Failure,
        title: "Agentbox preferences need attention",
        message: preferencesCheck.error ?? preferencesCheck.detail,
      });
      return;
    }

    const [healthCheck, authCheck, mcpCheck] = await Promise.all([
      checkHealth(),
      checkAuthenticatedAPI(),
      checkMcpUrl(),
    ]);
    const nextChecks = [preferencesCheck, healthCheck, authCheck, mcpCheck];
    setChecks(nextChecks);
    setIsLoading(false);

    const failure = nextChecks.find((check) => check.status === "fail");
    if (failure) {
      await showToast({
        style: Toast.Style.Failure,
        title: `${failure.title} failed`,
        message: failure.error ?? failure.detail,
      });
    }
  }, []);

  useEffect(() => {
    void runChecks();
  }, [refreshKey, runChecks]);

  const summary = useMemo(() => summarize(checks), [checks]);

  return (
    <List isLoading={isLoading} isShowingDetail>
      <List.Section title={summary.title} subtitle={summary.subtitle}>
        {checks.map((check) => (
          <List.Item
            key={check.id}
            title={check.title}
            subtitle={check.detail}
            icon={statusIcon(check.status)}
            accessories={[{ tag: statusTag(check.status) }]}
            detail={<List.Item.Detail markdown={checkMarkdown(check)} metadata={<CheckMetadata check={check} />} />}
            actions={<DoctorActions onRefresh={() => setRefreshKey((value) => value + 1)} />}
          />
        ))}
      </List.Section>
    </List>
  );
}

function DoctorActions({ onRefresh }: { onRefresh: () => void }) {
  const baseUrl = safeBaseUrl();

  return (
    <ActionPanel>
      <ActionPanel.Section>
        <Action
          title="Refresh Checks"
          icon={Icon.ArrowClockwise}
          onAction={onRefresh}
          shortcut={Keyboard.Shortcut.Common.Refresh}
        />
        {baseUrl && <Action.OpenInBrowser title="Open Dashboard" icon={Icon.Globe} url={baseUrl} />}
        <Action title="Open Extension Preferences" icon={Icon.Gear} onAction={() => void openExtensionPreferences()} />
      </ActionPanel.Section>
      <AgentboxUtilityActions />
    </ActionPanel>
  );
}

function CheckMetadata({ check }: { check: DoctorCheck }) {
  return (
    <List.Item.Detail.Metadata>
      <List.Item.Detail.Metadata.TagList title="Status">
        <List.Item.Detail.Metadata.TagList.Item text={statusLabel(check.status)} color={statusColor(check.status)} />
      </List.Item.Detail.Metadata.TagList>
      <List.Item.Detail.Metadata.Label title="Check" text={check.title} />
      <List.Item.Detail.Metadata.Label title="Detail" text={check.detail} />
      {check.error && <List.Item.Detail.Metadata.Label title="Error" text={check.error} />}
    </List.Item.Detail.Metadata>
  );
}

function checkPreferences(): DoctorCheck {
  try {
    const preferences = getPreferences();
    const missing = [];
    if (!preferences.baseUrl) {
      missing.push("base URL");
    }
    if (!preferences.apiKey) {
      missing.push("API key");
    }
    if (missing.length > 0) {
      return fail("preferences", "Preferences", `Missing ${missing.join(" and ")}.`, "Open extension preferences.");
    }
    const url = new URL(preferences.baseUrl);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return fail("preferences", "Preferences", "Base URL must start with http:// or https://.");
    }
    return pass("preferences", "Preferences", `${url.origin} with API key ${maskSecret(preferences.apiKey)}`);
  } catch (error) {
    return fail("preferences", "Preferences", "Raycast preferences are not usable.", normalizeError(error).message);
  }
}

async function checkHealth(): Promise<DoctorCheck> {
  try {
    const response = await health();
    if (!response.ok) {
      return fail("health", "Health Endpoint", "Health endpoint responded but did not report ok.", response.service);
    }
    return pass("health", "Health Endpoint", `Service ${response.service} is healthy.`);
  } catch (error) {
    return fail("health", "Health Endpoint", "Could not reach /api/health.", explainRequestError(error));
  }
}

async function checkAuthenticatedAPI(): Promise<DoctorCheck> {
  try {
    const threads = await listThreads(1);
    const detail =
      threads.length === 1
        ? "Authenticated API returned 1 visible thread."
        : "Authenticated API returned no visible threads.";
    return pass("authenticated-api", "Authenticated API", detail);
  } catch (error) {
    return fail(
      "authenticated-api",
      "Authenticated API",
      "Could not call /api/threads?limit=1.",
      explainRequestError(error),
    );
  }
}

async function checkMcpUrl(): Promise<DoctorCheck> {
  try {
    const endpoint = mcpUrl();
    const url = new URL(endpoint);
    if (url.pathname !== "/api/mcp") {
      return fail("mcp-url", "MCP URL", "MCP endpoint path is not /api/mcp.", sanitizeUrl(endpoint));
    }
    if (!url.searchParams.get("key")) {
      return fail("mcp-url", "MCP URL", "MCP URL is missing the API key query parameter.");
    }
    return pass("mcp-url", "MCP URL", sanitizeUrl(endpoint));
  } catch (error) {
    return fail("mcp-url", "MCP URL", "Could not construct the MCP URL.", normalizeError(error).message);
  }
}

function pass(id: string, title: string, detail: string): DoctorCheck {
  return { id, title, status: "pass", detail };
}

function fail(id: string, title: string, detail: string, error?: string): DoctorCheck {
  return { id, title, status: "fail", detail, error };
}

function updateCheck(checks: DoctorCheck[], next: DoctorCheck): DoctorCheck[] {
  return checks.map((check) => (check.id === next.id ? next : check));
}

function summarize(checks: DoctorCheck[]): { title: string; subtitle: string } {
  const failures = checks.filter((check) => check.status === "fail").length;
  const pending = checks.filter((check) => check.status === "pending").length;
  if (pending > 0) {
    return { title: "Running Diagnostics", subtitle: `${pending} pending` };
  }
  if (failures > 0) {
    return { title: "Diagnostics Failed", subtitle: `${failures} failed` };
  }
  return { title: "Diagnostics Passed", subtitle: `${checks.length} passed` };
}

function statusIcon(status: CheckStatus): List.Item.Props["icon"] {
  switch (status) {
    case "pass":
      return { source: Icon.CheckCircle, tintColor: Color.Green };
    case "fail":
      return { source: Icon.XMarkCircle, tintColor: Color.Red };
    case "pending":
      return { source: Icon.Clock, tintColor: Color.SecondaryText };
  }
}

function statusTag(status: CheckStatus): { value: string; color: Color } {
  return { value: statusLabel(status), color: statusColor(status) };
}

function statusLabel(status: CheckStatus): string {
  switch (status) {
    case "pass":
      return "Pass";
    case "fail":
      return "Fail";
    case "pending":
      return "Pending";
  }
}

function statusColor(status: CheckStatus): Color {
  switch (status) {
    case "pass":
      return Color.Green;
    case "fail":
      return Color.Red;
    case "pending":
      return Color.SecondaryText;
  }
}

function checkMarkdown(check: DoctorCheck): string {
  const lines = [`# ${check.title}`, "", `**Status:** ${statusLabel(check.status)}`, "", check.detail];
  if (check.error) {
    lines.push("", "## Detail", "", `\`${escapeBackticks(check.error)}\``);
  }
  return lines.join("\n");
}

function explainRequestError(error: unknown): string {
  if (error instanceof AgentboxAPIError) {
    if (error.status === 401 || error.status === 403) {
      return `${error.status}: API key was rejected. Check the Agentbox API Key preference.`;
    }
    return `${error.status}: ${error.message}`;
  }
  return normalizeError(error).message;
}

function sanitizeUrl(value: string): string {
  try {
    const url = new URL(value);
    const key = url.searchParams.get("key");
    if (key) {
      url.searchParams.set("key", maskSecret(key));
    }
    return url.toString();
  } catch {
    return value;
  }
}

function maskSecret(secret: string): string {
  if (secret.length <= 5) {
    return "****";
  }
  return `${secret.slice(0, 3)}${"*".repeat(Math.max(3, secret.length - 5))}${secret.slice(-2)}`;
}

function escapeBackticks(value: string): string {
  return value.replace(/`/g, "\\`");
}

function normalizeError(error: unknown): Error {
  if (error instanceof Error) {
    return error;
  }
  return new Error(String(error));
}
