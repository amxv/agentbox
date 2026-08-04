"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { attributionLabel } from "../../../components/attribution";
import { fetchSession } from "../../../components/session";
import { MessageContent } from "../../../threads/[threadId]/message-content";
import styles from "./owner-content-thread.module.css";

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

  return <main className={styles.page}>
    <header className={styles.topbar}><Link href="/owner/content">← Deployment content</Link><span>OWNER VIEW · READ ONLY</span><Link href="/threads">Normal inbox</Link></header>
    <section className={styles.shell}>
      <div className={styles.warning}><strong>READ ONLY</strong><span>This thread may be private to another user. No reply, upload, or visibility action is available from this owner-only surface.</span></div>
      {loading && <div className={styles.empty}>Loading owner content…</div>}
      {error && <div className={styles.error}>{error}</div>}
      {thread && <>
        <section className={styles.hero}>
          <p>Owned by {thread.owner.display_name}{thread.owner.disabled_at ? " · disabled" : ""}</p>
          <h1>{thread.title}</h1>
          <div><span>{thread.owner.email}</span><span>Updated {formatDate(thread.updated_at)}</span><code>{thread.id}</code></div>
          <div className={styles.badges}>
            {thread.visibility_summary.private && <em>Private</em>}
            {thread.visibility.shared_teams.map((team) => <em key={team.id}>{team.name}</em>)}
            {thread.visibility_summary.public && <em>Public</em>}
          </div>
          <span className={styles.creator}>Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)}</span>
        </section>
        <section className={styles.messages}>
          {thread.messages.length === 0 && <div className={styles.empty}>No messages.</div>}
          {thread.messages.map((message, index) => <article className={styles.message} key={message.id}>
            <header><div><strong>{attributionLabel(message.created_by_user_display_name, message.created_by_actor_name, message.author)}</strong><span>Message {index + 1}</span></div><time dateTime={message.created_at}>{formatDate(message.created_at)}</time></header>
            <MessageContent body={message.body} contentType={message.body_content_type} />
            {message.assets.length > 0 && <div className={styles.assets}>{message.assets.map((asset) => {
              const resolution = assetResolutions[asset.id];
              const unavailable = asset.unavailable || resolution?.available === false;
              return <div className={styles.asset} key={asset.id}>
                {!asset.purged_at && !unavailable && resolution?.preview_url && <>
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img src={resolution.preview_url} alt={asset.file_name} loading="lazy" />
                </>}
                <div><strong>{asset.file_name}</strong><span>{asset.mime_type || "File"} · {formatBytes(asset.size_bytes)}</span></div>
                {asset.purged_at ? <em>Attachment deleted by deployment owner</em> : unavailable ? <em>{resolution?.unavailable_reason || asset.unavailable_reason || "Attachment unavailable"}</em> : <>
                  {asset.preview_path && !resolution?.preview_url && <button type="button" disabled={assetBusy === `preview:${asset.id}`} onClick={() => void resolveAsset(asset, "preview")}>{assetBusy === `preview:${asset.id}` ? "Loading preview…" : "Load preview"}</button>}
                  {asset.download_path && <button type="button" disabled={assetBusy === `download:${asset.id}`} onClick={() => void resolveAsset(asset, "download")}>{assetBusy === `download:${asset.id}` ? "Signing…" : "Open attachment"}</button>}
                </>}
              </div>;
            })}</div>}
          </article>)}
        </section>
      </>}
    </section>
  </main>;
}
