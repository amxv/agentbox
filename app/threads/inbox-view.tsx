"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { MessageComposer } from "../components/message-composer";
import { AuthContext, fetchSession, signOutSession } from "../components/session";
import { ThemeSwitcher } from "../components/theme-switcher";
import { createDashboardThread, postDashboardMessage } from "../components/agentbox-write";
import { attributionLabel } from "../components/attribution";

type Thread = {
  id: string;
  title: string;
  updated_at: string;
  created_by: string;
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
  visibility_summary: {
    owned_by_me: boolean;
    private: boolean;
    shared_with_me: boolean;
    shared_teams: Team[];
    matched_teams: Team[];
    public: boolean;
  };
};

type Team = {
  id: string;
  slug: string;
  name: string;
};

type InboxFilter = "all" | "private" | "shared" | "public" | `team:${string}`;

function visibilityLabels(thread: Thread) {
  const labels: string[] = [];
  if (thread.visibility_summary.private) labels.push("Private");
  for (const team of thread.visibility_summary.shared_teams) labels.push(team.name);
  if (thread.visibility_summary.public) labels.push("Public");
  return labels.length > 0 ? labels : [thread.visibility_summary.owned_by_me ? "Owned" : "Shared"];
}

function formatDate(value: string) {
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}

export function InboxView() {
  const router = useRouter();
  const [auth, setAuth] = useState<AuthContext | null>(null);
  const [threads, setThreads] = useState<Thread[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [activeFilter, setActiveFilter] = useState<InboxFilter>("all");
  const [newThreadTitle, setNewThreadTitle] = useState("");
  const [showCreateComposer, setShowCreateComposer] = useState(false);
  const [creatingEmpty, setCreatingEmpty] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);

  const loadThreads = useCallback(async function loadThreads() {
    setLoading(true);
    setError(null);
    try {
      const session = await fetchSession();
      if (!session) {
        router.replace("/login?next=/threads");
        return;
      }
      setAuth(session);
      const query = new URLSearchParams();
      if (activeFilter.startsWith("team:")) {
        query.set("filter", "team");
        query.set("team", activeFilter.slice("team:".length));
      } else if (activeFilter !== "all") {
        query.set("filter", activeFilter);
      }
      const suffix = query.size > 0 ? `?${query.toString()}` : "";
      const [response, teamsResponse] = await Promise.all([
        fetch(`/api/threads${suffix}`, { cache: "no-store" }),
        fetch("/api/me/teams", { cache: "no-store" })
      ]);
      const [data, teamsData] = await Promise.all([response.json(), teamsResponse.json()]);
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      if (!teamsResponse.ok) throw new Error(teamsData.error ?? `HTTP ${teamsResponse.status}`);
      setThreads(data.threads ?? []);
      setTeams(teamsData.teams ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [activeFilter, router]);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void loadThreads();
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [loadThreads]);

  const latestUpdatedAt = useMemo(() => {
    if (threads.length === 0) return null;
    return threads.reduce((latest, thread) => {
      const current = new Date(thread.updated_at).getTime();
      return current > latest ? current : latest;
    }, 0);
  }, [threads]);

  async function signOut() {
    try {
      await signOutSession();
    } finally {
      router.replace("/login");
    }
  }

  async function createThreadOnly() {
    const title = newThreadTitle.trim();
    if (!title || creatingEmpty) return;
    setCreatingEmpty(true);
    setCreateError(null);
    try {
      const thread = await createDashboardThread(title);
      setNewThreadTitle("");
      setShowCreateComposer(false);
      router.push(`/threads/${thread.id}`);
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreatingEmpty(false);
    }
  }

  async function createThreadWithMessage(body: string, files: File[]) {
    const title = newThreadTitle.trim();
    if (!title) throw new Error("Thread title is required.");
    const thread = await createDashboardThread(title);
    await postDashboardMessage(thread.id, body, files);
    setNewThreadTitle("");
    setShowCreateComposer(false);
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
            {auth && <span className="session-chip">{attributionLabel(auth.user_display_name, auth.actor_name)}</span>}
            {auth && <button className="site-nav__link" type="button" onClick={() => void signOut()}>Sign out</button>}
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <main className="dashboard-main shell">
        <section className="dashboard-header">
          <div className="dashboard-header__row">
            <div>
              <p className="section-label">Unified inbox</p>
              <h1 className="dashboard-title">Inbox</h1>
              <p className="dashboard-copy">Private threads you own and every thread currently shared with one of your teams.</p>
            </div>
            {auth && (
              <div className="card card--compact">
                <p className="stat-label">Current filter</p>
                <h2 className="card-title">{threads.length}</h2>
                <p className="copy">{latestUpdatedAt ? `Last updated ${formatDate(new Date(latestUpdatedAt).toISOString())}` : "No activity yet."}</p>
              </div>
            )}
          </div>
        </section>

        <div className="dashboard-stack">
          <div className="composer-toggle-row">
            <button className="button button--solid" type="button" onClick={() => setShowCreateComposer((value) => !value)}>
              {showCreateComposer ? "Close" : "+ Create Thread"}
            </button>
          </div>

          {showCreateComposer && (
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
                  {creatingEmpty ? "Creating..." : "Create empty"}
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
          )}

          <section className="inbox-filter-shell" aria-label="Inbox filters">
            <div className="inbox-filter-heading">
              <div>
                <span className="stat-label">Visibility</span>
                <strong>Filter accessible threads</strong>
              </div>
              <span className="thread-meta">Filtering happens on the server.</span>
            </div>
            <div className="inbox-filter-list">
              {([
                ["all", "All"],
                ["private", "Private"],
                ["shared", "Shared with me"],
                ["public", "Public"]
              ] as const).map(([value, label]) => (
                <button
                  className={activeFilter === value ? "inbox-filter inbox-filter--active" : "inbox-filter"}
                  key={value}
                  type="button"
                  aria-pressed={activeFilter === value}
                  onClick={() => setActiveFilter(value)}
                >
                  {label}
                </button>
              ))}
              {teams.map((team) => {
                const value = `team:${team.id}` as InboxFilter;
                return (
                  <button
                    className={activeFilter === value ? "inbox-filter inbox-filter--active" : "inbox-filter"}
                    key={team.id}
                    type="button"
                    aria-pressed={activeFilter === value}
                    onClick={() => setActiveFilter(value)}
                  >
                    {team.name}
                  </button>
                );
              })}
            </div>
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
            {!loading && !error && threads.length === 0 && <p className="empty-state">No threads match this filter.</p>}
            {!loading && !error && threads.map((thread) => (
              <Link key={thread.id} href={`/threads/${thread.id}`} className="thread-card">
                <div className="thread-meta-row">
                  <span className="thread-meta mono">{thread.id}</span>
                  <span className="thread-meta">Updated {formatDate(thread.updated_at)}</span>
                </div>
                <span className="thread-title">{thread.title}</span>
                <div className="thread-card__footer">
                  <span className="thread-meta">Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)}</span>
                  <span className="visibility-badge-list">
                    {visibilityLabels(thread).map((label, index) => <span className="visibility-badge" key={`${label}-${index}`}>{label}</span>)}
                  </span>
                </div>
              </Link>
            ))}
          </section>
        </div>
      </main>
    </div>
  );
}
