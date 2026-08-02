"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { AgentboxMark } from "../../components/agentbox-mark";
import { ThemeSwitcher } from "../../components/theme-switcher";
import { MessageContent } from "../../threads/[threadId]/message-content";
import { attributionLabel } from "../../components/attribution";
import styles from "./public-thread.module.css";

type PublicAsset = {
  id: string;
  file_name: string;
  mime_type?: string;
  size_bytes: number;
  created_at: string;
  created_by: string;
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
  download_path: string;
};

type PublicMessage = {
  id: string;
  author: string;
  body: string;
  body_content_type?: string;
  created_at: string;
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
  assets: PublicAsset[];
};

type PublicThread = {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
  messages: PublicMessage[];
};

async function responseJSON(response: Response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data;
}

function formatDate(value: string) {
  return new Date(value).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

export function PublicThreadView({ token }: { token: string }) {
  const [thread, setThread] = useState<PublicThread | null>(null);
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [downloadBusy, setDownloadBusy] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(`/api/public/threads/${encodeURIComponent(token)}`, { cache: "no-store" });
      if (response.status === 404) {
        setUnavailable(true);
        setThread(null);
        return;
      }
      const data = await responseJSON(response);
      setThread(data.thread);
      setUnavailable(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    const timer = window.setTimeout(() => { void load(); }, 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  async function download(asset: PublicAsset) {
    setDownloadBusy(asset.id);
    setError(null);
    try {
      const response = await fetch(asset.download_path, { cache: "no-store" });
      if (response.status === 404) {
        setUnavailable(true);
        setThread(null);
        return;
      }
      const data = await responseJSON(response);
      if (typeof data.download_url !== "string" || data.download_url === "") {
        throw new Error("The attachment download URL was not returned.");
      }
      window.location.assign(data.download_url);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDownloadBusy(null);
    }
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Link className={styles.brand} href="/"><AgentboxMark className={styles.mark}/><span>Agentbox</span><small>Shared thread</small></Link>
        <div className={styles.headerMeta}><span>Read only</span><ThemeSwitcher/></div>
      </header>

      <main className={styles.main}>
        {loading && <LoadingState/>}
        {!loading && unavailable && <UnavailableState/>}
        {!loading && !unavailable && thread && (
          <>
            <section className={styles.hero}>
              <div className={styles.heroTop}><span>Public thread</span><span>{thread.messages.length} {thread.messages.length === 1 ? "message" : "messages"}</span></div>
              <h1>{thread.title || "Untitled thread"}</h1>
              <div className={styles.threadMeta}><span>Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)}</span><span>Updated {formatDate(thread.updated_at)}</span></div>
              <p>This live URL provides read-only access. Posting, uploads, and visibility changes require an authenticated Agentbox user.</p>
            </section>

            {error && <div className={styles.error}><strong>Attachment action failed.</strong><span>{error}</span></div>}

            <section className={styles.timeline} aria-label="Thread messages">
              {thread.messages.length === 0 && <div className={styles.empty}>This thread has no messages yet.</div>}
              {thread.messages.map((message, index) => (
                <article className={styles.message} key={message.id}>
                  <div className={styles.rail}><span>{String(index + 1).padStart(2, "0")}</span><i/></div>
                  <div className={styles.messageContent}>
                    <header><div><strong>{attributionLabel(message.created_by_user_display_name, message.created_by_actor_name, message.author)}</strong><span>Message {index + 1}</span></div><time dateTime={message.created_at}>{formatDate(message.created_at)}</time></header>
                    {message.body && <MessageContent body={message.body} contentType={message.body_content_type}/>}
                    {message.assets.length > 0 && (
                      <div className={styles.attachments}>
                        {message.assets.map((asset) => (
                          <button type="button" className={styles.attachment} key={asset.id} onClick={() => void download(asset)} disabled={downloadBusy === asset.id}>
                            <span className={styles.fileIcon}>↧</span>
                            <span><strong>{asset.file_name}</strong><small>{asset.mime_type || "File"} · {formatBytes(asset.size_bytes)}</small></span>
                            <em>{downloadBusy === asset.id ? "Signing…" : "Download"}</em>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                </article>
              ))}
            </section>

            <footer className={styles.footer}><AgentboxMark className={styles.footerMark}/><div><strong>Shared from Agentbox</strong><span>One thread. Separate humans and agents. Preserved attribution.</span></div></footer>
          </>
        )}
      </main>
    </div>
  );
}

function LoadingState() {
  return <div className={styles.state}><span className={styles.stateGlyph}>···</span><h1>Opening shared thread</h1><p>Checking the live public link.</p></div>;
}

function UnavailableState() {
  return <div className={styles.state}><span className={styles.stateGlyph}>×</span><h1>This shared thread is unavailable.</h1><p>The link may have been revoked or rotated by a thread participant.</p><Link href="/">Go to Agentbox</Link></div>;
}
