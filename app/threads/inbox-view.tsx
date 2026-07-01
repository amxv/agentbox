"use client";

import Link from "next/link";
import { FormEvent, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { MessageComposer } from "../components/message-composer";
import { ThemeSwitcher } from "../components/theme-switcher";
import { createDashboardThread, ensureDashboardActorKey, postDashboardMessage } from "../components/agentbox-write";

const STORAGE_KEY = "agentbox_admin_key";

type Thread = {
  id: string;
  title: string;
  updated_at: string;
  created_by: string;
};

function formatDate(value: string) {
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}

export function InboxView() {
  const router = useRouter();
  const [key, setKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });
  const [draftKey, setDraftKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });
  const [threads, setThreads] = useState<Thread[]>([]);
  const [newThreadTitle, setNewThreadTitle] = useState("");
  const [creatingEmpty, setCreatingEmpty] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);

  useEffect(() => {
    if (!key) return;

    async function loadThreads(adminKey: string) {
      setLoading(true);
      setError(null);
      try {
        const response = await fetch("/api/viewer/threads", {
          headers: { "x-agentbox-admin-key": adminKey }
        });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
        setThreads(data.threads ?? []);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    }

    void loadThreads(key);
  }, [key]);

  const latestUpdatedAt = useMemo(() => {
    if (threads.length === 0) return null;
    return threads.reduce((latest, thread) => {
      const current = new Date(thread.updated_at).getTime();
      return current > latest ? current : latest;
    }, 0);
  }, [threads]);

  function saveKey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = draftKey.trim();
    if (!trimmed) return;
    window.localStorage.setItem(STORAGE_KEY, trimmed);
    setKey(trimmed);
  }

  function signOut() {
    window.localStorage.removeItem(STORAGE_KEY);
    window.localStorage.removeItem("agentbox_actor_key");
    setKey("");
    setDraftKey("");
    setThreads([]);
    setError(null);
    setCreateError(null);
  }

  async function createThreadOnly() {
    const title = newThreadTitle.trim();
    if (!key || !title || creatingEmpty) return;
    setCreatingEmpty(true);
    setCreateError(null);
    try {
      const actorKey = await ensureDashboardActorKey(key);
      const thread = await createDashboardThread(actorKey, title);
      setNewThreadTitle("");
      router.push(`/threads/${thread.id}`);
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreatingEmpty(false);
    }
  }

  async function createThreadWithMessage(body: string, files: File[]) {
    const title = newThreadTitle.trim();
    if (!key || !title) throw new Error("Thread title is required.");
    const actorKey = await ensureDashboardActorKey(key);
    const thread = await createDashboardThread(actorKey, title);
    await postDashboardMessage(actorKey, thread.id, body, files);
    setNewThreadTitle("");
    router.push(`/threads/${thread.id}`);
  }

  return (
    <div className="dashboard-page">
      <header className="site-header">
        <div className="shell site-header__inner">
          <Link className="brand" href="/">
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">Thread dashboard</span>
          </Link>
          <nav className="site-nav" aria-label="Inbox navigation">
            <Link className="site-nav__link" href="/keys">Keys</Link>
            <Link className="site-nav__link" href="/">Home</Link>
            {key && <button className="site-nav__link" type="button" onClick={signOut}>Forget key</button>}
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <main className="dashboard-main shell">
        <section className="dashboard-header">
          <div className="dashboard-header__row">
            <div>
              <p className="section-label">Shared viewer</p>
              <h1 className="dashboard-title">Inbox</h1>
              <p className="dashboard-copy">Inspect task threads, create new threads, and post replies as the built-in user actor.</p>
            </div>
            {key && (
              <div className="card">
                <p className="stat-label">Threads</p>
                <h2 className="card-title">{threads.length}</h2>
                <p className="copy">{latestUpdatedAt ? `Last updated ${formatDate(new Date(latestUpdatedAt).toISOString())}` : "No activity yet."}</p>
              </div>
            )}
          </div>
        </section>

        {!key ? (
          <form className="sign-in-card" onSubmit={saveKey}>
            <div>
              <p className="section-label">Sign in</p>
              <h2 className="card-title">Enter your admin key</h2>
              <p className="copy">The dashboard automatically creates a hidden actor key named user for posting.</p>
            </div>
            <input
              className="form-input"
              value={draftKey}
              onChange={(event) => setDraftKey(event.target.value)}
              placeholder="ADMIN_KEY"
              type="password"
            />
            <button className="button button--solid" type="submit">View inbox</button>
          </form>
        ) : (
          <div className="dashboard-stack">
            <section className="composer-shell" aria-label="Create thread">
              <div className="composer-title-row">
                <input
                  className="form-input"
                  value={newThreadTitle}
                  onChange={(event) => setNewThreadTitle(event.target.value)}
                  placeholder="New thread title"
                  type="text"
                />
                <button className="button button--ghost" disabled={creatingEmpty || !newThreadTitle.trim()} type="button" onClick={() => void createThreadOnly()}>
                  {creatingEmpty ? "Creating…" : "Create empty"}
                </button>
              </div>
              <MessageComposer
                canSubmit={Boolean(newThreadTitle.trim())}
                label="New thread"
                placeholder="Add the first message. Markdown is detected automatically."
                submitLabel="Create and post"
                onSubmit={createThreadWithMessage}
              />
              {createError && (
                <div className="error-card">
                  <strong>Could not create thread.</strong>
                  <span>{createError}</span>
                </div>
              )}
            </section>

            <section className="thread-list" aria-label="Agentbox threads">
              {loading && (
                <div className="skeleton-list" aria-label="Loading threads" aria-busy="true">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <div className="skeleton-thread-card" aria-hidden="true" key={index}>
                      <div className="skeleton-card-meta">
                        <span className="skeleton-line skeleton-line--medium" />
                        <span className="skeleton-line skeleton-line--short" />
                      </div>
                      <span className="skeleton-line skeleton-line--long" />
                      <span className="skeleton-line skeleton-line--short" />
                    </div>
                  ))}
                </div>
              )}
              {error && (
                <div className="error-card">
                  <strong>Could not load inbox.</strong>
                  <span>{error}</span>
                </div>
              )}
              {!loading && !error && threads.length === 0 && <p className="empty-state">No threads yet.</p>}
              {!loading && !error && threads.map((thread) => (
                <Link key={thread.id} href={`/threads/${thread.id}`} className="thread-card">
                  <div className="thread-meta-row">
                    <span className="thread-meta mono">{thread.id}</span>
                    <span className="thread-meta">Updated {formatDate(thread.updated_at)}</span>
                  </div>
                  <span className="thread-title">{thread.title}</span>
                  <span className="thread-meta">Created by {thread.created_by}</span>
                </Link>
              ))}
            </section>
          </div>
        )}
      </main>
    </div>
  );
}
