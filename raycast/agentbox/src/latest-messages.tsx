import { Action, ActionPanel, Icon, Keyboard, List, Toast, openExtensionPreferences, showToast } from "@raycast/api";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AgentboxAPIError, Message, ThreadWithMessages, dashboardThreadUrl, getThread, listThreads } from "./api";
import { AttachmentActions } from "./attachment-actions";
import { formatDate, messageMarkdown } from "./markdown";
import PostMessage from "./post-message";
import { AgentboxUtilityActions } from "./utility-actions";

type InboxMessage = Message & {
  threadTitle: string;
  threadUpdatedAt: string;
};

type LoadState = {
  isLoading: boolean;
  error: Error | null;
  hasLoaded: boolean;
};

const THREAD_LIMIT = 30;
const MESSAGE_LIMIT = 100;

export default function LatestMessages() {
  const [searchText, setSearchText] = useState("");
  const [messages, setMessages] = useState<InboxMessage[]>([]);
  const [loadState, setLoadState] = useState<LoadState>({ isLoading: true, error: null, hasLoaded: false });
  const [refreshKey, setRefreshKey] = useState(0);
  const requestId = useRef(0);

  const loadMessages = useCallback(async (runId: number) => {
    setLoadState((current) => ({ ...current, isLoading: true, error: null }));
    try {
      const recentThreads = await listThreads(THREAD_LIMIT);
      const detailedThreads = await Promise.all(recentThreads.map((thread) => getThread(thread.id)));
      if (requestId.current !== runId) {
        return;
      }

      setMessages(flattenMessages(detailedThreads));
      setLoadState({ isLoading: false, error: null, hasLoaded: true });
    } catch (error) {
      if (requestId.current !== runId) {
        return;
      }
      const normalized = normalizeError(error);
      setMessages([]);
      setLoadState({ isLoading: false, error: normalized, hasLoaded: true });
      await showToast({
        style: Toast.Style.Failure,
        title: "Could not load messages",
        message: normalized.message,
      });
    }
  }, []);

  useEffect(() => {
    const runId = requestId.current + 1;
    requestId.current = runId;
    void loadMessages(runId);
  }, [loadMessages, refreshKey]);

  const filteredMessages = useMemo(() => filterMessages(messages, searchText), [messages, searchText]);
  const emptyView = (
    <InboxEmptyView
      error={loadState.error}
      hasLoaded={loadState.hasLoaded}
      isSearching={Boolean(searchText.trim())}
      onRefresh={() => setRefreshKey((value) => value + 1)}
    />
  );

  return (
    <List
      filtering={false}
      isLoading={loadState.isLoading}
      isShowingDetail
      onSearchTextChange={setSearchText}
      searchBarPlaceholder="Search recent Agentbox messages"
      searchText={searchText}
    >
      {filteredMessages.length === 0 ? (
        emptyView
      ) : (
        <List.Section title="Latest Messages" subtitle={`${filteredMessages.length}`}>
          {filteredMessages.map((message) => (
            <MessageListItem key={message.id} message={message} onRefresh={() => setRefreshKey((value) => value + 1)} />
          ))}
        </List.Section>
      )}
    </List>
  );
}

function MessageListItem({ message, onRefresh }: { message: InboxMessage; onRefresh: () => void }) {
  return (
    <List.Item
      id={message.id}
      title={messageTitle(message)}
      subtitle={message.threadTitle || message.thread_id}
      accessories={messageAccessories(message)}
      detail={<List.Item.Detail markdown={messageMarkdown(message)} metadata={<MessageMetadata message={message} />} />}
      actions={<MessageActions message={message} onRefresh={onRefresh} />}
    />
  );
}

function MessageActions({ message, onRefresh }: { message: InboxMessage; onRefresh: () => void }) {
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
        <Action.OpenInBrowser title="Open Thread in Dashboard" icon={Icon.Globe} url={threadUrl} />
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
        <Action.CopyToClipboard title="Copy Thread URL" content={threadUrl} />
        <Action.CopyToClipboard title="Copy Thread ID" content={message.thread_id} />
        <Action.CopyToClipboard title="Copy Message ID" content={message.id} shortcut={Keyboard.Shortcut.Common.Copy} />
      </ActionPanel.Section>
      <AttachmentActions
        assets={message.assets.map((asset) => ({ ...asset, messageId: message.id }))}
        title="Message Attachments"
      />
      <AgentboxUtilityActions />
    </ActionPanel>
  );
}

function InboxEmptyView({
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
        title={configError ? "Configure Agentbox" : "Could Not Load Messages"}
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
    return <List.EmptyView icon={Icon.Tray} title="Loading Latest Messages" />;
  }

  return (
    <List.EmptyView
      icon={isSearching ? Icon.MagnifyingGlass : Icon.Tray}
      title={isSearching ? "No Matching Messages" : "No Messages Yet"}
      description={isSearching ? "No recent messages matched this search." : "Your Agentbox inbox is empty."}
      actions={
        <ActionPanel>
          <Action title="Refresh" icon={Icon.ArrowClockwise} onAction={onRefresh} />
        </ActionPanel>
      }
    />
  );
}

function MessageMetadata({ message }: { message: InboxMessage }) {
  return (
    <List.Item.Detail.Metadata>
      <List.Item.Detail.Metadata.Label title="Thread" text={message.threadTitle || message.thread_id} />
      <List.Item.Detail.Metadata.Label title="Author" text={message.author || "Unknown"} />
      <List.Item.Detail.Metadata.Label title="Attachments" text={String(message.assets.length)} />
      <List.Item.Detail.Metadata.Label title="Created" text={formatDate(message.created_at)} />
      <List.Item.Detail.Metadata.Separator />
      <List.Item.Detail.Metadata.Label title="Thread ID" text={message.thread_id} />
      <List.Item.Detail.Metadata.Label title="Message ID" text={message.id} />
      <List.Item.Detail.Metadata.Link
        title="Dashboard"
        text="Open thread"
        target={safeDashboardThreadUrl(message.thread_id)}
      />
    </List.Item.Detail.Metadata>
  );
}

function flattenMessages(threads: ThreadWithMessages[]): InboxMessage[] {
  return threads
    .flatMap((thread) =>
      thread.messages.map((message) => ({
        ...message,
        threadTitle: thread.title,
        threadUpdatedAt: thread.updated_at,
      })),
    )
    .sort((left, right) => new Date(right.created_at).getTime() - new Date(left.created_at).getTime())
    .slice(0, MESSAGE_LIMIT);
}

function filterMessages(messages: InboxMessage[], searchText: string): InboxMessage[] {
  const query = searchText.trim().toLowerCase();
  if (!query) {
    return messages;
  }
  return messages.filter((message) => {
    return (
      message.body.toLowerCase().includes(query) ||
      message.author.toLowerCase().includes(query) ||
      message.threadTitle.toLowerCase().includes(query) ||
      message.thread_id.toLowerCase().includes(query)
    );
  });
}

function messageTitle(message: InboxMessage): string {
  const preview = message.body.replace(/\s+/g, " ").trim();
  if (preview.length > 120) {
    return `${preview.slice(0, 117)}...`;
  }
  return preview || (message.assets.length > 0 ? `${message.assets.length} attachment message` : "Empty message");
}

function messageAccessories(message: InboxMessage): List.Item.Accessory[] {
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
