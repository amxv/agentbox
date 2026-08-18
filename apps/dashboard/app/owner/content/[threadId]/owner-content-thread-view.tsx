"use client";

import { ArrowLeftIcon, DownloadIcon, EyeIcon, FileWarningIcon, ShieldAlertIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { attributionLabel } from "../../../components/attribution";
import { MetricStrip, MonoValue, PanelHeader, PanelMain } from "../../../components/panel-shell";
import { fetchSession } from "../../../components/session";
import { MessageContent } from "../../../threads/[threadId]/message-content";

type Asset = {
  id: string;
  file_name: string;
  mime_type?: string;
  size_bytes: number;
  download_path?: string;
  preview_path?: string;
  purged_at?: string;
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
  body_content_type?: string;
  created_at: string;
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
  assets: Asset[];
};

type Team = { id: string; slug: string; name: string };

type ThreadDetail = {
  id: string;
  title: string;
  created_by: string;
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
  created_at: string;
  updated_at: string;
  owner: { id: string; email: string; display_name: string; disabled_at?: string };
  visibility: { shared_teams: Team[] };
  visibility_summary: { private: boolean; public: boolean; shared_teams: Team[] };
  messages: Message[];
};

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function isPreviewableImage(asset: Asset) {
  return Boolean(asset.preview_path && asset.mime_type?.toLowerCase().startsWith("image/") && !asset.purged_at);
}

export function OwnerContentThreadView({ threadId }: { threadId: string }) {
  const router = useRouter();
  const [thread, setThread] = useState<ThreadDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [assetResolutions, setAssetResolutions] = useState<Record<string, AssetResolution>>({});
  const [assetBusy, setAssetBusy] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const session = await fetchSession();
      if (!session?.is_owner || session.subject_type !== "user_session") {
        router.replace(`/login?next=${encodeURIComponent(`/owner/content/${threadId}`)}`);
        return;
      }
      const response = await fetch(`/api/owner/content/threads/${encodeURIComponent(threadId)}`, { cache: "no-store" });
      if (response.status === 401 || response.status === 403) {
        router.replace(`/login?next=${encodeURIComponent(`/owner/content/${threadId}`)}`);
        return;
      }
      if (response.status === 404) {
        router.replace("/owner/content");
        return;
      }
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setThread(data.thread);
      setAssetResolutions({});
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [router, threadId]);

  useEffect(() => {
    const timer = window.setTimeout(() => { void load(); }, 0);
    return () => window.clearTimeout(timer);
  }, [load]);

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

  async function resolveAsset(asset: Asset, kind: "download" | "preview") {
    const path = kind === "preview" ? asset.preview_path : asset.download_path;
    if (!path || asset.purged_at) return;
    const busyKey = `${kind}:${asset.id}`;
    setAssetBusy(busyKey);
    setError(null);
    try {
      const response = await fetch(path, { cache: "no-store" });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      if (data.available === false) {
        setAssetResolutions((current) => ({
          ...current,
          [asset.id]: { available: false, unavailable_reason: data.unavailable_reason || "Attachment unavailable" }
        }));
        return;
      }
      const field = kind === "preview" ? "preview_url" : "download_url";
      const signedURL = data[field];
      if (typeof signedURL !== "string" || signedURL === "") throw new Error(`The attachment ${kind} URL was not returned.`);
      setAssetResolutions((current) => ({
        ...current,
        [asset.id]: { ...current[asset.id], available: true, [field]: signedURL }
      }));
      if (kind === "download") window.location.assign(signedURL);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setAssetBusy((current) => current === busyKey ? null : current);
    }
  }

  return (
      <PanelMain width="reading">
        <div>
          <Button className="-ml-2" variant="ghost" size="sm" render={<Link href="/owner/content" />}>
            <ArrowLeftIcon data-icon="inline-start" />
            Deployment content
          </Button>
        </div>

        <Alert>
          <ShieldAlertIcon />
          <AlertTitle>Read-only owner inspection</AlertTitle>
          <AlertDescription>
            This thread may be private to another user. Reply, upload, and visibility controls are intentionally unavailable here.
          </AlertDescription>
        </Alert>

        {loading ? <OwnerThreadSkeleton /> : null}
        {error ? (
          <Alert variant="destructive">
            <AlertTitle>Could not load owner content</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        {thread ? (
          <>
            <PanelHeader
              title={thread.title}
              description={
                <span className="flex flex-col gap-2">
                  <span>Owned by {thread.owner.display_name}{thread.owner.disabled_at ? " · disabled" : ""} · {thread.owner.email} · Updated {formatDate(thread.updated_at)}</span>
                  <MonoValue>{thread.id}</MonoValue>
                  <span>Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)}</span>
                </span>
              }
              actions={
                <>
                  {thread.visibility_summary.private ? <Badge variant="outline">Private</Badge> : null}
                  {thread.visibility.shared_teams.map((team) => <Badge variant="outline" key={team.id}>{team.name}</Badge>)}
                  {thread.visibility_summary.public ? <Badge>Public</Badge> : null}
                </>
              }
              aside={
                <MetricStrip
                  items={[
                    { label: "Messages", value: thread.messages.length },
                    { label: "Created", value: formatDate(thread.created_at) }
                  ]}
                />
              }
            />

            <section className="grid gap-4" aria-label="Thread messages">
              {thread.messages.length === 0 ? (
                <Empty className="border py-16">
                  <EmptyHeader>
                    <EmptyMedia variant="icon"><FileWarningIcon /></EmptyMedia>
                    <EmptyTitle>No messages</EmptyTitle>
                    <EmptyDescription>This thread exists but has no message content.</EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : null}

              {thread.messages.map((message, index) => (
                <Card key={message.id}>
                  <CardHeader className="border-b">
                    <div className="flex min-w-0 flex-col gap-1">
                      <span className="font-mono text-[0.65rem] tracking-[0.12em] text-muted-foreground uppercase">Message {index + 1}</span>
                      <h2 className="font-heading text-sm font-semibold">
                        {attributionLabel(message.created_by_user_display_name, message.created_by_actor_name, message.author)}
                      </h2>
                      <MonoValue>{message.id}</MonoValue>
                    </div>
                    <CardAction>
                      <time className="text-xs text-muted-foreground" dateTime={message.created_at}>
                        {formatDate(message.created_at)}
                      </time>
                    </CardAction>
                  </CardHeader>
                  <CardContent className="grid gap-5">
                    <MessageContent body={message.body} contentType={message.body_content_type} />
                    {message.assets.length > 0 ? (
                      <div className="grid gap-4">
                        <Separator />
                        <span className="font-mono text-[0.65rem] tracking-[0.12em] text-muted-foreground uppercase">Attachments</span>
                        {message.assets.map((asset) => {
                          const resolution = assetResolutions[asset.id];
                          const unavailable = asset.unavailable || resolution?.available === false;
                          return (
                            <div className="grid gap-3 border p-3" key={asset.id}>
                              {!asset.purged_at && !unavailable && resolution?.preview_url ? (
                                // eslint-disable-next-line @next/next/no-img-element
                                <img className="max-h-[32rem] w-full object-contain" src={resolution.preview_url} alt={asset.file_name} loading="lazy" />
                              ) : null}
                              {isPreviewableImage(asset) && !resolution?.preview_url && !resolution?.preview_failed && !unavailable ? (
                                <Skeleton className="h-64 w-full rounded-none" />
                              ) : null}
                              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                                <span className="min-w-0">
                                  <strong className="block truncate text-xs">{asset.file_name}</strong>
                                  <span className="text-xs text-muted-foreground">{asset.mime_type || "File"} · {formatBytes(asset.size_bytes)}</span>
                                </span>
                                {asset.purged_at ? (
                                  <Badge variant="destructive">Deleted by owner</Badge>
                                ) : unavailable ? (
                                  <Badge variant="destructive">{resolution?.unavailable_reason || asset.unavailable_reason || "Attachment unavailable"}</Badge>
                                ) : (
                                  <span className="flex shrink-0 flex-wrap gap-2">
                                    {asset.preview_path && !resolution?.preview_url && (!isPreviewableImage(asset) || resolution?.preview_failed) ? (
                                      <Button variant="outline" type="button" disabled={assetBusy === `preview:${asset.id}`} onClick={() => void resolveAsset(asset, "preview")}>
                                        <EyeIcon data-icon="inline-start" />
                                        {assetBusy === `preview:${asset.id}` ? "Loading" : resolution?.preview_failed ? "Retry preview" : "Preview"}
                                      </Button>
                                    ) : null}
                                    {asset.download_path ? (
                                      <Button variant="outline" type="button" disabled={assetBusy === `download:${asset.id}`} onClick={() => void resolveAsset(asset, "download")}>
                                        <DownloadIcon data-icon="inline-start" />
                                        {assetBusy === `download:${asset.id}` ? "Signing" : "Open"}
                                      </Button>
                                    ) : null}
                                  </span>
                                )}
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    ) : null}
                  </CardContent>
                </Card>
              ))}
            </section>
          </>
        ) : null}
      </PanelMain>
  );
}

function OwnerThreadSkeleton() {
  return (
    <div className="grid gap-6" aria-label="Loading owner content" aria-busy="true">
      <div className="grid gap-3 border-b pb-8">
        <Skeleton className="h-3 w-40" />
        <Skeleton className="h-10 w-3/4" />
        <Skeleton className="h-3 w-1/2" />
      </div>
      {Array.from({ length: 2 }).map((_, index) => (
        <Card key={index}>
          <CardHeader className="border-b"><Skeleton className="h-5 w-48" /></CardHeader>
          <CardContent className="grid gap-3">
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-3 w-5/6" />
            <Skeleton className="h-3 w-2/3" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
