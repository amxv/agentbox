"use client";

import { CheckIcon, ChevronDownIcon, GlobeIcon, LinkIcon, SmartphoneIcon } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { experimentalIosPwaThreadUrl, threadWebUrl } from "@/lib/thread-links";

export function ThreadLinkMenu({ threadId }: { threadId: string }) {
  const [copied, setCopied] = useState<"web" | "pwa" | null>(null);

  async function copyLink(kind: "web" | "pwa") {
    try {
      const link = kind === "web"
        ? threadWebUrl(window.location.origin, threadId)
        : experimentalIosPwaThreadUrl(window.location.origin, threadId);
      await navigator.clipboard.writeText(link);
      setCopied(kind);
      window.setTimeout(() => setCopied(null), 1600);
    } catch {
      setCopied(null);
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button type="button" variant="outline" size="sm" className="gap-1.5" />}>
        <LinkIcon data-icon="inline-start" />
        Copy link
        <ChevronDownIcon data-icon="inline-end" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuItem onClick={() => void copyLink("web")}>
          {copied === "web" ? <CheckIcon /> : <GlobeIcon />}
          {copied === "web" ? "Copied web thread link" : "Copy web thread link"}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => void copyLink("pwa")}>
          {copied === "pwa" ? <CheckIcon /> : <SmartphoneIcon />}
          {copied === "pwa" ? "Copied experimental iOS link" : "Copy iOS PWA link (experimental)"}
        </DropdownMenuItem>
        <p className="px-3 pb-2 pt-1 text-xs leading-relaxed text-muted-foreground">
          iOS may launch the installed PWA but ignore the thread path, or refuse the link. Use the web link for reliable access.
        </p>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
