"use client";

import { useMemo, useState } from "react";
import { CopyButton } from "./copy-button";
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
      <div className="message-content">
        <div className="message-toolbar">
          <span className="format-badge">{messageFormatLabel(resolvedType, wasInferred)}</span>
          <div className="message-actions">
            <CopyButton value={body} label="Copy message" />
            {resolvedType === "text/markdown" && (
              <button className="mini-button" type="button" onClick={() => setShowSource(false)}>
                Rendered
              </button>
            )}
          </div>
        </div>
        <pre className="message-body">{safeBody}</pre>
      </div>
    );
  }

  return (
    <div className="message-content">
      <div className="message-toolbar">
        <span className="format-badge">{messageFormatLabel(resolvedType, wasInferred)}</span>
        <div className="message-actions">
          <CopyButton value={body} label="Copy message" />
          <button className="mini-button" type="button" onClick={() => setShowSource(true)}>
            Raw
          </button>
        </div>
      </div>
      <MarkdownMessage body={body} />
    </div>
  );
}
