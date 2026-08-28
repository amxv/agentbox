"use client";

import {
  ChevronDownIcon,
  DownloadIcon,
  EyeIcon,
  FileTextIcon,
  MessageSquareIcon,
  PlusIcon,
  ShieldAlertIcon
} from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle
} from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger
} from "@/components/ui/collapsible";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle
} from "@/components/ui/empty";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";
import { CopyButton } from "../../components/copy-button";
import { MessageContent } from "./message-content";
import { MessageComposer } from "../../components/message-composer";
import { postDashboardMessage } from "../../components/agentbox-write";
import { fetchSession } from "../../components/session";
import { ThreadVisibilityControl } from "./thread-visibility-control";
import { attributionLabel } from "../../components/attribution";
import {
  MetricStrip,
  MonoValue,
  PanelHeader,
  PanelMain,
} from "../../components/panel-shell";

type Asset = {
  id: string;
  file_name: string;
  mime_type: string | null;
  size_bytes: number;
  download_path?: string | null;
  preview_path?: string | null;
  purged_at?: string | null;
  unavailable?: boolean;
  unavailable_reason?: string;
};

type AssetResolution = {
  available: boolean;
  download_url?: string;
  preview_url?: string;
  preview_failed?: boolean;
  unavailable_reason?: string;
};

type Message = {
  id: string;
  author: string;
  body: string;
  body_content_type?: string | null;
  created_at: string;
  assets: Asset[];
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
};

type Thread = {
  id: string;
  title: string;
  updated_at: string;
  created_by: string;
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
  messages: Message[];
};

function formatDate(value: string) {
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** index;
  return `${value.toFixed(value >= 10 || index === 0 ? 0 : 1)} ${units[index]}`;
}

function getMessagePreview(body: string) {
  const normalized = body.replace(/\s+/g, " ").trim();
  if (!normalized) return "Empty message";
  return normalized.length > 150 ? `${normalized.slice(0, 150)}…` : normalized;
}

function getMessageKind(contentType?: string | null) {
  if (!contentType) return "Auto";
  if (contentType.includes("markdown")) return "Markdown";
  if (contentType.includes("plain")) return "Plain text";
  return contentType;
}

function isPreviewableImage(asset: Asset) {
  return Boolean(asset.preview_path && asset.mime_type?.toLowerCase().startsWith("image/") && !asset.purged_at);
}

function isMarkdownAsset(asset: Asset) {
  const mimeType = asset.mime_type?.toLowerCase().trim() ?? "";
  if (mimeType.startsWith("text/markdown")) return true;
  return /\.(md|markdown|mdown|mkd)$/i.test(asset.file_name);
}

export function ThreadView({ threadId }: { threadId: string }) {
  const router = useRouter();
  const [thread, setThread] = useState<Thread | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showReplyComposer, setShowReplyComposer] = useState(false);
  const [expandedMessages, setExpandedMessages] = useState<Set<string>>(() => new Set());
  const [assetResolutions, setAssetResolutions] = useState<Record<string, AssetResolution>>({});
  const [assetBusy, setAssetBusy] = useState<string | null>(null);
  const [downloadAllBusy, setDownloadAllBusy] = useState<string | null>(null);
  const [markdownPreviewBodies, setMarkdownPreviewBodies] = useState<Record<string, string>>({});
  const [markdownPreviewErrors, setMarkdownPreviewErrors] = useState<Record<string, string>>({});
  const [expandedMarkdownPreviews, setExpandedMarkdownPreviews] = useState<Set<string>>(() => new Set());

  const loadThread = useCallback(async function loadThread() {
    setLoading(true);
    setError(null);
    try {
      const session = await fetchSession();
      if (!session) {
        router.replace(`/login?next=/threads/${encodeURIComponent(threadId)}`);
        return;
      }
      const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/view`, { cache: "no-store" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      const nextThread = data.thread as Thread;
      setThread(nextThread);
      setExpandedMessages((current) => {
        const next = new Set<string>();
        for (const message of nextThread.messages) {
          if (current.has(message.id)) next.add(message.id);
        }
        if (nextThread.messages.length === 1) next.add(nextThread.messages[0].id);
        return next;
      });
      setAssetResolutions({});
      setMarkdownPreviewBodies({});
      setMarkdownPreviewErrors({});
      setExpandedMarkdownPreviews(new Set());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [router, threadId]);

  useEffect(() => {
    const timeout = window.setTimeout(() => { void loadThread(); }, 0);
    return () => window.clearTimeout(timeout);
  }, [loadThread]);

  useEffect(() => {
    const assets = thread?.messages.flatMap((message) => message.assets).filter(isPreviewableImage) ?? [];
    if (assets.length === 0) return;

    const controller = new AbortController();
    for (const asset of assets) {
      if (!asset.preview_path) continue;
      void fetch(asset.preview_path, { cache: "no-store", signal: controller.signal })
        .then(async (response) => {
          const data = await response.json().catch(() => ({}));
          if (!response.ok) {
            setAssetResolutions((current) => ({
              ...current,
              [asset.id]: { ...current[asset.id], available: true, preview_failed: true }
            }));
            return;
          }
          if (data.available === false) {
            setAssetResolutions((current) => ({
              ...current,
              [asset.id]: {
                available: false,
                unavailable_reason: data.unavailable_reason || "Attachment unavailable"
              }
            }));
            return;
          }
          if (typeof data.preview_url !== "string" || data.preview_url === "") return;
          setAssetResolutions((current) => ({
            ...current,
            [asset.id]: { ...current[asset.id], available: true, preview_url: data.preview_url }
          }));
        })
        .catch((err) => {
          if (err instanceof DOMException && err.name === "AbortError") return;
          setAssetResolutions((current) => ({
            ...current,
            [asset.id]: { ...current[asset.id], available: true, preview_failed: true }
          }));
        });
    }

    return () => controller.abort();
  }, [thread]);

  async function postReply(body: string, files: File[]) {
    await postDashboardMessage(threadId, body, files);
    await loadThread();
    setShowReplyComposer(false);
  }

  const assetCount = useMemo(
    () => thread?.messages.reduce((total, message) => total + message.assets.length, 0) ?? 0,
    [thread]
  );

  function toggleMessage(messageId: string) {
    setExpandedMessages((current) => {
      const next = new Set(current);
      if (next.has(messageId)) next.delete(messageId);
      else next.add(messageId);
      return next;
    });
  }

  async function getSignedAssetURL(asset: Asset, kind: "download" | "preview") {
    const path = kind === "preview" ? asset.preview_path : asset.download_path;
    if (!path || asset.purged_at) throw new Error("Attachment unavailable");
    const response = await fetch(path, { cache: "no-store" });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
    if (data.available === false) {
      const unavailableReason = data.unavailable_reason || "Attachment unavailable";
      setAssetResolutions((current) => ({
        ...current,
        [asset.id]: { available: false, unavailable_reason: unavailableReason }
      }));
      throw new Error(unavailableReason);
    }
    const field = kind === "preview" ? "preview_url" : "download_url";
    const signedURL = data[field];
    if (typeof signedURL !== "string" || signedURL === "") throw new Error(`The attachment ${kind} URL was not returned.`);
    setAssetResolutions((current) => ({
      ...current,
      [asset.id]: { ...current[asset.id], available: true, [field]: signedURL }
    }));
    return signedURL;
  }

  async function resolveAsset(asset: Asset, kind: "download" | "preview") {
    const busyKey = `${kind}:${asset.id}`;
    setAssetBusy(busyKey);
    setError(null);
    try {
      const signedURL = await getSignedAssetURL(asset, kind);
      if (kind === "download") window.location.assign(signedURL);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setAssetBusy((current) => current === busyKey ? null : current);
    }
  }

  async function toggleMarkdownPreview(asset: Asset) {
    if (expandedMarkdownPreviews.has(asset.id)) {
      setExpandedMarkdownPreviews((current) => {
        const next = new Set(current);
        next.delete(asset.id);
        return next;
      });
      return;
    }

    if (markdownPreviewBodies[asset.id] !== undefined) {
      setExpandedMarkdownPreviews((current) => new Set(current).add(asset.id));
      return;
    }

    const busyKey = `preview:${asset.id}`;
    setAssetBusy(busyKey);
    setMarkdownPreviewErrors((current) => {
      const next = { ...current };
      delete next[asset.id];
      return next;
    });
    try {
      const signedURL = await getSignedAssetURL(asset, "preview");
      const response = await fetch(signedURL, { cache: "no-store" });
      if (!response.ok) throw new Error(`Preview download failed with HTTP ${response.status}`);
      const body = await response.text();
      setMarkdownPreviewBodies((current) => ({ ...current, [asset.id]: body }));
      setExpandedMarkdownPreviews((current) => new Set(current).add(asset.id));
    } catch (err) {
      setMarkdownPreviewErrors((current) => ({
        ...current,
        [asset.id]: err instanceof Error ? err.message : String(err)
      }));
    } finally {
      setAssetBusy((current) => current === busyKey ? null : current);
    }
  }

  async function downloadAllAttachments(message: Message) {
    const assets = message.assets.filter((asset) => asset.download_path && !asset.purged_at && assetResolutions[asset.id]?.available !== false);
    if (assets.length === 0) return;
    setDownloadAllBusy(message.id);
    setError(null);
    try {
      const results = await Promise.allSettled(assets.map(async (asset) => ({ asset, url: await getSignedAssetURL(asset, "download") })));
      let failed = 0;
      for (const result of results) {
        if (result.status === "rejected") {
          failed += 1;
          continue;
        }
        const link = document.createElement("a");
        link.href = result.value.url;
        link.download = result.value.asset.file_name;
        link.style.display = "none";
        document.body.appendChild(link);
        link.click();
        link.remove();
      }
      if (failed > 0) setError(`${failed} attachment${failed === 1 ? "" : "s"} could not be downloaded.`);
    } finally {
      setDownloadAllBusy((current) => current === message.id ? null : current);
    }
  }

  return (
      <PanelMain width="reading">
        <PanelHeader
          title={thread?.title ?? "Thread"}
          description={
            <span className="flex flex-col gap-3">
              <span className="flex flex-wrap items-center gap-3">
                <MonoValue>{thread?.id ?? threadId}</MonoValue>
                <CopyButton value={thread?.id ?? threadId} label="Copy thread ID" />
              </span>
              {thread ? (
                <span>Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)} · Updated {formatDate(thread.updated_at)}</span>
              ) : null}
            </span>
          }
          actions={
            <>
              <Button type="button" onClick={() => setShowReplyComposer((value) => !value)}>
                <PlusIcon data-icon="inline-start" />
                {showReplyComposer ? "Close reply" : "Reply"}
              </Button>
              {thread ? <ThreadVisibilityControl threadId={thread.id} /> : null}
            </>
          }
          aside={thread ? (
            <MetricStrip
              items={[
                { label: "Messages", value: thread.messages.length },
                { label: "Attachments", value: assetCount }
              ]}
            />
          ) : null}
        />

        {showReplyComposer ? (
          <MessageComposer
            label="Reply"
            placeholder="Post a message. Markdown is detected automatically."
            submitLabel="Post message"
            onSubmit={postReply}
          />
        ) : null}

        {error ? (
          <Alert variant="destructive">
            <ShieldAlertIcon />
            <AlertTitle>Could not load thread</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <section className="flex flex-col gap-5" aria-label="Thread messages">
          {loading ? <MessageSkeleton /> : null}
          {!loading && !error && thread?.messages.length === 0 ? (
            <Empty className="border py-16">
              <EmptyHeader>
                <EmptyMedia variant="icon"><MessageSquareIcon /></EmptyMedia>
                <EmptyTitle>No messages yet</EmptyTitle>
                <EmptyDescription>Post the first reply to begin the thread.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : null}
          {!loading && !error ? thread?.messages.map((message, index) => {
            const isExpanded = expandedMessages.has(message.id);
            return (
              <Collapsible open={isExpanded} onOpenChange={() => toggleMessage(message.id)} key={message.id}>
                <Card>
                  <CollapsibleTrigger
                    render={
                      <Button
                        type="button"
                        variant="ghost"
                        className="panel-message-trigger h-auto w-full items-start justify-between gap-5 rounded-none p-5 text-left whitespace-normal"
                      />
                    }
                  >
                    <span className="flex min-w-0 items-start gap-4">
                      <Badge variant="secondary">#{index + 1}</Badge>
                      <span className="flex min-w-0 flex-col gap-3">
                        <span className="flex flex-wrap items-center gap-3">
                          <strong className="font-heading text-base font-semibold">{attributionLabel(message.created_by_user_display_name, message.created_by_actor_name, message.author)}</strong>
                          <Badge variant="outline">{getMessageKind(message.body_content_type)}</Badge>
                          {message.assets.length > 0 ? <Badge variant="outline">{message.assets.length} attachment{message.assets.length === 1 ? "" : "s"}</Badge> : null}
                        </span>
                        <span className="panel-message-preview line-clamp-2 text-sm/relaxed">{getMessagePreview(message.body)}</span>
                        <span className="flex flex-wrap items-center gap-3">
                          <MonoValue>{message.id}</MonoValue>
                          <span onClick={(event) => event.stopPropagation()}><CopyButton value={message.id} label="Copy message ID" /></span>
                        </span>
                      </span>
                    </span>
                    <span className="panel-message-meta flex shrink-0 items-center gap-3 text-sm">
                      <time dateTime={message.created_at}>{formatDate(message.created_at)}</time>
                      <ChevronDownIcon className={cn("transition-transform", isExpanded && "rotate-180")} />
                    </span>
                  </CollapsibleTrigger>
                  <CollapsibleContent>
                    <Separator />
                    <CardContent className="flex flex-col gap-8 pt-6">
                      <MessageContent body={message.body} contentType={message.body_content_type} />
                      {message.assets.length > 0 ? (
                        <section className="flex flex-col gap-4" aria-label="Attachments">
                          <div className="flex flex-wrap items-center justify-between gap-3">
                            <span className="font-mono text-xs tracking-[0.1em] text-muted-foreground uppercase">Attachments</span>
                            {message.assets.length > 1 && message.assets.some((asset) => asset.download_path && !asset.purged_at) ? (
                              <Button variant="outline" size="sm" disabled={downloadAllBusy === message.id} onClick={() => void downloadAllAttachments(message)}>
                                {downloadAllBusy === message.id ? <Spinner data-icon="inline-start" /> : <DownloadIcon data-icon="inline-start" />}
                                Download all attachments
                              </Button>
                            ) : null}
                          </div>
                          <div className="grid gap-4">
                            {message.assets.map((asset) => {
                              const resolution = assetResolutions[asset.id];
                              const unavailable = asset.unavailable || resolution?.available === false;
                              const unavailableReason = resolution?.unavailable_reason || asset.unavailable_reason || "Attachment unavailable";
                              const previewBusy = assetBusy === `preview:${asset.id}`;
                              const downloadBusy = assetBusy === `download:${asset.id}`;
                              const markdownPreviewOpen = expandedMarkdownPreviews.has(asset.id);
                              const markdownPreviewBody = markdownPreviewBodies[asset.id];
                              const markdownPreviewError = markdownPreviewErrors[asset.id];
                              return (
                                <Card size="sm" key={asset.id}>
                                  {!asset.purged_at && !unavailable && isPreviewableImage(asset) && resolution?.preview_url ? (
                                    // eslint-disable-next-line @next/next/no-img-element
                                    <img className="max-h-[32rem] w-full border-b bg-muted object-contain" src={resolution.preview_url} alt={asset.file_name} loading="lazy" />
                                  ) : null}
                                  {isPreviewableImage(asset) && !resolution?.preview_url && !resolution?.preview_failed && !unavailable ? (
                                    <Skeleton className="h-64 w-full rounded-none border-b" />
                                  ) : null}
                                  <CardHeader>
                                    <div className="flex min-w-0 items-start gap-4">
                                      <span className="flex size-10 shrink-0 items-center justify-center border bg-muted"><FileTextIcon /></span>
                                      <div className="flex min-w-0 flex-col gap-2">
                                        <CardTitle>{asset.file_name}</CardTitle>
                                        <CardDescription>{asset.mime_type ?? "Unknown type"} · {formatBytes(asset.size_bytes)}</CardDescription>
                                      </div>
                                    </div>
                                  </CardHeader>
                                  <CardContent className="flex flex-col gap-4">
                                    {markdownPreviewOpen && markdownPreviewBody !== undefined ? (
                                      <div className="border-t pt-4">
                                        <MessageContent body={markdownPreviewBody} contentType="text/markdown" />
                                      </div>
                                    ) : null}
                                    {markdownPreviewError ? (
                                      <Alert variant="destructive"><AlertTitle>Could not preview Markdown attachment</AlertTitle><AlertDescription>{markdownPreviewError}</AlertDescription></Alert>
                                    ) : null}
                                    {asset.purged_at ? (
                                      <Alert variant="destructive"><AlertTitle>Attachment deleted by deployment owner</AlertTitle></Alert>
                                    ) : unavailable ? (
                                      <Alert variant="destructive"><AlertTitle>Attachment unavailable</AlertTitle><AlertDescription>{unavailableReason}</AlertDescription></Alert>
                                    ) : (
                                      <div className="flex flex-wrap gap-3">
                                        {asset.preview_path && isMarkdownAsset(asset) ? (
                                          <Button variant="outline" disabled={previewBusy} onClick={() => void toggleMarkdownPreview(asset)}>
                                            {previewBusy ? <Spinner data-icon="inline-start" /> : <EyeIcon data-icon="inline-start" />}
                                            {markdownPreviewOpen ? "Hide preview" : markdownPreviewError ? "Retry preview" : "Preview Markdown"}
                                          </Button>
                                        ) : asset.preview_path && !resolution?.preview_url && (!isPreviewableImage(asset) || resolution?.preview_failed) ? (
                                          <Button variant="outline" disabled={previewBusy} onClick={() => void resolveAsset(asset, "preview")}>
                                            {previewBusy ? <Spinner data-icon="inline-start" /> : <EyeIcon data-icon="inline-start" />}
                                            {resolution?.preview_failed ? "Retry preview" : "Preview"}
                                          </Button>
                                        ) : null}
                                        {asset.download_path ? (
                                          <Button variant="outline" disabled={downloadBusy} onClick={() => void resolveAsset(asset, "download")}>
                                            {downloadBusy ? <Spinner data-icon="inline-start" /> : <DownloadIcon data-icon="inline-start" />}
                                            Download attachment
                                          </Button>
                                        ) : null}
                                      </div>
                                    )}
                                  </CardContent>
                                </Card>
                              );
                            })}
                          </div>
                        </section>
                      ) : null}
                    </CardContent>
                  </CollapsibleContent>
                </Card>
              </Collapsible>
            );
          }) : null}
        </section>
      </PanelMain>
  );
}

function MessageSkeleton() {
  return (
    <div className="flex flex-col gap-5" aria-label="Loading thread" aria-busy="true">
      {Array.from({ length: 3 }).map((_, index) => (
        <Card key={index}>
          <CardContent className="flex items-start justify-between gap-6">
            <div className="flex flex-1 items-start gap-4">
              <Skeleton className="h-5 w-8" />
              <div className="flex flex-1 flex-col gap-3">
                <Skeleton className="h-4 w-40" />
                <Skeleton className="h-3 w-full" />
                <Skeleton className="h-3 w-2/3" />
              </div>
            </div>
            <Skeleton className="h-4 w-28" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
