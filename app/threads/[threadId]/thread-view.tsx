"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { CopyButton } from "../../components/copy-button";
import { MessageContent } from "./message-content";
import { MessageComposer } from "../../components/message-composer";
import { postDashboardMessage } from "../../components/agentbox-write";
import { AppNav } from "../../components/app-nav";
import { AuthContext, fetchSession } from "../../components/session";
import { ThreadVisibilityControl } from "./thread-visibility-control";
import { attributionLabel } from "../../components/attribution";

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

export function ThreadView({ threadId }: { threadId: string }) {
  const router = useRouter();
  const [auth, setAuth] = useState<AuthContext | null>(null);
  const [thread, setThread] = useState<Thread | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showReplyComposer, setShowReplyComposer] = useState(false);
  const [expandedMessages, setExpandedMessages] = useState<Set<string>>(() => new Set());
  const [assetResolutions, setAssetResolutions] = useState<Record<string, AssetResolution>>({});
  const [assetBusy, setAssetBusy] = useState<string | null>(null);

  const loadThread = useCallback(async function loadThread() {
    setLoading(true);
    setError(null);
    try {
      const session = await fetchSession();
      if (!session) {
        router.replace(`/login?next=/threads/${encodeURIComponent(threadId)}`);
        return;
      }
      setAuth(session);
      const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/view`, { cache: "no-store" });
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
    const timeout = window.setTimeout(() => {
      void loadThread();
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [loadThread]);

  async function postReply(body: string, files: File[]) {
    await postDashboardMessage(threadId, body, files);
    await loadThread();
    setShowReplyComposer(false);
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
          [asset.id]: {
            available: false,
            unavailable_reason: data.unavailable_reason || "Attachment unavailable"
          }
        }));
        return;
      }
      const field = kind === "preview" ? "preview_url" : "download_url";
      const signedURL = data[field];
      if (typeof signedURL !== "string" || signedURL === "") {
        throw new Error(`The attachment ${kind} URL was not returned.`);
      }
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
    <div className="dashboard-page">
      <AppNav title="Thread" auth={auth} />

      <main className="dashboard-main shell">
        <section className="dashboard-header">
          <div className="dashboard-header__row">
            <div>
              <p className="section-label">Accessible thread</p>
              <h1 className="dashboard-title">{thread?.title ?? "Thread"}</h1>
              <div className="thread-id-row">
                <p className="dashboard-copy mono">
                  {thread?.id ?? threadId}{thread ? ` · Updated ${formatDate(thread.updated_at)}` : ""}
                </p>
                <CopyButton value={thread?.id ?? threadId} label="Copy thread ID" />
              </div>
              {thread && <p className="thread-meta">Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)}</p>}
            </div>
            {thread && (
              <div className="card card--compact">
                <p className="stat-label">Contents</p>
                <h2 className="card-title">{thread.messages.length} messages</h2>
                <p className="copy">{assetCount} attachments in this thread.</p>
              </div>
            )}
          </div>
        </section>

        <div className="composer-toggle-row">
          <button className="button button--solid" type="button" onClick={() => setShowReplyComposer((value) => !value)}>
            {showReplyComposer ? "Close" : "+ Reply"}
          </button>
          {thread && <ThreadVisibilityControl threadId={thread.id} />}
        </div>

        {showReplyComposer && (
          <MessageComposer
            label="Reply"
            placeholder="Post a message. Markdown is detected automatically."
            submitLabel="Post message"
            onSubmit={postReply}
          />
        )}

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
                        <strong className="message-author">{attributionLabel(message.created_by_user_display_name, message.created_by_actor_name, message.author)}</strong>
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
                        {message.assets.map((asset) => {
                          const resolution = assetResolutions[asset.id];
                          const unavailable = asset.unavailable || resolution?.available === false;
                          const unavailableReason = resolution?.unavailable_reason || asset.unavailable_reason || "Attachment unavailable";
                          const previewBusy = assetBusy === `preview:${asset.id}`;
                          const downloadBusy = assetBusy === `download:${asset.id}`;
                          return (
                            <div key={asset.id} className="asset-card">
                              {!asset.purged_at && !unavailable && resolution?.preview_url && (
                                <div className="preview-link">
                                  {/* eslint-disable-next-line @next/next/no-img-element */}
                                  <img className="preview-image" src={resolution.preview_url} alt={asset.file_name} loading="lazy" />
                                </div>
                              )}
                              <div className="asset-row">
                                <span className="thread-title">{asset.file_name}</span>
                                <span className="asset-meta">{asset.mime_type ?? "unknown type"} · {formatBytes(asset.size_bytes)}</span>
                              </div>
                              {asset.purged_at ? (
                                <span className="asset-tombstone">Attachment deleted by deployment owner</span>
                              ) : unavailable ? (
                                <span className="asset-tombstone">{unavailableReason}</span>
                              ) : (
                                <div className="asset-row">
                                  {asset.preview_path && !resolution?.preview_url && (
                                    <button className="download-link" type="button" disabled={previewBusy} onClick={() => void resolveAsset(asset, "preview")}>
                                      {previewBusy ? "Loading preview…" : "Load preview"}
                                    </button>
                                  )}
                                  {asset.download_path && (
                                    <button className="download-link" type="button" disabled={downloadBusy} onClick={() => void resolveAsset(asset, "download")}>
                                      {downloadBusy ? "Signing…" : "Open attachment"}
                                    </button>
                                  )}
                                </div>
                              )}
                            </div>
                          );
                        })}
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
