import {
  Action,
  ActionPanel,
  Detail,
  Icon,
  Keyboard,
  List,
  Toast,
  openExtensionPreferences,
  showToast,
} from "@raycast/api";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  AgentboxAPIError,
  SearchThreadResult,
  Thread,
  ThreadWithMessages,
  dashboardThreadUrl,
  getThread,
  listThreads,
  mcpUrl,
  searchThreads,
} from "./api";
import { AttachmentActions } from "./attachment-actions";
import { escapeBlockquote, escapeMarkdown, formatDate, threadMessagesMarkdown } from "./markdown";
import PostMessage from "./post-message";
import { AgentboxUtilityActions } from "./utility-actions";

type ListedThread = {
  id: string;
  title: string;
  createdAt?: string;
  updatedAt: string;
  createdBy: string;
  messageCount?: number;
  lastMessagePreview?: string;
  matchedSnippets: string[];
};

type LoadState = {
  isLoading: boolean;
  error: Error | null;
  hasLoaded: boolean;
};

const RECENT_LIMIT = 50;
const SEARCH_LIMIT = 25;
const SEARCH_DEBOUNCE_MS = 300;

export default function SearchThreads() {
  const [searchText, setSearchText] = useState("");
  const [threads, setThreads] = useState<ListedThread[]>([]);
  const [loadState, setLoadState] = useState<LoadState>({ isLoading: true, error: null, hasLoaded: false });
  const [refreshKey, setRefreshKey] = useState(0);
  const requestId = useRef(0);

  const trimmedSearch = searchText.trim();

  const loadThreads = useCallback(async (query: string, runId: number) => {
    setLoadState((current) => ({ ...current, isLoading: true, error: null }));
    try {
      const data = query
        ? (await searchThreads({ query, limit: SEARCH_LIMIT })).map(threadFromSearchResult)
        : (await listThreads(RECENT_LIMIT)).map(threadFromRecent);
      if (requestId.current !== runId) {
        return;
      }
      setThreads(data);
      setLoadState({ isLoading: false, error: null, hasLoaded: true });
    } catch (error) {
      if (requestId.current !== runId) {
        return;
      }
      const normalized = normalizeError(error);
      setThreads([]);
      setLoadState({ isLoading: false, error: normalized, hasLoaded: true });
      await showToast({
        style: Toast.Style.Failure,
        title: "Could not load threads",
        message: normalized.message,
      });
    }
  }, []);

  useEffect(() => {
    const runId = requestId.current + 1;
    requestId.current = runId;
    const timeout = setTimeout(
      () => {
        void loadThreads(trimmedSearch, runId);
      },
      trimmedSearch ? SEARCH_DEBOUNCE_MS : 0,
    );
    return () => clearTimeout(timeout);
  }, [loadThreads, refreshKey, trimmedSearch]);

  const emptyView = (
    <ThreadEmptyView
      error={loadState.error}
      hasLoaded={loadState.hasLoaded}
      isSearching={Boolean(trimmedSearch)}
      onRefresh={() => setRefreshKey((value) => value + 1)}
    />
  );

  return (
    <List
      filtering={false}
      isLoading={loadState.isLoading}
      isShowingDetail
      onSearchTextChange={setSearchText}
      searchBarPlaceholder="Search Agentbox threads"
      searchText={searchText}
    >
      {threads.length === 0 ? (
        emptyView
      ) : (
        <List.Section title={trimmedSearch ? "Search Results" : "Recent Threads"} subtitle={`${threads.length}`}>
          {threads.map((thread) => (
            <ThreadListItem key={thread.id} thread={thread} onRefresh={() => setRefreshKey((value) => value + 1)} />
          ))}
        </List.Section>
      )}
    </List>
  );
}

function ThreadListItem({ thread, onRefresh }: { thread: ListedThread; onRefresh: () => void }) {
  return (
    <List.Item
      id={thread.id}
      title={thread.title || thread.id}
      subtitle={thread.lastMessagePreview}
      accessories={threadAccessories(thread)}
      detail={
        <List.Item.Detail markdown={threadListMarkdown(thread)} metadata={<ThreadListMetadata thread={thread} />} />
      }
      actions={<ThreadActions thread={thread} onRefresh={onRefresh} />}
    />
  );
}

function ThreadDetailView({ threadId, seedTitle }: { threadId: string; seedTitle?: string }) {
  const [thread, setThread] = useState<ThreadWithMessages | null>(null);
  const [loadState, setLoadState] = useState<LoadState>({ isLoading: true, error: null, hasLoaded: false });
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    async function loadThread() {
      setLoadState((current) => ({ ...current, isLoading: true, error: null }));
      try {
        const data = await getThread(threadId);
        if (cancelled) {
          return;
        }
        setThread(data);
        setLoadState({ isLoading: false, error: null, hasLoaded: true });
      } catch (error) {
        if (cancelled) {
          return;
        }
        const normalized = normalizeError(error);
        setThread(null);
        setLoadState({ isLoading: false, error: normalized, hasLoaded: true });
        await showToast({
          style: Toast.Style.Failure,
          title: "Could not load thread",
          message: normalized.message,
        });
      }
    }
    void loadThread();
    return () => {
      cancelled = true;
    };
  }, [refreshKey, threadId]);

  const title = thread?.title ?? seedTitle ?? threadId;

  return (
    <Detail
      isLoading={loadState.isLoading}
      markdown={thread ? threadMessagesMarkdown(thread) : detailPlaceholderMarkdown(loadState.error, threadId)}
      metadata={thread ? <ThreadDetailMetadata thread={thread} /> : undefined}
      actions={
        <ThreadActions
          thread={
            thread
              ? threadFromDetailed(thread)
              : { id: threadId, title, updatedAt: "", createdBy: "", matchedSnippets: [] }
          }
          onRefresh={() => setRefreshKey((value) => value + 1)}
          detailedThread={thread}
          error={loadState.error}
        />
      }
    />
  );
}

function ThreadActions({
  thread,
  onRefresh,
  detailedThread,
  error,
}: {
  thread: ListedThread;
  onRefresh: () => void;
  detailedThread?: ThreadWithMessages | null;
  error?: Error | null;
}) {
  const threadUrl = safeDashboardThreadUrl(thread.id);
  const mcpEndpoint = safeMcpUrl();
  const isConfigError = isConfigurationError(error);
  const copyContent = detailedThread ? threadMessagesMarkdown(detailedThread) : threadListMarkdown(thread);
  const copyTitle = detailedThread ? "Copy Thread Messages" : "Copy Thread Preview";
  const assets =
    detailedThread?.messages.flatMap((message) => message.assets.map((asset) => ({ ...asset, messageId: message.id }))) ??
    [];

  return (
    <ActionPanel>
      <ActionPanel.Section>
        <Action.CopyToClipboard title={copyTitle} icon={Icon.Clipboard} content={copyContent} />
        <Action.Push
          title="Inspect Thread"
          icon={Icon.Sidebar}
          target={<ThreadDetailView threadId={thread.id} seedTitle={thread.title} />}
        />
        <Action.OpenInBrowser title="Open in Dashboard" icon={Icon.Globe} url={threadUrl} />
        <Action.Push
          title="Post Message"
          icon={Icon.Message}
          target={<PostMessage initialThreadId={thread.id} />}
          shortcut={{ modifiers: ["cmd"], key: "return" }}
        />
        <Action
          title="Refresh"
          icon={Icon.ArrowClockwise}
          onAction={onRefresh}
          shortcut={Keyboard.Shortcut.Common.Refresh}
        />
      </ActionPanel.Section>
      <ActionPanel.Section title="Copy">
        <Action.CopyToClipboard title="Copy Thread ID" content={thread.id} shortcut={Keyboard.Shortcut.Common.Copy} />
        <Action.CopyToClipboard title="Copy Thread URL" content={threadUrl} />
        <Action.CopyToClipboard title="Copy MCP URL" content={mcpEndpoint} concealed />
      </ActionPanel.Section>
      <AttachmentActions assets={assets} />
      <AgentboxUtilityActions />
      {isConfigError && (
        <ActionPanel.Section>
          <Action
            title="Open Extension Preferences"
            icon={Icon.Gear}
            onAction={() => void openExtensionPreferences()}
          />
        </ActionPanel.Section>
      )}
    </ActionPanel>
  );
}

function ThreadEmptyView({
  error,
  hasLoaded,
  isSearching,
  onRefresh,
}: {
  error: Error | null;
  hasLoaded: boolean;
  isSearching: boolean;
  onRefresh: () => void;
}) {
  if (error) {
    const configError = isConfigurationError(error);
    return (
      <List.EmptyView
        icon={configError ? Icon.Gear : Icon.Warning}
        title={configError ? "Configure Agentbox" : "Could Not Load Threads"}
        description={error.message}
        actions={
          <ActionPanel>
            <Action title="Refresh" icon={Icon.ArrowClockwise} onAction={onRefresh} />
            {configError && (
              <Action
                title="Open Extension Preferences"
                icon={Icon.Gear}
                onAction={() => void openExtensionPreferences()}
              />
            )}
          </ActionPanel>
        }
      />
    );
  }

  if (!hasLoaded) {
    return <List.EmptyView icon={Icon.MagnifyingGlass} title="Loading Agentbox Threads" />;
  }

  return (
    <List.EmptyView
      icon={isSearching ? Icon.MagnifyingGlass : Icon.Tray}
      title={isSearching ? "No Search Results" : "No Threads Yet"}
      description={isSearching ? "No thread titles or messages matched this search." : "Your Agentbox inbox is empty."}
      actions={
        <ActionPanel>
          <Action title="Refresh" icon={Icon.ArrowClockwise} onAction={onRefresh} />
        </ActionPanel>
      }
    />
  );
}

function ThreadListMetadata({ thread }: { thread: ListedThread }) {
  return (
    <List.Item.Detail.Metadata>
      <List.Item.Detail.Metadata.Label title="Thread ID" text={thread.id} />
      <List.Item.Detail.Metadata.Label title="Creator" text={thread.createdBy || "Unknown"} />
      {thread.messageCount !== undefined && (
        <List.Item.Detail.Metadata.Label title="Messages" text={String(thread.messageCount)} />
      )}
      {thread.createdAt && <List.Item.Detail.Metadata.Label title="Created" text={formatDate(thread.createdAt)} />}
      {thread.updatedAt && <List.Item.Detail.Metadata.Label title="Updated" text={formatDate(thread.updatedAt)} />}
      <List.Item.Detail.Metadata.Separator />
      <List.Item.Detail.Metadata.Link title="Dashboard" text="Open thread" target={safeDashboardThreadUrl(thread.id)} />
    </List.Item.Detail.Metadata>
  );
}

function ThreadDetailMetadata({ thread }: { thread: ThreadWithMessages }) {
  const assetCount = thread.messages.reduce((total, message) => total + message.assets.length, 0);
  return (
    <Detail.Metadata>
      <Detail.Metadata.Label title="Thread ID" text={thread.id} />
      <Detail.Metadata.Label title="Creator" text={thread.created_by || "Unknown"} />
      <Detail.Metadata.Label title="Messages" text={String(thread.messages.length)} />
      <Detail.Metadata.Label title="Attachments" text={String(assetCount)} />
      <Detail.Metadata.Label title="Created" text={formatDate(thread.created_at)} />
      <Detail.Metadata.Label title="Updated" text={formatDate(thread.updated_at)} />
      <Detail.Metadata.Separator />
      <Detail.Metadata.Link title="Dashboard" text="Open thread" target={safeDashboardThreadUrl(thread.id)} />
    </Detail.Metadata>
  );
}

function threadFromRecent(thread: Thread): ListedThread {
  return {
    id: thread.id,
    title: thread.title,
    createdAt: thread.created_at,
    updatedAt: thread.updated_at,
    createdBy: thread.created_by,
    matchedSnippets: [],
  };
}

function threadFromSearchResult(thread: SearchThreadResult): ListedThread {
  return {
    id: thread.id,
    title: thread.title,
    createdAt: thread.created_at,
    updatedAt: thread.updated_at,
    createdBy: thread.created_by,
    messageCount: thread.message_count,
    lastMessagePreview: thread.last_message_preview,
    matchedSnippets: thread.matched_snippets ?? [],
  };
}

function threadFromDetailed(thread: ThreadWithMessages): ListedThread {
  return {
    id: thread.id,
    title: thread.title,
    createdAt: thread.created_at,
    updatedAt: thread.updated_at,
    createdBy: thread.created_by,
    messageCount: thread.messages.length,
    matchedSnippets: [],
  };
}

function threadAccessories(thread: ListedThread): List.Item.Accessory[] {
  const accessories: List.Item.Accessory[] = [];
  if (thread.messageCount !== undefined) {
    accessories.push({ text: `${thread.messageCount} msg`, icon: Icon.SpeechBubble });
  }
  if (thread.createdBy) {
    accessories.push({ text: thread.createdBy, icon: Icon.Person });
  }
  if (thread.updatedAt) {
    accessories.push({ date: new Date(thread.updatedAt), tooltip: `Updated ${formatDate(thread.updatedAt)}` });
  }
  return accessories;
}

function threadListMarkdown(thread: ListedThread): string {
  const lines = [`# ${escapeMarkdown(thread.title || thread.id)}`, "", `\`${thread.id}\``];
  if (thread.lastMessagePreview) {
    lines.push("", "## Latest Message", "", escapeMarkdown(thread.lastMessagePreview));
  }
  if (thread.matchedSnippets.length > 0) {
    lines.push("", "## Matches");
    for (const snippet of thread.matchedSnippets) {
      if (snippet.trim()) {
        lines.push("", `> ${escapeBlockquote(snippet)}`);
      }
    }
  }
  if (!thread.lastMessagePreview && thread.matchedSnippets.length === 0) {
    lines.push("", "Open the detail view to load messages and attachments for this thread.");
  }
  return lines.join("\n");
}

function detailPlaceholderMarkdown(error: Error | null, threadId: string): string {
  if (error) {
    return `# Could Not Load Thread\n\n\`${threadId}\`\n\n${escapeMarkdown(error.message)}`;
  }
  return `# Loading Thread\n\n\`${threadId}\``;
}

function safeDashboardThreadUrl(threadId: string): string {
  try {
    return dashboardThreadUrl(threadId);
  } catch {
    return `https://agentbox-black.vercel.app/threads/${encodeURIComponent(threadId)}`;
  }
}

function safeMcpUrl(): string {
  try {
    return mcpUrl();
  } catch {
    return "";
  }
}

function normalizeError(error: unknown): Error {
  if (error instanceof Error) {
    return error;
  }
  return new Error(String(error));
}

function isConfigurationError(error: Error | null | undefined): boolean {
  if (!error) {
    return false;
  }
  if (error instanceof AgentboxAPIError) {
    return error.status === 401 || error.status === 403;
  }
  const message = error.message.toLowerCase();
  return (
    message.includes("preference") ||
    message.includes("api key") ||
    message.includes("base url") ||
    message.includes("unauthorized")
  );
}
