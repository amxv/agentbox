"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { CopyButton } from "../../components/copy-button";
import { MessageContent } from "./message-content";
import { MessageComposer } from "../../components/message-composer";
import { ensureDashboardActorKey, postDashboardMessage } from "../../components/agentbox-write";
import { ThemeSwitcher } from "../../components/theme-switcher";

const STORAGE_KEY = "agentbox_admin_key";

type Asset = {
  id: string;
  file_name: string;
  mime_type: string | null;
  size_bytes: number;
  download_url?: string | null;
  preview_url?: string | null;
};

type Message = {
  id: string;
  author: string;
  body: string;
  body_content_type?: string | null;
  created_at: string;
  assets: Asset[];
};

type Thread = {
  id: string;
  title: string;
  updated_at: string;
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

export function ThreadView({ threadId }: { threadId: string }) {
  const router = useRouter();
  const [thread, setThread] = useState<Thread | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expandedMessages, setExpandedMessages] = useState<Set<string>>(() => new Set());

  const loadThread = useCallback(async function loadThread(adminKey: string) {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(`/api/viewer/threads/${encodeURIComponent(threadId)}`, {
        headers: { "x-agentbox-admin-key": adminKey }
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setThread(data.thread);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [threadId]);

  useEffect(() => {
    const key = window.localStorage.getItem(STORAGE_KEY);
    if (!key) {
      router.replace("/threads");
      return;
    }
    const timeout = window.setTimeout(() => {
      void loadThread(key);
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [loadThread, router]);

  async function postReply(body: string, files: File[]) {
    const key = window.localStorage.getItem(STORAGE_KEY);
    if (!key) throw new Error("Admin key is required.");
    const actorKey = await ensureDashboardActorKey(key);
    await postDashboardMessage(actorKey, threadId, body, files);
    await loadThread(key);
  }
  const assetCount = useMemo(() => {
    return thread?.messages.reduce((total, message) => total + message.assets.length, 0) ?? 0;
  }, [thread]);

  function toggleMessage(messageId: string) {
    setExpandedMessages((current) => {
      const next = new Set(current);
      if (next.has(messageId)) {
        next.delete(messageId);
      } else {
        next.add(messageId);
      }
      return next;
    });
  }

  return (
    <div className="dashboard-page">
      <header className="site-header">
        <div className="shell site-header__inner">
          <Link className="brand" href="/threads">
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">Back to inbox</span>
          </Link>
          <nav className="site-nav" aria-label="Thread navigation">
            <Link className="site-nav__link" href="/threads">Inbox</Link>
            <Link className="site-nav__link" href="/keys">Keys</Link>
            <Link className="site-nav__link" href="/">Home</Link>
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <main className="dashboard-main shell">
        <section className="dashboard-header">
          <div className="dashboard-header__row">
            <div>
              <p className="section-label">Shared thread</p>
              <h1 className="dashboard-title">{thread?.title ?? "Thread"}</h1>
              <div className="thread-id-row">
                <p className="dashboard-copy mono">
                  {thread?.id ?? threadId}{thread ? ` · Updated ${formatDate(thread.updated_at)}` : ""}
                </p>
                <CopyButton value={thread?.id ?? threadId} label="Copy thread ID" />
              </div>
            </div>
            {thread && (
              <div className="card">
                <p className="stat-label">Contents</p>
                <h2 className="card-title">{thread.messages.length} messages</h2>
                <p className="copy">{assetCount} attachments in this thread.</p>
              </div>
            )}
          </div>
        </section>

        <MessageComposer
          label="Reply"
          placeholder="Post a message. Markdown is detected automatically."
          submitLabel="Post message"
          onSubmit={postReply}
        />

        <section className="message-list" aria-label="Thread messages">
          {loading && (
            <div className="skeleton-list" aria-label="Loading thread" aria-busy="true">
              {Array.from({ length: 3 }).map((_, index) => (
                <div className="skeleton-message-card" aria-hidden="true" key={index}>
                  <div className="skeleton-message-main">
                    <span className="skeleton-pill skeleton-pill--small" />
                    <div className="skeleton-stack">
                      <span className="skeleton-line skeleton-line--medium" />
                      <span className="skeleton-line skeleton-line--long" />
                    </div>
                  </div>
                  <div className="skeleton-meta-row">
                    <span className="skeleton-pill" />
                    <span className="skeleton-pill" />
                    <span className="skeleton-circle" />
                  </div>
                </div>
              ))}
            </div>
          )}
          {error && (
            <div className="error-card">
              <strong>Could not load thread.</strong>
              <span>{error}</span>
            </div>
          )}
          {!loading && !error && thread?.messages.length === 0 && <p className="empty-state">No messages yet.</p>}
          {!loading && !error && thread?.messages.map((message, index) => {
            const isExpanded = expandedMessages.has(message.id);
            const panelId = `message-panel-${message.id}`;
            return (
              <article key={message.id} className={isExpanded ? "message-card message-card--expanded" : "message-card"}>
                <div
                  role="button"
                  tabIndex={0}
                  className="message-toggle"
                  aria-expanded={isExpanded}
                  aria-controls={panelId}
                  onClick={() => toggleMessage(message.id)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      toggleMessage(message.id);
                    }
                  }}
                >
                  <span className="message-toggle__main">
                    <span className="message-index">#{index + 1}</span>
                    <span className="message-heading">
                      <span className="message-title-row">
                        <strong className="message-author">{message.author}</strong>
                        {message.id && (
                          <span className="message-id-chip" onClick={(event) => event.stopPropagation()}>
                            <span className="message-id-label">Message ID</span>
                            <span className="message-id-value mono">{message.id}</span>
                            <CopyButton value={message.id} label="Copy message ID" />
                          </span>
                        )}
                      </span>
                      <span className="message-preview">{getMessagePreview(message.body)}</span>
                    </span>
                  </span>
                  <span className="message-toggle__meta">
                    <span>{getMessageKind(message.body_content_type)}</span>
                    {message.assets.length > 0 && <span>{message.assets.length} attachments</span>}
                    <span>{formatDate(message.created_at)}</span>
                    <span className="message-chevron" aria-hidden="true" />
                  </span>
                </div>
                {isExpanded && (
                  <div id={panelId} className="message-panel">
                    <MessageContent body={message.body} contentType={message.body_content_type} />
                    {message.assets.length > 0 && (
                      <div className="asset-list">
                        <span className="asset-label">Attachments</span>
                        {message.assets.map((asset) => (
                          <div key={asset.id} className="asset-card">
                            {asset.preview_url && (
                              <a className="preview-link" href={asset.download_url ?? asset.preview_url} target="_blank" rel="noreferrer">
                                {/* eslint-disable-next-line @next/next/no-img-element */}
                                <img className="preview-image" src={asset.preview_url} alt={asset.file_name} loading="lazy" />
                              </a>
                            )}
                            <div className="asset-row">
                              <span className="thread-title">{asset.file_name}</span>
                              <span className="asset-meta">{asset.mime_type ?? "unknown type"} · {formatBytes(asset.size_bytes)}</span>
                            </div>
                            {asset.download_url && (
                              <a className="download-link" href={asset.download_url} target="_blank" rel="noreferrer">Open attachment</a>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </article>
            );
          })}
        </section>
      </main>
    </div>
  );
}
