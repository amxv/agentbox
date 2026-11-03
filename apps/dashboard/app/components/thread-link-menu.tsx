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
import { experimentalIosPwaLaunchUrl, threadWebUrl } from "@/lib/thread-links";

export function ThreadLinkMenu({ threadId }: { threadId: string }) {
  const [copied, setCopied] = useState<"web" | "pwa" | null>(null);

  async function copyLink(kind: "web" | "pwa") {
    try {
      const link = kind === "web"
        ? threadWebUrl(window.location.origin, threadId)
        : experimentalIosPwaLaunchUrl(window.location.origin);
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
          {copied === "web" ? "Copied thread link" : "Copy thread link (HTTPS)"}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => void copyLink("pwa")}>
          {copied === "pwa" ? <CheckIcon /> : <SmartphoneIcon />}
          {copied === "pwa" ? "Copied experimental launcher" : "Copy iOS app launcher (experimental)"}
        </DropdownMenuItem>
        <p className="px-3 pb-2 pt-1 text-xs leading-relaxed text-muted-foreground">
          HTTPS thread links can open the installed app on supported Android setups. iOS opens them in a browser.
          The undocumented iOS launcher may open the app at its inbox, not this thread, or fail.
        </p>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
