"use client";

import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { AlignLeftIcon, WandSparklesIcon } from "lucide-react";
import { Button } from "@/components/ui/button";

type MarkdownPreference = {
  allPlainAsMarkdown: boolean;
  messagePreferences: Record<string, boolean>;
};

type ServerPreferenceState = {
  key: string;
  preference: MarkdownPreference;
  ready: boolean;
  error: string | null;
};

const STORAGE_PREFIX = "agentbox:thread-markdown:v1:";
const CHANGE_EVENT = "agentbox:thread-markdown-change";
const fallbackStorage = new Map<string, string>();
const EMPTY_PREFERENCE: MarkdownPreference = { allPlainAsMarkdown: false, messagePreferences: {} };

function parsePreference(raw: string): MarkdownPreference {
  try {
    const value = JSON.parse(raw);
    if (!value || typeof value !== "object") throw new Error("Invalid saved preference");
    const messagePreferences: Record<string, boolean> = {};
    const overrides = value.message_preferences ?? value.messagePreferences;
    if (overrides && typeof overrides === "object") {
      for (const [messageId, enabled] of Object.entries(overrides)) {
        if (typeof enabled === "boolean") messagePreferences[messageId] = enabled;
      }
    }
    return { allPlainAsMarkdown: (value.all_plain_as_markdown ?? value.allPlainAsMarkdown) === true, messagePreferences };
  } catch {
    return { allPlainAsMarkdown: false, messagePreferences: {} };
  }
}

function readSavedPreference(key: string) {
  if (typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem(key) ?? "";
  } catch {
    return fallbackStorage.get(key) ?? "";
  }
}

function savePreference(key: string, update: (current: MarkdownPreference) => MarkdownPreference) {
  const serialized = JSON.stringify(update(parsePreference(readSavedPreference(key))));
  try {
    window.localStorage.setItem(key, serialized);
  } catch {
    fallbackStorage.set(key, serialized);
  }
  window.dispatchEvent(new CustomEvent(CHANGE_EVENT, { detail: key }));
}

function useLocalThreadMarkdownPreview(threadId: string) {
  const key = `${STORAGE_PREFIX}${threadId}`;
  const subscribe = useCallback((notify: () => void) => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === key || event.key === null) notify();
    };
    const onLocalChange = (event: Event) => {
      if ((event as CustomEvent<string>).detail === key) notify();
    };
    window.addEventListener("storage", onStorage);
    window.addEventListener(CHANGE_EVENT, onLocalChange);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener(CHANGE_EVENT, onLocalChange);
    };
  }, [key]);
  const snapshot = useCallback(() => readSavedPreference(key), [key]);
  const raw = useSyncExternalStore(subscribe, snapshot, () => "");
  const preferences = useMemo(() => parsePreference(raw), [raw]);

  const toggleAll = useCallback(() => {
    savePreference(key, (current) => ({
      allPlainAsMarkdown: !current.allPlainAsMarkdown,
      messagePreferences: {}
    }));
  }, [key]);

  const setMessageMarkdown = useCallback((messageId: string, enabled: boolean) => {
    savePreference(key, (current) => ({
      ...current,
      messagePreferences: { ...current.messagePreferences, [messageId]: enabled }
    }));
  }, [key]);

  const isMessageMarkdown = useCallback(
    (messageId: string) => preferences.messagePreferences[messageId] ?? preferences.allPlainAsMarkdown,
    [preferences]
  );

  return { allPlainAsMarkdown: preferences.allPlainAsMarkdown, toggleAll, setMessageMarkdown, isMessageMarkdown };
}

function useServerThreadMarkdownPreview(threadId: string, enabled: boolean) {
  const [state, setState] = useState<ServerPreferenceState>({ key: "", preference: EMPTY_PREFERENCE, ready: false, error: null });
  const latestRef = useRef({ key: "", preference: EMPTY_PREFERENCE });
  const writeQueue = useRef<Promise<void>>(Promise.resolve());
  const url = `/api/threads/${encodeURIComponent(threadId)}/display-preference`;

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    void (async () => {
      try {
        const response = await fetch(url, { cache: "no-store", signal: controller.signal });
        const payload = await response.json();
        if (!response.ok) throw new Error(payload.error ?? `HTTP ${response.status}`);
        if (controller.signal.aborted) return;
        const preference = parsePreference(JSON.stringify(payload.preference));
        latestRef.current = { key: threadId, preference };
        setState({ key: threadId, preference, ready: true, error: null });
      } catch (err) {
        if (controller.signal.aborted) return;
        latestRef.current = { key: threadId, preference: EMPTY_PREFERENCE };
        setState({
          key: threadId,
          preference: EMPTY_PREFERENCE,
          ready: true,
          error: `Could not load saved Markdown preferences: ${err instanceof Error ? err.message : String(err)}`
        });
      }
    })();
    return () => controller.abort();
  }, [enabled, threadId, url]);

  const update = useCallback((modify: (preference: MarkdownPreference) => MarkdownPreference) => {
    if (!enabled || latestRef.current.key !== threadId) return;
    const preference = modify(latestRef.current.preference);
    latestRef.current = { key: threadId, preference };
    setState({ key: threadId, preference, ready: true, error: null });

    // Serialize quick successive clicks so a slower response cannot overwrite
    // a newer choice on the server. Writes only occur after user interaction.
    writeQueue.current = writeQueue.current.catch(() => undefined).then(async () => {
      const body = JSON.stringify({
        all_plain_as_markdown: preference.allPlainAsMarkdown,
        message_preferences: preference.messagePreferences
      });
      const response = await fetch(url, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body,
        // A quick tab close should not cancel an ordinary small preference save.
        keepalive: body.length < 60_000
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}));
        throw new Error(payload.error ?? `HTTP ${response.status}`);
      }
    }).catch((err) => {
      setState((current) => current.key === threadId ? {
        ...current,
        error: `Markdown display change was not saved: ${err instanceof Error ? err.message : String(err)}`
      } : current);
    });
  }, [enabled, threadId, url]);

  const toggleAll = useCallback(() => {
    update((current) => ({ allPlainAsMarkdown: !current.allPlainAsMarkdown, messagePreferences: {} }));
  }, [update]);
  const setMessageMarkdown = useCallback((messageId: string, isMarkdown: boolean) => {
    update((current) => ({
      ...current,
      messagePreferences: { ...current.messagePreferences, [messageId]: isMarkdown }
    }));
  }, [update]);

  const current = state.key === threadId && enabled ? state : null;
  const preference = current?.preference ?? EMPTY_PREFERENCE;
  const isMessageMarkdown = useCallback((messageId: string) => (
    preference.messagePreferences[messageId] ?? preference.allPlainAsMarkdown
  ), [preference]);

  return {
    allPlainAsMarkdown: preference.allPlainAsMarkdown,
    toggleAll,
    setMessageMarkdown,
    isMessageMarkdown,
    ready: current?.ready ?? false,
    error: current?.error ?? null
  };
}

/** A user's private choice syncs across devices; anonymous shares remain local. */
export function useThreadMarkdownPreview(threadId: string, storage: "server" | "local" = "server") {
  const local = useLocalThreadMarkdownPreview(threadId);
  const server = useServerThreadMarkdownPreview(threadId, storage === "server");
  return storage === "server" ? server : { ...local, ready: true, error: null };
}

export function ThreadMarkdownPreviewButton({
  active,
  count,
  onToggle
}: {
  active: boolean;
  count: number;
  onToggle: () => void;
}) {
  if (count === 0) return null;

  const label = active ? "Restore original message formatting" : "Try Markdown for all plain-text messages";
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? "secondary" : "outline"}
      aria-label={label}
      aria-pressed={active}
      title={label}
      onClick={onToggle}
      className="h-9 min-w-9 gap-2 px-2.5 sm:px-3"
    >
      {active ? <AlignLeftIcon aria-hidden="true" /> : <WandSparklesIcon aria-hidden="true" />}
      <span className="hidden sm:inline">{active ? "Original formatting" : "Try Markdown for thread"}</span>
    </Button>
  );
}
