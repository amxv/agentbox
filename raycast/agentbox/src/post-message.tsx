import {
  Action,
  ActionPanel,
  Clipboard,
  Form,
  Icon,
  List,
  Toast,
  open,
  openExtensionPreferences,
  showToast,
} from "@raycast/api";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AgentboxAPIError,
  SearchThreadResult,
  Thread,
  ThreadPageInfo,
  attributionLabel,
  dashboardThreadUrl,
  listThreadPage,
  postMessage,
  searchThreadPage,
  visibilityLabels,
} from "./api";
import { BODY_FORMATS, FormValuesBase, normalizeFormError, uploadFilesForThread } from "./form-helpers";
import { escapeMarkdown } from "./markdown";
import { AgentboxUtilityActions } from "./utility-actions";

type PostMessageValues = FormValuesBase & {
  threadId: string;
  body: string;
};

type PostMessageProps = {
  initialThreadId?: string;
  initialThreadTitle?: string;
  launchContext?: {
    threadId?: string;
    threadTitle?: string;
  };
  arguments?: {
    threadId?: string;
  };
};

type ThreadChoice = {
  id: string;
  title: string;
  updatedAt: string;
  attribution: string;
  visibility: string[];
  messageCount?: number;
  preview?: string;
};

const PAGE_SIZE = 25;
const SEARCH_DEBOUNCE_MS = 300;
const EMPTY_PAGE: ThreadPageInfo = { limit: PAGE_SIZE, has_more: false };

export default function PostMessage(props: PostMessageProps) {
  const initialThreadId = useMemo(
    () => props.initialThreadId ?? props.launchContext?.threadId ?? props.arguments?.threadId ?? "",
    [props.arguments?.threadId, props.initialThreadId, props.launchContext?.threadId],
  );
  const initialThreadTitle = props.initialThreadTitle ?? props.launchContext?.threadTitle;
  if (initialThreadId) {
    return <PostMessageForm initialThreadId={initialThreadId} initialThreadTitle={initialThreadTitle} />;
  }
  return <PostThreadPicker />;
}

function PostThreadPicker() {
  const [searchText, setSearchText] = useState("");
  const [threads, setThreads] = useState<ThreadChoice[]>([]);
  const [page, setPage] = useState<ThreadPageInfo>(EMPTY_PAGE);
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const requestId = useRef(0);
  const loadingMoreRef = useRef(false);
  const query = searchText.trim();

  const load = useCallback(
    async ({
      append,
      cursor,
      queryText,
      runId,
    }: {
      append: boolean;
      cursor?: string;
      queryText: string;
      runId: number;
    }) => {
      if (append) {
        loadingMoreRef.current = true;
        setIsLoadingMore(true);
      } else {
        setIsLoading(true);
        setError(null);
      }
      try {
        const response = queryText
          ? await searchThreadPage({ query: queryText, limit: PAGE_SIZE, cursor, filter: "all" })
          : await listThreadPage({ limit: PAGE_SIZE, cursor, filter: "all" });
        if (requestId.current !== runId) return;
        const choices = queryText
          ? response.threads.map((thread) => searchChoice(thread as SearchThreadResult))
          : response.threads.map((thread) => listChoice(thread as Thread));
        setThreads((current) => (append ? appendUniqueChoices(current, choices) : choices));
        setPage(response.page);
        setError(null);
      } catch (loadError) {
        if (requestId.current !== runId) return;
        const normalized = normalizePickerError(loadError);
        if (!append) setThreads([]);
        setError(normalized);
        await showToast({
          style: Toast.Style.Failure,
          title: append ? "Could not load more threads" : "Could not load accessible threads",
          message: normalized.message,
        });
      } finally {
        if (requestId.current === runId) {
          loadingMoreRef.current = false;
          setIsLoadingMore(false);
          setIsLoading(false);
        }
      }
    },
    [],
  );

  useEffect(() => {
    const runId = requestId.current + 1;
    requestId.current = runId;
    const timeout = setTimeout(
      () => void load({ append: false, queryText: query, runId }),
      query ? SEARCH_DEBOUNCE_MS : 0,
    );
    return () => clearTimeout(timeout);
  }, [load, query]);

  function resetForSearch() {
    requestId.current += 1;
    loadingMoreRef.current = false;
    setIsLoadingMore(false);
    setThreads([]);
    setPage(EMPTY_PAGE);
  }

  function handleSearchTextChange(value: string) {
    if (value === searchText) return;
    resetForSearch();
    setSearchText(value);
  }

  function loadMore() {
    const cursor = page.next_cursor;
    if (!cursor || loadingMoreRef.current) return;
    void load({ append: true, cursor, queryText: query, runId: requestId.current });
  }

  function refresh() {
    resetForSearch();
    const runId = requestId.current;
    void load({ append: false, queryText: query, runId });
  }

  return (
    <List
      filtering={false}
      isLoading={isLoading || isLoadingMore}
      isShowingDetail
      onSearchTextChange={handleSearchTextChange}
      pagination={{ pageSize: PAGE_SIZE, hasMore: Boolean(page.next_cursor), onLoadMore: loadMore }}
      searchBarPlaceholder="Choose an accessible thread"
      searchText={searchText}
    >
      {error ? (
        <List.EmptyView
          icon={Icon.Warning}
          title="Could not load accessible threads"
          description={error.message}
          actions={
            <ActionPanel>
              <Action title="Retry" icon={Icon.ArrowClockwise} onAction={refresh} />
              <Action.Push
                title="Enter Thread ID Manually"
                icon={Icon.Terminal}
                target={<PostMessageForm allowThreadIDEntry />}
              />
              <Action
                title="Open Extension Preferences"
                icon={Icon.Gear}
                onAction={() => void openExtensionPreferences()}
              />
            </ActionPanel>
          }
        />
      ) : !isLoading && threads.length === 0 ? (
        <List.EmptyView
          icon={Icon.Message}
          title={query ? "No accessible threads matched" : "No accessible threads"}
          description={
            query
              ? "Try another search or use the expert thread-ID path."
              : "Create a private thread first, or enter a known accessible thread ID."
          }
          actions={
            <ActionPanel>
              <Action.Push
                title="Enter Thread ID Manually"
                icon={Icon.Terminal}
                target={<PostMessageForm allowThreadIDEntry />}
              />
              <AgentboxUtilityActions />
            </ActionPanel>
          }
        />
      ) : (
        <List.Section
          title={query ? "Search Results" : "Accessible Threads"}
          subtitle={`${threads.length}${page.has_more ? "+" : ""}`}
        >
          {threads.map((thread) => (
            <List.Item
              key={thread.id}
              id={thread.id}
              title={thread.title || "Untitled thread"}
              subtitle={thread.attribution}
              accessories={threadChoiceAccessories(thread)}
              detail={<List.Item.Detail markdown={threadChoiceMarkdown(thread)} />}
              actions={<ThreadChoiceActions thread={thread} onRefresh={refresh} />}
            />
          ))}
        </List.Section>
      )}
    </List>
  );
}

function ThreadChoiceActions({ thread, onRefresh }: { thread: ThreadChoice; onRefresh: () => void }) {
  return (
    <ActionPanel>
      <ActionPanel.Section>
        <Action.Push
          title="Post to Thread"
          icon={Icon.Message}
          target={<PostMessageForm initialThreadId={thread.id} initialThreadTitle={thread.title} />}
        />
        <Action.OpenInBrowser title="Open Thread in Dashboard" icon={Icon.Globe} url={dashboardThreadUrl(thread.id)} />
        <Action title="Refresh Accessible Threads" icon={Icon.ArrowClockwise} onAction={onRefresh} />
      </ActionPanel.Section>
      <ActionPanel.Section title="Expert">
        <Action.Push
          title="Enter Thread ID Manually"
          icon={Icon.Terminal}
          target={<PostMessageForm allowThreadIDEntry />}
        />
        <Action.CopyToClipboard title="Copy Selected Thread ID" content={thread.id} />
      </ActionPanel.Section>
      <AgentboxUtilityActions />
    </ActionPanel>
  );
}

function PostMessageForm({
  allowThreadIDEntry = false,
  initialThreadId = "",
  initialThreadTitle,
}: {
  allowThreadIDEntry?: boolean;
  initialThreadId?: string;
  initialThreadTitle?: string;
}) {
  const [isLoading, setIsLoading] = useState(false);
  const [postedThreadId, setPostedThreadId] = useState<string | null>(initialThreadId || null);
  const [threadIdValue, setThreadIdValue] = useState(initialThreadId);
  const [bodyValue, setBodyValue] = useState("");
  const [threadIdError, setThreadIdError] = useState<string | undefined>();
  const [bodyError, setBodyError] = useState<string | undefined>();

  async function handleSubmit(values: PostMessageValues) {
    if (isLoading) return false;
    const threadId = threadIdValue.trim();
    const body = bodyValue;
    const files = values.files ?? [];
    if (!threadId) {
      setThreadIdError("Thread ID is required.");
      return false;
    }
    if (!body.trim() && files.length === 0) {
      setBodyError("Add a message or at least one attachment.");
      return false;
    }

    setIsLoading(true);
    setThreadIdError(undefined);
    setBodyError(undefined);
    const toast = await showToast({
      style: Toast.Style.Animated,
      title: "Posting message",
      message: initialThreadTitle || threadId,
    });
    try {
      const uploadedAssets = await uploadFilesForThread(threadId, files);
      const message = await postMessage({
        threadId,
        body,
        bodyContentType: values.bodyFormat,
        uploadedAssets,
      });
      setPostedThreadId(threadId);
      toast.style = Toast.Style.Success;
      toast.title = "Posted message";
      toast.message = initialThreadTitle || message.id;
      toast.primaryAction = { title: "Open Thread", onAction: () => void open(dashboardThreadUrl(threadId)) };
      toast.secondaryAction = { title: "Copy Message ID", onAction: () => void Clipboard.copy(message.id) };
      return true;
    } catch (submissionError) {
      const normalized = normalizeFormError(submissionError);
      setBodyError(normalized.message);
      toast.style = Toast.Style.Failure;
      toast.title = "Could not post message";
      toast.message = normalized.message;
      return false;
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Form
      enableDrafts
      isLoading={isLoading}
      navigationTitle={initialThreadTitle ? `Post · ${initialThreadTitle}` : "Post Message"}
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Post Message" icon={Icon.Message} onSubmit={handleSubmit} />
          {postedThreadId && (
            <ActionPanel.Section title="Thread">
              <Action.OpenInBrowser title="Open Thread" url={dashboardThreadUrl(postedThreadId)} icon={Icon.Globe} />
              <Action.CopyToClipboard title="Copy Thread URL" content={dashboardThreadUrl(postedThreadId)} />
              <Action.CopyToClipboard title="Copy Thread ID" content={postedThreadId} />
            </ActionPanel.Section>
          )}
          <AgentboxUtilityActions />
        </ActionPanel>
      }
    >
      {allowThreadIDEntry ? (
        <Form.TextField
          id="threadId"
          title="Thread ID"
          placeholder="thr_..."
          value={threadIdValue}
          onChange={(value) => {
            setThreadIdValue(value);
            setThreadIdError(undefined);
          }}
          error={threadIdError}
        />
      ) : (
        <Form.Description title="Thread" text={initialThreadTitle || initialThreadId} />
      )}
      <Form.TextArea
        id="body"
        title="Message"
        placeholder="Write a message for the thread. Attachments can be posted with an empty body."
        enableMarkdown
        value={bodyValue}
        onChange={(value) => {
          setBodyValue(value);
          setBodyError(undefined);
        }}
        error={bodyError}
      />
      <Form.Dropdown id="bodyFormat" title="Body Format" defaultValue="auto">
        {BODY_FORMATS.map((format) => (
          <Form.Dropdown.Item key={format.value} value={format.value} title={format.title} />
        ))}
      </Form.Dropdown>
      <Form.FilePicker
        id="files"
        title="Attachments"
        allowMultipleSelection
        canChooseDirectories={false}
        canChooseFiles
      />
    </Form>
  );
}

function listChoice(thread: Thread): ThreadChoice {
  return {
    id: thread.id,
    title: thread.title,
    updatedAt: thread.updated_at,
    attribution: attributionLabel(thread, thread.created_by),
    visibility: visibilityLabels(thread.visibility_summary),
  };
}

function searchChoice(thread: SearchThreadResult): ThreadChoice {
  return {
    id: thread.id,
    title: thread.title,
    updatedAt: thread.updated_at,
    attribution: attributionLabel(thread, thread.created_by),
    visibility: visibilityLabels(thread.visibility_summary),
    messageCount: thread.message_count,
    preview: thread.last_message_preview,
  };
}

function appendUniqueChoices(current: ThreadChoice[], incoming: ThreadChoice[]): ThreadChoice[] {
  const byID = new Map(current.map((thread) => [thread.id, thread]));
  for (const thread of incoming) byID.set(thread.id, thread);
  return Array.from(byID.values());
}

function threadChoiceAccessories(thread: ThreadChoice): List.Item.Accessory[] {
  const accessories: List.Item.Accessory[] = [{ text: thread.visibility.join(" · "), icon: Icon.Eye }];
  if (thread.messageCount !== undefined)
    accessories.push({ text: `${thread.messageCount} msg`, icon: Icon.SpeechBubble });
  const updated = safeDate(thread.updatedAt);
  if (updated) accessories.push({ date: updated, tooltip: `Updated ${updated.toLocaleString()}` });
  return accessories;
}

function threadChoiceMarkdown(thread: ThreadChoice): string {
  const lines = [
    `# ${escapeMarkdown(thread.title || "Untitled thread")}`,
    "",
    `**Visibility:** ${escapeMarkdown(thread.visibility.join(" · "))}`,
    "",
    `**Created by:** ${escapeMarkdown(thread.attribution)}`,
  ];
  if (thread.messageCount !== undefined) lines.push("", `**Messages:** ${thread.messageCount}`);
  if (thread.preview) lines.push("", "## Latest message", "", escapeMarkdown(thread.preview));
  return lines.join("\n");
}

function safeDate(value: string): Date | undefined {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

function normalizePickerError(error: unknown): Error {
  if (error instanceof AgentboxAPIError) return error;
  return error instanceof Error ? error : new Error(String(error));
}
