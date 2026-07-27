"use client";

import { useState } from "react";

type PublicCopyButtonProps = {
  value?: string;
  sourceUrl?: string;
  label?: string;
  copiedLabel?: string;
  className?: string;
};

export function PublicCopyButton({
  value,
  sourceUrl,
  label = "Copy",
  copiedLabel = "Copied",
  className = ""
}: PublicCopyButtonProps) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      let text = value ?? "";
      if (!text && sourceUrl) {
        const response = await fetch(sourceUrl);
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        text = await response.text();
      }
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  }

  return (
    <button className={className} type="button" onClick={() => void copy()}>
      {copied ? copiedLabel : label}
    </button>
  );
}
