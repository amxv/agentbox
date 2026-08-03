import {
  Action,
  ActionPanel,
  Icon,
  Keyboard,
  List,
  LocalStorage,
  Toast,
  openExtensionPreferences,
  showToast,
} from "@raycast/api";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AgentboxAPIError,
  Asset,
  Message,
  SearchThreadResult,
  Team,
  Thread,
  ThreadFilter,
  ThreadPageInfo,
  ThreadVisibilitySummary,
  ThreadWithMessages,
  attributionLabel,
  dashboardThreadUrl,
  getAssetDownloadUrl,
  getThread,
  listTeams,
  listThreadPage,
  searchThreadPage,
  visibilityLabels,
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
import ManageVisibility from "./manage-visibility";
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
  visibility: ThreadVisibilitySummary;
};

type LoadState = {
  isLoading: boolean;
  error: Error | null;
  hasLoaded: boolean;
};

type ThreadMessage = Message & {
  threadTitle: string;
};

type InboxFilterValue = "all" | "private" | "shared" | "public" | `team:${string}`;

const PAGE_SIZE = 25;
const SEARCH_DEBOUNCE_MS = 300;
const IMAGE_PREVIEW_URL_EXPIRY_SECONDS = 60 * 60;
const INBOX_FILTER_STORAGE_KEY = "agentbox.inbox.filter";
const EMPTY_PAGE: ThreadPageInfo = { limit: PAGE_SIZE, has_more: false };

export default function BrowseThreads() {
  const [searchText, setSearchText] = useState("");
  const [threads, setThreads] = useState<ListedThread[]>([]);
  const [page, setPage] = useState<ThreadPageInfo>(EMPTY_PAGE);
  const [teams, setTeams] = useState<Team[]>([]);
  const [filterValue, setFilterValue] = useState<InboxFilterValue>("all");
  const [filterReady, setFilterReady] = useState(false);
  const [loadState, setLoadState] = useState<LoadState>({ isLoading: true, error: null, hasLoaded: false });
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const requestId = useRef(0);
  const loadingMoreRef = useRef(false);

  const trimmedSearch = searchText.trim();

  useEffect(() => {
    let cancelled = false;
    async function restoreFilter() {
      const stored = await LocalStorage.getItem<string>(INBOX_FILTER_STORAGE_KEY);
      if (!cancelled) {
        setFilterValue(parseInboxFilter(stored));
        setFilterReady(true);
      }
    }
    void restoreFilter();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    async function loadCallerTeams() {
      try {
        const data = await listTeams();
        if (cancelled) return;
        setTeams(data);
        setFilterValue((current) => {
          if (!current.startsWith("team:")) return current;
          const slug = current.slice("team:".length);
          if (data.some((team) => team.slug === slug)) return current;
          void LocalStorage.setItem(INBOX_FILTER_STORAGE_KEY, "all");
          return "all";
        });
      } catch (error) {
        if (!cancelled) {
          await showToast({
            style: Toast.Style.Failure,
            title: "Could not load team filters",
            message: normalizeError(error).message,
          });
        }
      }
    }
    void loadCallerTeams();
    return () => {
      cancelled = true;
    };
  }, [refreshKey]);

  const loadThreads = useCallback(
    async ({
      append,
      cursor,
      filter,
      query,
      runId,
    }: {
      append: boolean;
      cursor?: string;
      filter: InboxFilterValue;
      query: string;
      runId: number;
    }) => {
      if (append) {
        loadingMoreRef.current = true;
        setIsLoadingMore(true);
      } else {
        setLoadState((current) => ({ ...current, isLoading: true, error: null }));
      }
      try {
        const filterParams = threadFilterParams(filter);
        const response = query
          ? await searchThreadPage({ query, limit: PAGE_SIZE, cursor, ...filterParams })
          : await listThreadPage({ limit: PAGE_SIZE, cursor, ...filterParams });
        if (requestId.current !== runId) return;
        const data = query
          ? response.threads.map((thread) => threadFromSearchResult(thread as SearchThreadResult))
          : response.threads.map((thread) => threadFromRecent(thread as Thread));
        setThreads((current) => (append ? appendUniqueThreads(current, data) : data));
        setPage(response.page);
        setLoadState({ isLoading: false, error: null, hasLoaded: true });
      } catch (error) {
        if (requestId.current !== runId) return;
        const normalized = normalizeError(error);
        if (!append) setThreads([]);
        setLoadState({ isLoading: false, error: normalized, hasLoaded: true });
        await showToast({
          style: Toast.Style.Failure,
          title: append ? "Could not load more threads" : "Could not load threads",
          message: normalized.message,
        });
      } finally {
        if (requestId.current === runId) {
          loadingMoreRef.current = false;
          setIsLoadingMore(false);
        }
      }
    },
    [],
  );

  useEffect(() => {
    if (!filterReady) return;
    const runId = requestId.current + 1;
    requestId.current = runId;
    const timeout = setTimeout(
      () => {
        void loadThreads({ append: false, filter: filterValue, query: trimmedSearch, runId });
      },
      trimmedSearch ? SEARCH_DEBOUNCE_MS : 0,
    );
    return () => clearTimeout(timeout);
  }, [filterReady, filterValue, loadThreads, refreshKey, trimmedSearch]);

  function resetForRequestChange() {
    requestId.current += 1;
    loadingMoreRef.current = false;
    setIsLoadingMore(false);
    setThreads([]);
    setPage(EMPTY_PAGE);
  }

  function handleSearchTextChange(value: string) {
    if (value === searchText) return;
    resetForRequestChange();
    setSearchText(value);
  }

  function handleFilterChange(value: string) {
    const next = parseInboxFilter(value);
    if (next === filterValue) return;
    resetForRequestChange();
    setFilterValue(next);
    void LocalStorage.setItem(INBOX_FILTER_STORAGE_KEY, next);
  }

  function refresh() {
    resetForRequestChange();
    setRefreshKey((value) => value + 1);
  }

  function removeThread(threadId: string) {
    requestId.current += 1;
    loadingMoreRef.current = false;
    setIsLoadingMore(false);
    setThreads((current) => current.filter((thread) => thread.id !== threadId));
    setPage(EMPTY_PAGE);
    setRefreshKey((value) => value + 1);
  }

  function loadMore() {
    const cursor = page.next_cursor;
    if (!cursor || loadingMoreRef.current) return;
    void loadThreads({
      append: true,
      cursor,
      filter: filterValue,
      query: trimmedSearch,
      runId: requestId.current,
    });
  }

  const emptyView = (
    <ThreadEmptyView
      error={loadState.error}
      hasLoaded={loadState.hasLoaded}
      isSearching={Boolean(trimmedSearch)}
      onRefresh={refresh}
    />
  );

  return (
    <List
      filtering={false}
      isLoading={loadState.isLoading || isLoadingMore || !filterReady}
      isShowingDetail
      onSearchTextChange={handleSearchTextChange}
      pagination={{ pageSize: PAGE_SIZE, hasMore: Boolean(page.next_cursor), onLoadMore: loadMore }}
      searchBarAccessory={<ThreadFilterDropdown teams={teams} value={filterValue} onChange={handleFilterChange} />}
      searchBarPlaceholder="Search Agentbox threads"
      searchText={searchText}
    >
      {threads.length === 0 ? (
        emptyView
      ) : (
        <List.Section
          title={trimmedSearch ? "Search Results" : inboxFilterTitle(filterValue, teams)}
          subtitle={`${threads.length}${page.has_more ? "+" : ""}`}
        >
          {threads.map((thread) => (
            <ThreadListItem key={thread.id} thread={thread} onRefresh={refresh} onThreadRemoved={removeThread} />
          ))}
        </List.Section>
      )}
    </List>
  );
}

function ThreadFilterDropdown({
  onChange,
  teams,
  value,
}: {
  onChange: (value: string) => void;
  teams: Team[];
  value: InboxFilterValue;
}) {
  return (
    <List.Dropdown tooltip="Filter accessible threads" value={value} onChange={onChange}>
      <List.Dropdown.Section title="Visibility">
        <List.Dropdown.Item value="all" title="All Accessible" />
        <List.Dropdown.Item value="private" title="Private to Me" />
        <List.Dropdown.Item value="shared" title="Shared with Me" />
        <List.Dropdown.Item value="public" title="Public" />
      </List.Dropdown.Section>
      {teams.length > 0 && (
        <List.Dropdown.Section title="Teams">
          {teams.map((team) => (
            <List.Dropdown.Item key={team.id} value={`team:${team.slug}`} title={team.name} />
          ))}
        </List.Dropdown.Section>
      )}
    </List.Dropdown>
  );
}

function parseInboxFilter(value: string | undefined): InboxFilterValue {
  const trimmed = value?.trim() ?? "";
  if (trimmed === "private" || trimmed === "shared" || trimmed === "public") return trimmed;
  if (trimmed.startsWith("team:") && trimmed.slice("team:".length).trim()) {
    return `team:${trimmed.slice("team:".length).trim()}`;
  }
  return "all";
}

function threadFilterParams(value: InboxFilterValue): { filter: ThreadFilter; team?: string } {
  if (value.startsWith("team:")) {
    return { filter: "team", team: value.slice("team:".length) };
  }
  switch (value) {
    case "private":
      return { filter: "private" };
    case "shared":
      return { filter: "shared" };
    case "public":
      return { filter: "public" };
    default:
      return { filter: "all" };
  }
}

function appendUniqueThreads(current: ListedThread[], incoming: ListedThread[]): ListedThread[] {
  const byID = new Map(current.map((thread) => [thread.id, thread]));
  for (const thread of incoming) byID.set(thread.id, thread);
  return Array.from(byID.values());
}

function inboxFilterTitle(value: InboxFilterValue, teams: Team[]): string {
  if (value === "private") return "Private to Me";
  if (value === "shared") return "Shared with Me";
  if (value === "public") return "Public Threads";
  if (value.startsWith("team:")) {
    const slug = value.slice("team:".length);
    return teams.find((team) => team.slug === slug)?.name ?? "Team Threads";
  }
  return "All Accessible Threads";
}

function ThreadListItem({
  onRefresh,
  onThreadRemoved,
  thread,
}: {
  onRefresh: () => void;
  onThreadRemoved: (threadId: string) => void;
  thread: ListedThread;
}) {
  return (
    <List.Item
      id={thread.id}
      title={thread.title || thread.id}
      subtitle={thread.lastMessagePreview}
      accessories={threadAccessories(thread)}
      detail={
        <List.Item.Detail markdown={threadListMarkdown(thread)} metadata={<ThreadListMetadata thread={thread} />} />
      }
      actions={<ThreadActions thread={thread} onRefresh={onRefresh} onThreadRemoved={onThreadRemoved} />}
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

    const assetsNeedingSignedUrls = imageAssets.filter(
      (asset) => !asset.preview_url && !asset.download_url && !asset.purged_at && !asset.unavailable,
    );
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

function ThreadActions({
  onRefresh,
  onThreadRemoved,
  thread,
}: {
  onRefresh: () => void;
  onThreadRemoved: (threadId: string) => void;
  thread: ListedThread;
}) {
  const threadUrl = safeDashboardThreadUrl(thread.id);

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
        <Action.Push
          title="Manage Visibility"
          icon={Icon.Eye}
          target={
            <ManageVisibility
              threadId={thread.id}
              threadTitle={thread.title}
              onChanged={onRefresh}
              onSelfRevoked={onThreadRemoved}
            />
          }
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
  const labels = visibilityLabels(thread.visibility);
  return (
    <List.Item.Detail.Metadata>
      <List.Item.Detail.Metadata.Label title="Creator" text={thread.createdBy || "Unknown"} />
      <List.Item.Detail.Metadata.Label title="Visibility" text={labels.join(" · ")} />
      <List.Item.Detail.Metadata.Label title="Owned by Me" text={thread.visibility.owned_by_me ? "Yes" : "No"} />
      {thread.visibility.matched_teams.length > 0 && (
        <List.Item.Detail.Metadata.Label
          title="Matched Teams"
          text={thread.visibility.matched_teams.map((team) => team.name).join(", ")}
        />
      )}
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
    createdBy: attributionLabel(thread, thread.created_by),
    matchedSnippets: [],
    visibility: thread.visibility_summary,
  };
}

function threadFromSearchResult(thread: SearchThreadResult): ListedThread {
  return {
    id: thread.id,
    title: thread.title,
    createdAt: thread.created_at,
    updatedAt: thread.updated_at,
    createdBy: attributionLabel(thread, thread.created_by),
    messageCount: thread.message_count,
    lastMessagePreview: thread.last_message_preview,
    matchedSnippets: thread.matched_snippets ?? [],
    visibility: thread.visibility_summary,
  };
}

function threadAccessories(thread: ListedThread): List.Item.Accessory[] {
  const accessories: List.Item.Accessory[] = [];
  accessories.push({ text: visibilityLabels(thread.visibility).join(" · "), icon: Icon.Eye });
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
  const lines = [
    `# ${escapeMarkdown(thread.title || thread.id)}`,
    "",
    `**Visibility:** ${escapeMarkdown(visibilityLabels(thread.visibility).join(" · "))}`,
    "",
    `**Created by:** ${escapeMarkdown(thread.createdBy || "Unknown")}`,
  ];
  if (thread.visibility.matched_teams.length > 0) {
    lines.push(
      "",
      `**Access through:** ${escapeMarkdown(thread.visibility.matched_teams.map((team) => team.name).join(", "))}`,
    );
  }
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
      <List.Item.Detail.Metadata.Label title="Author" text={attributionLabel(message, message.author)} />
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
    accessories.push({ text: attributionLabel(message, message.author), icon: Icon.Person });
  }
  if (message.created_at) {
    accessories.push({ date: new Date(message.created_at), tooltip: `Sent ${formatDate(message.created_at)}` });
  }
  return accessories;
}

function safeDashboardThreadUrl(threadId: string): string {
  return dashboardThreadUrl(threadId);
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
