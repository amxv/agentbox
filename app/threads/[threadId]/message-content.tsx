"use client";

import { Code2Icon, EyeIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CopyButton } from "../../components/copy-button";
import { MarkdownMessage } from "./markdown-message";
import { inferBodyContentType, messageFormatLabel, normalizeContentType } from "./markdown-utils";

const LARGE_MARKDOWN_THRESHOLD = 300_000;

export function MessageContent({ body, contentType }: { body: string; contentType?: string | null }) {
  const safeBody = body || "(empty message)";
  const explicitType = normalizeContentType(contentType);
  const inferredType = useMemo(() => inferBodyContentType(body), [body]);
  const resolvedType = explicitType ?? inferredType;
  const wasInferred = explicitType === null;
  const [showSource, setShowSource] = useState(resolvedType === "text/markdown" && body.length > LARGE_MARKDOWN_THRESHOLD);

  if (resolvedType === "text/plain" || showSource) {
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <MessageToolbar
          label={messageFormatLabel(resolvedType, wasInferred)}
          body={body}
          action={resolvedType === "text/markdown" ? (
            <Button variant="outline" size="sm" type="button" onClick={() => setShowSource(false)}>
              <EyeIcon data-icon="inline-start" />
              Rendered
            </Button>
          ) : null}
        />
        <pre className="max-h-[60rem] min-w-0 overflow-auto whitespace-pre-wrap break-words border bg-[var(--panel-code-bg)] p-6 font-mono text-sm/7 text-[var(--panel-code-foreground)]">
          {safeBody}
        </pre>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <MessageToolbar
        label={messageFormatLabel(resolvedType, wasInferred)}
        body={body}
        action={
          <Button variant="outline" size="sm" type="button" onClick={() => setShowSource(true)}>
            <Code2Icon data-icon="inline-start" />
            Raw
          </Button>
        }
      />
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
    <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
      <Badge variant="secondary">{label}</Badge>
      <div className="flex flex-wrap items-center gap-3">
        <CopyButton value={body} label="Copy message" />
        {action}
      </div>
    </div>
  );
}
