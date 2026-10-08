"use client";

import { useCallback, useState } from "react";
import { RotateCcwIcon, WandSparklesIcon } from "lucide-react";
import { Button } from "@/components/ui/button";

/** Reader-only rendering preferences. Nothing is written back to the message API. */
export function useThreadMarkdownPreview() {
  const [allPlainAsMarkdown, setAllPlainAsMarkdown] = useState(false);
  const [messagePreferences, setMessagePreferences] = useState<Record<string, boolean>>({});

  const toggleAll = useCallback(() => {
    setAllPlainAsMarkdown((current) => !current);
    setMessagePreferences({});
  }, []);

  const setMessageMarkdown = useCallback((messageId: string, enabled: boolean) => {
    setMessagePreferences((current) => ({ ...current, [messageId]: enabled }));
  }, []);

  const reset = useCallback(() => {
    setAllPlainAsMarkdown(false);
    setMessagePreferences({});
  }, []);

  const isMessageMarkdown = useCallback(
    (messageId: string) => messagePreferences[messageId] ?? allPlainAsMarkdown,
    [allPlainAsMarkdown, messagePreferences]
  );

  return { allPlainAsMarkdown, toggleAll, reset, setMessageMarkdown, isMessageMarkdown };
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
      {active ? <RotateCcwIcon aria-hidden="true" /> : <WandSparklesIcon aria-hidden="true" />}
      <span className="hidden sm:inline">{active ? "Original formatting" : "Try Markdown for thread"}</span>
    </Button>
  );
}
