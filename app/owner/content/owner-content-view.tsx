"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { AppNav } from "../../components/app-nav";
import { attributionLabel } from "../../components/attribution";
import { fetchSession, type AuthContext } from "../../components/session";
import styles from "./owner-content.module.css";

type User = {
  id: string;
  email: string;
  display_name: string;
  disabled_at?: string;
};

type Team = {
  id: string;
  slug: string;
  name: string;
};

type VisibilitySummary = {
  private: boolean;
  public: boolean;
  shared_teams: Team[];
};

type ThreadSummary = {
  id: string;
  title: string;
  owner_user_id: string;
  owner: User;
  created_by: string;
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
  updated_at: string;
  message_count: number;
  last_message_preview: string;
  matched_snippets: string[];
  visibility_summary: VisibilitySummary;
};

type TeamWithMembers = Team & { members: User[] };
type PageInfo = {
  limit: number;
  offset: number;
  has_more: boolean;
  next_cursor?: string;
  previous_cursor?: string;
};

async function responseJSON(response: Response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error ?? data.message ?? `HTTP ${response.status}`);
  return data;
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function visibilityLabels(summary: VisibilitySummary) {
  const labels: string[] = [];
  if (summary.private) labels.push("Private");
  labels.push(...summary.shared_teams.map((team) => team.name));
  if (summary.public) labels.push("Public");
  return labels.length > 0 ? labels : ["Unshared"];
}

export function OwnerContentView() {
  const router = useRouter();
  const [auth, setAuth] = useState<AuthContext | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [teams, setTeams] = useState<TeamWithMembers[]>([]);
  const [threads, setThreads] = useState<ThreadSummary[]>([]);
  const [query, setQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [userID, setUserID] = useState("");
  const [teamRef, setTeamRef] = useState("");
  const [cursor, setCursor] = useState("");
  const [page, setPage] = useState<PageInfo>({ limit: 25, offset: 0, has_more: false });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (signal: AbortSignal) => {
    setLoading(true);
    setError(null);
    try {
      const session = await fetchSession(signal);
      if (!session?.is_owner || session.subject_type !== "user_session") {
        router.replace("/login?next=/owner/content");
        return;
      }
      setAuth(session);
      const params = new URLSearchParams({ limit: "25" });
      if (cursor) params.set("cursor", cursor);
      if (userID) params.set("user_id", userID);
      if (teamRef) params.set("team", teamRef);
      const contentPath = submittedQuery
        ? `/api/owner/content/search?${new URLSearchParams({ ...Object.fromEntries(params), query: submittedQuery }).toString()}`
        : `/api/owner/content/threads?${params.toString()}`;
      const [contentResponse, usersResponse, teamsResponse] = await Promise.all([
        fetch(contentPath, { cache: "no-store", signal }),
        fetch("/api/owner/users?limit=100", { cache: "no-store", signal }),
        fetch("/api/owner/teams?limit=100", { cache: "no-store", signal })
      ]);
      if ([contentResponse, usersResponse, teamsResponse].some((response) => response.status === 401 || response.status === 403)) {
        router.replace("/login?next=/owner/content");
        return;
      }
      const [contentData, usersData, teamsData] = await Promise.all([
        responseJSON(contentResponse),
        responseJSON(usersResponse),
        responseJSON(teamsResponse)
      ]);
      setThreads(contentData.threads ?? []);
      setPage(contentData.page ?? { limit: 25, offset: 0, has_more: false });
      setUsers(usersData.users ?? []);
      setTeams(teamsData.teams ?? []);
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") return;
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (!signal.aborted) setLoading(false);
    }
  }, [cursor, router, submittedQuery, teamRef, userID]);

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => { void load(controller.signal); }, 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [load]);

  const selectedUser = useMemo(() => users.find((user) => user.id === userID), [userID, users]);
  const selectedTeam = useMemo(() => teams.find((team) => team.id === teamRef || team.slug === teamRef), [teamRef, teams]);

  function submitSearch(event: FormEvent) {
    event.preventDefault();
    setCursor("");
    setSubmittedQuery(query.trim());
  }

  function changeUser(value: string) {
    setCursor("");
    setUserID(value);
  }

  function changeTeam(value: string) {
    setCursor("");
    setTeamRef(value);
  }

  return (
    <main className={styles.page}>
      <AppNav title="All content (read-only)" auth={auth} />

      <section className={styles.shell}>
        <div className={styles.warning}>
          <strong>OWNER VIEW · READ ONLY</strong>
          <span>This deployment-wide audit surface bypasses normal thread visibility only for this permanent-owner browser session. It cannot post, upload, or change visibility.</span>
        </div>

        <div className={styles.hero}>
          <div>
            <p>Deployment-wide content</p>
            <h1>Inspect every thread without changing it.</h1>
            <span>Search private and shared content, filter by owner or team, and review preserved attribution and attachment tombstones.</span>
          </div>
          <div className={styles.count}><b>{threads.length}</b><span>matching threads</span></div>
        </div>

        <section className={styles.filters}>
          <form onSubmit={submitSearch} className={styles.search}>
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search titles and message bodies" aria-label="Search all content" />
            <button type="submit">Search</button>
            {submittedQuery && <button type="button" className={styles.ghost} onClick={() => { setCursor(""); setQuery(""); setSubmittedQuery(""); }}>Clear</button>}
          </form>
          <label>
            <span>User</span>
            <select value={userID} onChange={(event) => changeUser(event.target.value)}>
              <option value="">All users</option>
              {users.map((user) => <option value={user.id} key={user.id}>{user.display_name}{user.disabled_at ? " · disabled" : ""}</option>)}
            </select>
          </label>
          <label>
            <span>Team share</span>
            <select value={teamRef} onChange={(event) => changeTeam(event.target.value)}>
              <option value="">All teams</option>
              {teams.map((team) => <option value={team.id} key={team.id}>{team.name}</option>)}
            </select>
          </label>
        </section>

        {(selectedUser || selectedTeam || submittedQuery) && <div className={styles.activeFilters}>
          <span>Showing:</span>
          {submittedQuery && <em>“{submittedQuery}”</em>}
          {selectedUser && <em>{selectedUser.display_name}</em>}
          {selectedTeam && <em>{selectedTeam.name}</em>}
        </div>}

        {error && <div className={styles.error}><strong>Owner content failed.</strong><span>{error}</span></div>}
        {loading && <div className={styles.empty}>Loading deployment-wide content…</div>}
        {!loading && !error && threads.length === 0 && <div className={styles.empty}>No threads match these filters.</div>}

        {!loading && !error && <section className={styles.list}>
          {threads.map((thread) => (
            <Link className={styles.card} href={`/owner/content/${thread.id}`} key={thread.id}>
              <div className={styles.cardHead}>
                <div>
                  <span>{thread.owner.display_name}{thread.owner.disabled_at ? " · disabled" : ""}</span>
                  <time dateTime={thread.updated_at}>{formatDate(thread.updated_at)}</time>
                </div>
                <code>{thread.id}</code>
              </div>
              <h2>{thread.title}</h2>
              <p>{thread.matched_snippets?.[0] || thread.last_message_preview || "No messages yet."}</p>
              <div className={styles.cardFoot}>
                <span>{thread.message_count} message{thread.message_count === 1 ? "" : "s"} · Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)}</span>
                <span className={styles.badges}>{visibilityLabels(thread.visibility_summary).map((label, index) => <em key={`${label}-${index}`}>{label}</em>)}</span>
              </div>
            </Link>
          ))}
        </section>}
        {!loading && !error && (page.previous_cursor !== undefined || page.next_cursor !== undefined) && <div className={styles.pager}>
          <button type="button" disabled={page.previous_cursor === undefined} onClick={() => setCursor(page.previous_cursor ?? "")}>Newer</button>
          <span>Showing {page.offset + 1}–{page.offset + threads.length}</span>
          <button type="button" disabled={page.next_cursor === undefined} onClick={() => setCursor(page.next_cursor ?? "")}>Older</button>
        </div>}
      </section>
    </main>
  );
}
