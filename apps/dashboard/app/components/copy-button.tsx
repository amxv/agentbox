"use client";

import { CheckIcon, CopyIcon } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";

export function CopyButton({
  value,
  label = "Copy",
  size = "icon-sm"
}: {
  value: string;
  label?: string;
  size?: "icon-xs" | "icon-sm" | "icon";
}) {
  const [copied, setCopied] = useState(false);

  async function handleClick() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      setCopied(false);
    }
  }

  return (
    <Button
      aria-label={copied ? "Copied" : label}
      title={copied ? "Copied" : label}
      type="button"
      variant="ghost"
      size={size}
      onClick={handleClick}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </Button>
  );
}
