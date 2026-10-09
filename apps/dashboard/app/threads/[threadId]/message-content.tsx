"use client";

import { AlignLeftIcon, Code2Icon, EyeIcon, WandSparklesIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CopyButton } from "../../components/copy-button";
import { MarkdownMessage } from "./markdown-message";
import { inferBodyContentType, messageFormatLabel, normalizeContentType } from "./markdown-utils";

const LARGE_MARKDOWN_THRESHOLD = 300_000;

export function MessageContent({
  body,
  contentType,
  forceMarkdown,
  onForceMarkdownChange,
  hideToolbar = false,
  sourceMode
}: {
  body: string;
  contentType?: string | null;
  forceMarkdown?: boolean;
  onForceMarkdownChange?: (enabled: boolean) => void;
  hideToolbar?: boolean;
  sourceMode?: boolean;
}) {
  const safeBody = body || "(empty message)";
  const explicitType = normalizeContentType(contentType);
  const inferredType = useMemo(() => inferBodyContentType(body), [body]);
  const resolvedType = explicitType ?? inferredType;
  const wasInferred = explicitType === null;
  const isPlainText = resolvedType === "text/plain";
  const [localMarkdownPreview, setLocalMarkdownPreview] = useState(false);
  const previewMarkdown = isPlainText && (forceMarkdown ?? localMarkdownPreview);
  const [sourcePreference, setSourcePreference] = useState<boolean | null>(null);
  const showSource = sourceMode ?? (previewMarkdown
    ? sourcePreference === true
    : isPlainText || (sourcePreference ?? body.length > LARGE_MARKDOWN_THRESHOLD));

  function toggleMarkdownPreview() {
    const next = !previewMarkdown;
    setSourcePreference(null);
    if (onForceMarkdownChange) onForceMarkdownChange(next);
    else setLocalMarkdownPreview(next);
  }

  const previewAction = isPlainText ? (
    <Button
      variant={previewMarkdown ? "secondary" : "outline"}
      size="sm"
      type="button"
      onClick={toggleMarkdownPreview}
      aria-label={previewMarkdown ? "Show original plain text" : "Attempt Markdown rendering"}
      title={previewMarkdown ? "Show original plain text" : "Attempt Markdown rendering"}
      aria-pressed={previewMarkdown}
      className="h-9 min-w-9 px-2.5 sm:h-8"
    >
      {previewMarkdown ? <AlignLeftIcon aria-hidden="true" /> : <WandSparklesIcon aria-hidden="true" />}
      <span className="hidden sm:inline">{previewMarkdown ? "Plain text" : "Try Markdown"}</span>
    </Button>
  ) : null;

  if (showSource) {
    return (
      <div className="flex min-w-0 flex-col gap-3 sm:gap-5">
        {!hideToolbar ? <MessageToolbar
          label={previewMarkdown ? "Markdown · preview" : messageFormatLabel(resolvedType, wasInferred)}
          body={body}
          action={
            <>
              {previewAction}
              {resolvedType === "text/markdown" || previewMarkdown ? (
                <Button variant="outline" size="sm" type="button" className="h-9 sm:h-8" onClick={() => setSourcePreference(false)} title="Show rendered Markdown">
                  <EyeIcon aria-hidden="true" />
                  <span className="hidden sm:inline">Rendered</span>
                </Button>
              ) : null}
            </>
          }
        /> : null}
        <pre className="max-h-[60rem] min-w-0 overflow-auto whitespace-pre-wrap break-words border bg-[var(--panel-code-bg)] p-3 font-mono text-[0.84rem]/relaxed text-[var(--panel-code-foreground)] sm:p-6 sm:text-sm/7">
          {safeBody}
        </pre>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3 sm:gap-5">
      {!hideToolbar ? <MessageToolbar
        label={previewMarkdown ? "Markdown · preview" : messageFormatLabel(resolvedType, wasInferred)}
        body={body}
        action={
          <>
            {previewAction}
            <Button variant="outline" size="sm" type="button" className="h-9 sm:h-8" onClick={() => setSourcePreference(true)} title="Show Markdown source" aria-label="Show Markdown source">
              <Code2Icon aria-hidden="true" />
              <span className="hidden sm:inline">Raw</span>
            </Button>
          </>
        }
      /> : null}
      <MarkdownMessage body={body} />
    </div>
  );
}

function MessageToolbar({
  label,
  body,
  action
}: {
  label: string;
  body: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 border-b pb-3 sm:gap-3 sm:pb-4">
      <Badge variant="secondary">{label}</Badge>
      <div className="flex flex-wrap items-center gap-1.5 sm:gap-2">
        <CopyButton value={body} label="Copy message" />
        {action}
      </div>
    </div>
  );
}
