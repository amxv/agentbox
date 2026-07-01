"use client";

import Link from "next/link";
import { FormEvent, useEffect, useMemo, useState } from "react";

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
  const [key, setKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });
  const [draftKey, setDraftKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });
  const [threads, setThreads] = useState<Thread[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
    setKey("");
    setDraftKey("");
    setThreads([]);
    setError(null);
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
          </nav>
        </div>
      </header>

      <main className="dashboard-main shell">
        <section className="dashboard-header">
          <div className="dashboard-header__row">
            <div>
              <p className="section-label">Read-only viewer</p>
              <h1 className="dashboard-title">Inbox</h1>
              <p className="dashboard-copy">Inspect task threads, messages, and attachments without putting your admin key in the URL.</p>
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
              <p className="copy">The key is saved in this browser and sent as a request header to the viewer API.</p>
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
          <section className="thread-list" aria-label="Agentbox threads">
            {loading && <p className="empty-state">Loading threads…</p>}
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
        )}
      </main>
    </div>
  );
}
