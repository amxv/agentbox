import { Action, ActionPanel, Icon, Keyboard, List, Toast, openExtensionPreferences, showToast } from "@raycast/api";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AgentboxAPIError,
  Asset,
  Message,
  SearchThreadResult,
  Thread,
  ThreadWithMessages,
  dashboardThreadUrl,
  getAssetDownloadUrl,
  getThread,
  listThreads,
  mcpUrl,
  searchThreads,
} from "./api";
import { AttachmentActions } from "./attachment-actions";
import {
  escapeBlockquote,
  escapeMarkdown,
  formatBytes,
  formatDate,
  isImageAttachment,
  messageBodyMarkdown,
  messageMarkdown,
  threadMessagesMarkdown,
} from "./markdown";
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

type ThreadMessage = Message & {
  threadTitle: string;
  threadCreatedBy: string;
  threadUpdatedAt: string;
};

const RECENT_LIMIT = 50;
const SEARCH_LIMIT = 25;
const SEARCH_DEBOUNCE_MS = 300;
const IMAGE_PREVIEW_URL_EXPIRY_SECONDS = 60 * 60;

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

function ThreadMessageBrowser({ threadId, seedTitle }: { threadId: string; seedTitle?: string }) {
  const [thread, setThread] = useState<ThreadWithMessages | null>(null);
  const [loadState, setLoadState] = useState<LoadState>({ isLoading: true, error: null, hasLoaded: false });
  const [refreshKey, setRefreshKey] = useState(0);
  const [selectedMessageId, setSelectedMessageId] = useState<string | undefined>();

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
  const messages = useMemo(() => (thread ? chronologicalThreadMessages(thread) : []), [thread]);

  useEffect(() => {
    if (messages.length === 0) {
      setSelectedMessageId(undefined);
      return;
    }
    if (!selectedMessageId || !messages.some((message) => message.id === selectedMessageId)) {
      setSelectedMessageId(messages[0].id);
    }
  }, [messages, selectedMessageId]);

  return (
    <List
      filtering={false}
      isLoading={loadState.isLoading}
      isShowingDetail
      navigationTitle={title}
      onSelectionChange={(id) => setSelectedMessageId(id ?? undefined)}
      searchBarPlaceholder="Browse messages"
    >
      {thread && messages.length > 0 ? (
        <List.Section title={title} subtitle={`${messages.length} messages`}>
          {messages.map((message, index) => (
            <ThreadMessageListItem
              key={message.id}
              index={index}
              isSelected={message.id === selectedMessageId}
              message={message}
              onRefresh={() => setRefreshKey((value) => value + 1)}
              thread={thread}
            />
          ))}
        </List.Section>
      ) : (
        <ThreadMessageEmptyView
          error={loadState.error}
          hasLoaded={loadState.hasLoaded}
          onRefresh={() => setRefreshKey((value) => value + 1)}
          threadId={threadId}
          title={title}
        />
      )}
    </List>
  );
}

function ThreadMessageListItem({
  index,
  isSelected,
  message,
  onRefresh,
  thread,
}: {
  index: number;
  isSelected: boolean;
  message: ThreadMessage;
  onRefresh: () => void;
  thread: ThreadWithMessages;
}) {
  return (
    <List.Item
      id={message.id}
      title={messageTitle(message)}
      subtitle={`#${index + 1}`}
      accessories={messageAccessories(message)}
      detail={<MessagePreviewDetail isSelected={isSelected} message={message} />}
      actions={<MessageActions message={message} onRefresh={onRefresh} thread={thread} />}
    />
  );
}

function MessagePreviewDetail({ isSelected, message }: { isSelected: boolean; message: ThreadMessage }) {
  const [imagePreviewUrls, setImagePreviewUrls] = useState<Record<string, string>>({});
  const [imagePreviewError, setImagePreviewError] = useState<string | null>(null);
  const imageAssets = useMemo(() => message.assets.filter(isImageAttachment), [message.assets]);
  const imageAssetIds = useMemo(() => imageAssets.map((asset) => asset.id).join(","), [imageAssets]);

  useEffect(() => {
    let isMounted = true;
    setImagePreviewError(null);
    setImagePreviewUrls({});

    if (!isSelected || imageAssets.length === 0) {
      return () => {
        isMounted = false;
      };
    }

    const assetsNeedingSignedUrls = imageAssets.filter((asset) => !asset.download_url && !asset.public_url);
    if (assetsNeedingSignedUrls.length === 0) {
      return () => {
        isMounted = false;
      };
    }

    async function loadPreviewUrls(assets: Asset[]) {
      try {
        const signedUrls = await Promise.all(
          assets.map(async (asset) => {
            const signed = await getAssetDownloadUrl(asset.id, IMAGE_PREVIEW_URL_EXPIRY_SECONDS);
            return [asset.id, signed.download_url] as const;
          }),
        );
        if (isMounted) {
          setImagePreviewUrls(Object.fromEntries(signedUrls));
        }
      } catch (error) {
        if (isMounted) {
          setImagePreviewError(normalizeError(error).message);
        }
      }
    }

    void loadPreviewUrls(assetsNeedingSignedUrls);

    return () => {
      isMounted = false;
    };
  }, [imageAssetIds, imageAssets, isSelected, message.id]);

  return (
    <List.Item.Detail
      markdown={messageBodyMarkdown(message, { imagePreviewUrls, imagePreviewError })}
      metadata={<MessageMetadata message={message} />}
    />
  );
}

function ThreadActions({ thread, onRefresh }: { thread: ListedThread; onRefresh: () => void }) {
  const threadUrl = safeDashboardThreadUrl(thread.id);
  const mcpEndpoint = safeMcpUrl();

  return (
    <ActionPanel>
      <ActionPanel.Section>
        <Action.Push
          title="Browse Messages"
          icon={Icon.Sidebar}
          target={<ThreadMessageBrowser threadId={thread.id} seedTitle={thread.title} />}
        />
        <Action.CopyToClipboard
          title="Copy Thread Preview"
          icon={Icon.Clipboard}
          content={threadListMarkdown(thread)}
        />
        <Action.Push
          title="Post Message"
          icon={Icon.Message}
          target={<PostMessage initialThreadId={thread.id} />}
          shortcut={{ modifiers: ["cmd"], key: "return" }}
        />
        <Action.OpenInBrowser title="Open in Dashboard" icon={Icon.Globe} url={threadUrl} />
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
      <AgentboxUtilityActions />
    </ActionPanel>
  );
}

function MessageActions({
  message,
  onRefresh,
  thread,
}: {
  message: ThreadMessage;
  onRefresh: () => void;
  thread: ThreadWithMessages;
}) {
  const threadUrl = safeDashboardThreadUrl(message.thread_id);

  return (
    <ActionPanel>
      <ActionPanel.Section>
        <Action.CopyToClipboard title="Copy Message" icon={Icon.Clipboard} content={message.body} />
        <Action.CopyToClipboard
          title="Copy Message as Markdown"
          icon={Icon.Document}
          content={messageMarkdown(message)}
        />
        <Action.CopyToClipboard
          title="Copy Thread Transcript"
          icon={Icon.TextDocument}
          content={threadMessagesMarkdown(thread)}
        />
        <Action.OpenInBrowser title="Open in Dashboard" icon={Icon.Globe} url={threadUrl} />
        <Action.Push
          title="Post Reply"
          icon={Icon.Message}
          target={<PostMessage initialThreadId={message.thread_id} />}
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
        <Action.CopyToClipboard title="Copy Message ID" content={message.id} shortcut={Keyboard.Shortcut.Common.Copy} />
        <Action.CopyToClipboard title="Copy Thread ID" content={message.thread_id} />
        <Action.CopyToClipboard title="Copy Thread URL" content={threadUrl} />
      </ActionPanel.Section>
      <AttachmentActions
        assets={message.assets.map((asset) => ({ ...asset, messageId: message.id }))}
        title="Message Attachments"
      />
      <AgentboxUtilityActions />
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

function ThreadMessageEmptyView({
  error,
  hasLoaded,
  onRefresh,
  threadId,
  title,
}: {
  error: Error | null;
  hasLoaded: boolean;
  onRefresh: () => void;
  threadId: string;
  title: string;
}) {
  if (error) {
    const configError = isConfigurationError(error);
    return (
      <List.EmptyView
        icon={configError ? Icon.Gear : Icon.Warning}
        title={configError ? "Configure Agentbox" : "Could Not Load Thread"}
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
    return <List.EmptyView icon={Icon.Sidebar} title="Loading Thread Messages" description={threadId} />;
  }

  return (
    <List.EmptyView
      icon={Icon.Tray}
      title="No Messages Yet"
      description={title}
      actions={
        <ActionPanel>
          <Action.Push title="Post Message" icon={Icon.Message} target={<PostMessage initialThreadId={threadId} />} />
          <Action title="Refresh" icon={Icon.ArrowClockwise} onAction={onRefresh} />
          <Action.OpenInBrowser title="Open in Dashboard" icon={Icon.Globe} url={safeDashboardThreadUrl(threadId)} />
        </ActionPanel>
      }
    />
  );
}

function MessageMetadata({ message }: { message: ThreadMessage }) {
  return (
    <List.Item.Detail.Metadata>
      <List.Item.Detail.Metadata.Label title="Author" text={message.author || "Unknown"} />
      <List.Item.Detail.Metadata.Label title="Attachments" text={String(message.assets.length)} />
      <List.Item.Detail.Metadata.Label title="Created" text={formatDate(message.created_at)} />
      <List.Item.Detail.Metadata.Separator />
      <List.Item.Detail.Metadata.Label title="Thread" text={message.threadTitle || message.thread_id} />
      <List.Item.Detail.Metadata.Label title="Thread ID" text={message.thread_id} />
      <List.Item.Detail.Metadata.Label title="Message ID" text={message.id} />
      <List.Item.Detail.Metadata.Label title="Format" text={message.body_content_type || "auto"} />
      {message.assets.length > 0 && (
        <>
          <List.Item.Detail.Metadata.Separator />
          {message.assets.map((asset) => (
            <List.Item.Detail.Metadata.Label
              key={asset.id}
              title={asset.file_name || asset.filename || asset.id}
              text={`${asset.mime_type || "unknown type"} - ${formatBytes(asset.size_bytes)}`}
            />
          ))}
        </>
      )}
      <List.Item.Detail.Metadata.Separator />
      <List.Item.Detail.Metadata.Link
        title="Dashboard"
        text="Open thread"
        target={safeDashboardThreadUrl(message.thread_id)}
      />
    </List.Item.Detail.Metadata>
  );
}

function chronologicalThreadMessages(thread: ThreadWithMessages): ThreadMessage[] {
  return thread.messages
    .map((message, index) => ({ message, index }))
    .sort((left, right) => {
      const leftTime = new Date(left.message.created_at).getTime();
      const rightTime = new Date(right.message.created_at).getTime();
      if (leftTime !== rightTime) {
        return leftTime - rightTime;
      }
      return left.index - right.index;
    })
    .map(({ message }) => ({
      ...message,
      threadTitle: thread.title,
      threadCreatedBy: thread.created_by,
      threadUpdatedAt: thread.updated_at,
    }));
}

function messageTitle(message: ThreadMessage): string {
  const preview = message.body.replace(/\s+/g, " ").trim();
  if (preview.length > 120) {
    return `${preview.slice(0, 117)}...`;
  }
  return preview || (message.assets.length > 0 ? `${message.assets.length} attachment message` : "Empty message");
}

function messageAccessories(message: ThreadMessage): List.Item.Accessory[] {
  const accessories: List.Item.Accessory[] = [];
  if (message.assets.length > 0) {
    accessories.push({ text: `${message.assets.length} file`, icon: Icon.Paperclip });
  }
  if (message.author) {
    accessories.push({ text: message.author, icon: Icon.Person });
  }
  if (message.created_at) {
    accessories.push({ date: new Date(message.created_at), tooltip: `Sent ${formatDate(message.created_at)}` });
  }
  return accessories;
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
