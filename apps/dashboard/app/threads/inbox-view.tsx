"use client";

import { ArrowUpRightIcon, InboxIcon, PlusIcon, SearchIcon, XIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { MessageComposer } from "../components/message-composer";
import { AuthContext, fetchSession } from "../components/session";
import { createDashboardThread, postDashboardMessage } from "../components/agentbox-write";
import { attributionLabel } from "../components/attribution";
import { MetricStrip, MonoValue, PanelHeader, PanelMain, SectionIntro } from "../components/panel-shell";

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

type ThreadPageInfo = {
  limit: number;
  has_more: boolean;
  next_cursor?: string;
};

const initialThreadPage: ThreadPageInfo = { limit: 50, has_more: false };
const visiblePollIntervalMs = 30_000;
const searchedPollIntervalMs = 60_000;
const fullRefreshAfterInactiveMs = 15_000;
const autoRefreshDedupMs = 1_000;

function threadQuery(filter: InboxFilter, searchQuery: string, cursor?: string, limit = 50) {
  const query = new URLSearchParams({ limit: String(limit) });
  if (filter.startsWith("team:")) {
    query.set("filter", "team");
    query.set("team", filter.slice("team:".length));
  } else if (filter !== "all") {
    query.set("filter", filter);
  }
  if (searchQuery) query.set("query", searchQuery);
  if (cursor) query.set("cursor", cursor);
  return query;
}

function appendUniqueThreads(current: Thread[], incoming: Thread[]) {
  const seen = new Set(current.map((thread) => thread.id));
  return [...current, ...incoming.filter((thread) => !seen.has(thread.id))];
}

function threadVersion(thread?: Thread) {
  return thread ? `${thread.id}:${thread.updated_at}` : "";
}

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
  const [threadPage, setThreadPage] = useState<ThreadPageInfo>(initialThreadPage);
  const [teams, setTeams] = useState<Team[]>([]);
  const [activeFilter, setActiveFilter] = useState<InboxFilter>("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [newThreadTitle, setNewThreadTitle] = useState("");
  const [showCreateComposer, setShowCreateComposer] = useState(false);
  const [creatingEmpty, setCreatingEmpty] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const requestGeneration = useRef(0);
  const latestThreadVersion = useRef("");
  const refreshInFlight = useRef<Promise<void> | null>(null);
  const latestCheckInFlight = useRef<Promise<void> | null>(null);
  const inactiveSince = useRef<number | null>(null);
  const lastAutoRefreshAt = useRef(0);

  const loadThreads = useCallback(async function loadThreads(signal: AbortSignal, generation: number) {
    setLoading(true);
    setError(null);
    try {
      const session = await fetchSession(signal);
      if (!session) {
        router.replace("/login?next=/threads");
        return;
      }
      setAuth(session);
      const query = threadQuery(activeFilter, submittedQuery);
      const [response, teamsResponse] = await Promise.all([
        fetch(`/api/threads?${query.toString()}`, { cache: "no-store", signal }),
        fetch("/api/me/teams", { cache: "no-store", signal })
      ]);
      const [data, teamsData] = await Promise.all([response.json(), teamsResponse.json()]);
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      if (!teamsResponse.ok) throw new Error(teamsData.error ?? `HTTP ${teamsResponse.status}`);
      if (generation !== requestGeneration.current) return;
      setThreads(data.threads ?? []);
      setThreadPage(data.page ?? initialThreadPage);
      setTeams(teamsData.teams ?? []);
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") return;
      if (generation === requestGeneration.current) setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (!signal.aborted && generation === requestGeneration.current) setLoading(false);
    }
  }, [activeFilter, router, submittedQuery]);

  useEffect(() => {
    const controller = new AbortController();
    const generation = requestGeneration.current + 1;
    requestGeneration.current = generation;
    const timeout = window.setTimeout(() => {
      void loadThreads(controller.signal, generation);
    }, 0);
    return () => {
      window.clearTimeout(timeout);
      controller.abort();
    };
  }, [loadThreads]);

  useEffect(() => {
    latestThreadVersion.current = threadVersion(threads[0]);
  }, [threads]);

  const refreshThreadsSilently = useCallback(() => {
    if (refreshInFlight.current) return refreshInFlight.current;
    const generation = requestGeneration.current;
    const query = threadQuery(activeFilter, submittedQuery);
    const refresh = (async () => {
      try {
        const response = await fetch(`/api/threads?${query.toString()}`, { cache: "no-store" });
        if (response.status === 401) {
          router.replace("/login?next=/threads");
          return;
        }
        const data = await response.json();
        if (!response.ok || generation !== requestGeneration.current) return;
        setThreads(data.threads ?? []);
        setThreadPage(data.page ?? initialThreadPage);
      } catch {
        // Auto-refresh is best-effort. Keep the last good inbox visible on transient failures.
      } finally {
        refreshInFlight.current = null;
      }
    })();
    refreshInFlight.current = refresh;
    return refresh;
  }, [activeFilter, router, submittedQuery]);

  const checkLatestThread = useCallback(() => {
    if (latestCheckInFlight.current || refreshInFlight.current) {
      return latestCheckInFlight.current ?? refreshInFlight.current ?? Promise.resolve();
    }
    const generation = requestGeneration.current;
    const query = threadQuery(activeFilter, submittedQuery, undefined, 1);
    const check = (async () => {
      try {
        const response = await fetch(`/api/threads?${query.toString()}`, { cache: "no-store" });
        if (response.status === 401) {
          router.replace("/login?next=/threads");
          return;
        }
        const data = await response.json();
        if (!response.ok || generation !== requestGeneration.current) return;
        if (threadVersion(data.threads?.[0]) !== latestThreadVersion.current) {
          await refreshThreadsSilently();
        }
      } catch {
        // A later poll/focus event will retry without surfacing background network noise.
      } finally {
        latestCheckInFlight.current = null;
      }
    })();
    latestCheckInFlight.current = check;
    return check;
  }, [activeFilter, refreshThreadsSilently, router, submittedQuery]);

  useEffect(() => {
    let pollTimer: number | null = null;

    function stopPolling() {
      if (pollTimer !== null) window.clearInterval(pollTimer);
      pollTimer = null;
    }

    function startPolling() {
      stopPolling();
      if (document.hidden || !document.hasFocus()) return;
      pollTimer = window.setInterval(() => {
        void checkLatestThread();
      }, submittedQuery ? searchedPollIntervalMs : visiblePollIntervalMs);
    }

    function markInactive() {
      if (inactiveSince.current === null) inactiveSince.current = Date.now();
      stopPolling();
    }

    function refreshOnReturn() {
      if (document.hidden || !document.hasFocus()) return;
      const now = Date.now();
      const inactiveFor = inactiveSince.current === null ? 0 : now - inactiveSince.current;
      inactiveSince.current = null;
      startPolling();
      if (now - lastAutoRefreshAt.current < autoRefreshDedupMs) return;
      lastAutoRefreshAt.current = now;
      if (inactiveFor >= fullRefreshAfterInactiveMs) {
        void refreshThreadsSilently();
      } else {
        void checkLatestThread();
      }
    }

    function handleVisibilityChange() {
      if (document.hidden) markInactive();
      else refreshOnReturn();
    }

    function handlePageShow(event: PageTransitionEvent) {
      if (event.persisted) {
        lastAutoRefreshAt.current = Date.now();
        void refreshThreadsSilently();
      }
    }

    document.addEventListener("visibilitychange", handleVisibilityChange);
    window.addEventListener("blur", markInactive);
    window.addEventListener("focus", refreshOnReturn);
    window.addEventListener("online", refreshOnReturn);
    window.addEventListener("pageshow", handlePageShow);
    startPolling();

    return () => {
      stopPolling();
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      window.removeEventListener("blur", markInactive);
      window.removeEventListener("focus", refreshOnReturn);
      window.removeEventListener("online", refreshOnReturn);
      window.removeEventListener("pageshow", handlePageShow);
    };
  }, [checkLatestThread, refreshThreadsSilently, submittedQuery]);

  async function loadMoreThreads() {
    const cursor = threadPage.next_cursor;
    if (!cursor || loadingMore) return;
    const generation = requestGeneration.current;
    setLoadingMore(true);
    setError(null);
    try {
      const query = threadQuery(activeFilter, submittedQuery, cursor);
      const response = await fetch(`/api/threads?${query.toString()}`, { cache: "no-store" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      if (generation !== requestGeneration.current) return;
      setThreads((current) => appendUniqueThreads(current, data.threads ?? []));
      setThreadPage(data.page ?? initialThreadPage);
    } catch (err) {
      if (generation === requestGeneration.current) setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (generation === requestGeneration.current) setLoadingMore(false);
    }
  }

  function selectFilter(filter: InboxFilter) {
    if (filter === activeFilter) return;
    requestGeneration.current += 1;
    setLoadingMore(false);
    setThreads([]);
    setThreadPage(initialThreadPage);
    setActiveFilter(filter);
  }

  function setSearch(nextQuery: string) {
    const next = nextQuery.trim();
    if (next === submittedQuery) return;
    requestGeneration.current += 1;
    setLoadingMore(false);
    setThreads([]);
    setThreadPage(initialThreadPage);
    setSubmittedQuery(next);
  }

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSearch(searchQuery);
  }

  const latestUpdatedAt = useMemo(() => {
    if (threads.length === 0) return null;
    return threads.reduce((latest, thread) => {
      const current = new Date(thread.updated_at).getTime();
      return current > latest ? current : latest;
    }, 0);
  }, [threads]);

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
      <PanelMain>
        <PanelHeader
          title="Threads that belong to you."
          description="Private work you own and every thread shared with one of your teams, in one quiet, searchable-by-context queue."
          actions={
            <Button type="button" onClick={() => setShowCreateComposer((value) => !value)}>
              {showCreateComposer ? <XIcon data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
              {showCreateComposer ? "Close composer" : "Create thread"}
            </Button>
          }
          aside={
            <MetricStrip
              items={[
                { label: "Loaded", value: threads.length, detail: submittedQuery ? `matching “${submittedQuery}”` : "threads in this filter" },
                {
                  label: "Latest activity",
                  value: latestUpdatedAt ? formatDate(new Date(latestUpdatedAt).toISOString()) : "None",
                  detail: auth ? attributionLabel(auth.user_display_name, auth.actor_name) : "Resolving session"
                }
              ]}
            />
          }
        />

        {showCreateComposer ? (
          <section className="grid gap-6" aria-label="Create thread">
            <Card>
              <CardHeader className="border-b">
                <SectionIntro
                  className="border-0 px-0 pb-0"
                  eyebrow="New private thread"
                  title="Name the work before the first message."
                  description="New threads begin private. You can share them with a team or publish a read-only link from the thread later."
                />
              </CardHeader>
              <CardContent>
                <FieldGroup>
                  <Field orientation="responsive">
                    <FieldLabel htmlFor="new-thread-title">Thread title</FieldLabel>
                    <div className="flex min-w-0 flex-1 gap-3">
                      <Input
                        id="new-thread-title"
                        value={newThreadTitle}
                        onChange={(event) => setNewThreadTitle(event.target.value)}
                        placeholder="A precise title for the handoff"
                        type="text"
                      />
                      <Button
                        variant="outline"
                        disabled={creatingEmpty || !newThreadTitle.trim()}
                        type="button"
                        onClick={() => void createThreadOnly()}
                      >
                        {creatingEmpty ? "Creating" : "Create empty"}
                      </Button>
                    </div>
                    <FieldDescription className="sr-only">A title is required before posting the first message.</FieldDescription>
                  </Field>
                </FieldGroup>
              </CardContent>
            </Card>
            <MessageComposer
              canSubmit={Boolean(newThreadTitle.trim())}
              label="First message"
              placeholder="Add the context another agent needs. Markdown is detected automatically."
              submitLabel="Create and post"
              onSubmit={createThreadWithMessage}
            />
            {createError ? (
              <Alert variant="destructive">
                <AlertTitle>Could not create thread</AlertTitle>
                <AlertDescription>{createError}</AlertDescription>
              </Alert>
            ) : null}
          </section>
        ) : null}

        <Card>
          <CardHeader className="border-b">
            <SectionIntro
              className="border-0 px-0 pb-0"
              eyebrow="Browse"
              title="Search and filter accessible threads"
              description="Search titles and message bodies inside the selected visibility filter. Authorization and filtering stay server-side."
            />
          </CardHeader>
          <CardContent className="grid gap-5">
            <Field>
              <FieldLabel htmlFor="thread-search">Search threads</FieldLabel>
              <form className="flex min-w-0 gap-2" onSubmit={submitSearch}>
                <Input
                  id="thread-search"
                  value={searchQuery}
                  onChange={(event) => setSearchQuery(event.target.value)}
                  placeholder="Titles and message bodies"
                  type="search"
                />
                <Button type="submit">
                  <SearchIcon data-icon="inline-start" />
                  Search
                </Button>
                {submittedQuery ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    aria-label="Clear search"
                    onClick={() => {
                      setSearchQuery("");
                      setSearch("");
                    }}
                  >
                    <XIcon />
                  </Button>
                ) : null}
              </form>
              <FieldDescription>Search stays active when you switch between All, Private, Shared with me, Public, and team filters.</FieldDescription>
            </Field>
            <div className="overflow-x-auto pb-1">
              <ToggleGroup
                aria-label="Inbox filters"
                className="min-w-max"
                value={[activeFilter]}
                variant="outline"
                size="lg"
                spacing={0}
                onValueChange={(value) => {
                  const next = value[0] as InboxFilter | undefined;
                  if (next) selectFilter(next);
                }}
              >
                {([
                  ["all", "All"],
                  ["private", "Private"],
                  ["shared", "Shared with me"],
                  ["public", "Public"]
                ] as const).map(([value, label]) => (
                  <ToggleGroupItem value={value} key={value}>{label}</ToggleGroupItem>
                ))}
                {teams.map((team) => (
                  <ToggleGroupItem value={`team:${team.id}`} key={team.id}>{team.name}</ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
          </CardContent>
        </Card>

        <section className="grid gap-5" aria-label="Agentbox threads">
          {loading ? <ThreadListSkeleton /> : null}
          {error ? (
            <Alert variant="destructive">
              <AlertTitle>Could not load inbox</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          {!loading && !error && threads.length === 0 ? (
            <Empty className="border py-16">
              <EmptyHeader>
                <EmptyMedia variant="icon"><InboxIcon /></EmptyMedia>
                <EmptyTitle>{submittedQuery ? "No threads match this search" : "No threads match this filter"}</EmptyTitle>
                <EmptyDescription>
                  {submittedQuery ? "Try another search or visibility filter." : "Choose another visibility filter or create a private thread."}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : null}
          {!loading && !error ? threads.map((thread) => (
            <Link className="group block outline-none" key={thread.id} href={`/threads/${thread.id}`}>
              <Card className="transition-transform group-hover:-translate-y-px group-focus-visible:ring-2 group-focus-visible:ring-ring">
                <CardHeader className="border-b">
                  <div className="flex min-w-0 flex-col gap-3">
                    <div className="flex min-w-0 flex-wrap items-center gap-3">
                      <MonoValue>{thread.id}</MonoValue>
                      <span className="text-sm text-muted-foreground">Updated {formatDate(thread.updated_at)}</span>
                    </div>
                    <h2 className="font-heading text-xl font-semibold tracking-[-0.03em] text-balance sm:text-2xl">
                      {thread.title}
                    </h2>
                  </div>
                  <CardAction>
                    <ArrowUpRightIcon className="text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5" />
                  </CardAction>
                </CardHeader>
                <CardContent className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                  <span className="text-sm/relaxed text-muted-foreground">
                    Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)}
                  </span>
                  <span className="flex flex-wrap gap-2">
                    {visibilityLabels(thread).map((label, index) => (
                      <Badge variant={label === "Public" ? "default" : "outline"} key={`${label}-${index}`}>{label}</Badge>
                    ))}
                  </span>
                </CardContent>
              </Card>
            </Link>
          )) : null}
          {!loading && !error && threadPage.next_cursor ? (
            <div className="flex justify-center pt-5">
              <Button variant="outline" disabled={loadingMore} type="button" onClick={() => void loadMoreThreads()}>
                {loadingMore ? "Loading" : "Load more threads"}
              </Button>
            </div>
          ) : null}
        </section>
      </PanelMain>
  );
}

function ThreadListSkeleton() {
  return (
    <div className="grid gap-5" aria-label="Loading threads" aria-busy="true">
      {Array.from({ length: 4 }).map((_, index) => (
        <Card key={index}>
          <CardHeader className="border-b">
            <div className="flex flex-col gap-3">
              <Skeleton className="h-3 w-48" />
              <Skeleton className="h-6 w-3/5" />
            </div>
          </CardHeader>
          <CardContent className="flex justify-between gap-4">
            <Skeleton className="h-3 w-64" />
            <Skeleton className="h-5 w-24" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
