"use client";

import { ArrowUpRightIcon, FilesIcon, SearchIcon, ShieldAlertIcon, XIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { attributionLabel } from "../../components/attribution";
import { MetricStrip, MonoValue, PanelHeader, PanelMain } from "../../components/panel-shell";
import { fetchSession } from "../../components/session";

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
  const userOptions = useMemo(
    () => [
      { label: "All users", value: "all" },
      ...users.map((user) => ({
        label: `${user.display_name}${user.disabled_at ? " · disabled" : ""}`,
        value: user.id
      }))
    ],
    [users]
  );
  const teamOptions = useMemo(
    () => [
      { label: "All teams", value: "all" },
      ...teams.map((team) => ({ label: team.name, value: team.id }))
    ],
    [teams]
  );

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
      <PanelMain>
        <Alert>
          <ShieldAlertIcon />
          <AlertTitle>Owner view, read only</AlertTitle>
          <AlertDescription>
            This deployment-wide audit surface bypasses normal thread visibility only for this permanent-owner browser session. It cannot post, upload, or change visibility.
          </AlertDescription>
        </Alert>

        <PanelHeader
          title="Inspect every thread without changing it."
          description="Search private and shared content, filter by owner or team, and review preserved attribution and attachment tombstones."
          aside={
            <MetricStrip
              items={[
                { label: "Matching threads", value: threads.length, detail: submittedQuery ? `Search: ${submittedQuery}` : "Current filter set" },
                { label: "Page position", value: threads.length ? `${page.offset + 1}–${page.offset + threads.length}` : "0", detail: "read-only audit rows" }
              ]}
            />
          }
        />

        <Card>
          <CardContent>
            <FieldGroup className="lg:grid lg:grid-cols-[minmax(18rem,1fr)_16rem_16rem] lg:items-end">
              <Field>
                <FieldLabel htmlFor="owner-content-search">Search all content</FieldLabel>
                <form className="flex min-w-0 gap-2" onSubmit={submitSearch}>
                  <Input
                    id="owner-content-search"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder="Titles and message bodies"
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
                      onClick={() => { setCursor(""); setQuery(""); setSubmittedQuery(""); }}
                    >
                      <XIcon />
                    </Button>
                  ) : null}
                </form>
              </Field>
              <Field>
                <FieldLabel>User</FieldLabel>
                <Select items={userOptions} value={userID || "all"} onValueChange={(value) => { if (value) changeUser(value === "all" ? "" : value); }}>
                  <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {userOptions.map((option) => (
                        <SelectItem value={option.value} key={option.value}>{option.label}</SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel>Team share</FieldLabel>
                <Select items={teamOptions} value={teamRef || "all"} onValueChange={(value) => { if (value) changeTeam(value === "all" ? "" : value); }}>
                  <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {teamOptions.map((option) => (
                        <SelectItem value={option.value} key={option.value}>{option.label}</SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>

        {selectedUser || selectedTeam || submittedQuery ? (
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-[0.65rem] tracking-[0.1em] text-muted-foreground uppercase">Showing</span>
            {submittedQuery ? <Badge variant="secondary">“{submittedQuery}”</Badge> : null}
            {selectedUser ? <Badge variant="outline">{selectedUser.display_name}</Badge> : null}
            {selectedTeam ? <Badge variant="outline">{selectedTeam.name}</Badge> : null}
          </div>
        ) : null}

        {error ? (
          <Alert variant="destructive">
            <AlertTitle>Owner content failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <section className="grid gap-3" aria-label="Deployment content">
          {loading ? <OwnerContentSkeleton /> : null}
          {!loading && !error && threads.length === 0 ? (
            <Empty className="border py-16">
              <EmptyHeader>
                <EmptyMedia variant="icon"><FilesIcon /></EmptyMedia>
                <EmptyTitle>No threads match these filters</EmptyTitle>
                <EmptyDescription>Clear the search or choose a different owner or team.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : null}

          {!loading && !error ? threads.map((thread) => (
            <Link className="group block outline-none" href={`/owner/content/${thread.id}`} key={thread.id}>
              <Card className="transition-transform group-hover:-translate-y-px group-focus-visible:ring-2 group-focus-visible:ring-ring">
                <CardHeader className="border-b">
                  <div className="flex min-w-0 flex-col gap-2">
                    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                      <span>{thread.owner.display_name}{thread.owner.disabled_at ? " · disabled" : ""}</span>
                      <span aria-hidden="true">/</span>
                      <time dateTime={thread.updated_at}>{formatDate(thread.updated_at)}</time>
                    </div>
                    <h2 className="font-heading text-lg font-semibold tracking-[-0.025em] text-balance sm:text-xl">{thread.title}</h2>
                    <MonoValue>{thread.id}</MonoValue>
                  </div>
                  <CardAction>
                    <ArrowUpRightIcon className="text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5" />
                  </CardAction>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                  <p className="line-clamp-3 text-xs/relaxed text-muted-foreground sm:text-sm/relaxed">
                    {thread.matched_snippets?.[0] || thread.last_message_preview || "No messages yet."}
                  </p>
                  <div className="flex flex-col gap-3 border-t pt-3 sm:flex-row sm:items-center sm:justify-between">
                    <span className="text-xs text-muted-foreground">
                      {thread.message_count} message{thread.message_count === 1 ? "" : "s"} · Created by {attributionLabel(thread.created_by_user_display_name, thread.created_by_actor_name, thread.created_by)}
                    </span>
                    <span className="flex flex-wrap gap-1.5">
                      {visibilityLabels(thread.visibility_summary).map((label, index) => (
                        <Badge variant={label === "Public" ? "default" : "outline"} key={`${label}-${index}`}>{label}</Badge>
                      ))}
                    </span>
                  </div>
                </CardContent>
              </Card>
            </Link>
          )) : null}
        </section>

        {!loading && !error && (page.previous_cursor !== undefined || page.next_cursor !== undefined) ? (
          <div className="flex flex-wrap items-center justify-center gap-3 border-t pt-6">
            <Button variant="outline" type="button" disabled={page.previous_cursor === undefined} onClick={() => setCursor(page.previous_cursor ?? "")}>Newer</Button>
            <span className="font-mono text-[0.68rem] text-muted-foreground">Showing {page.offset + 1}–{page.offset + threads.length}</span>
            <Button variant="outline" type="button" disabled={page.next_cursor === undefined} onClick={() => setCursor(page.next_cursor ?? "")}>Older</Button>
          </div>
        ) : null}
      </PanelMain>
  );
}

function OwnerContentSkeleton() {
  return (
    <div className="grid gap-3" aria-label="Loading deployment-wide content" aria-busy="true">
      {Array.from({ length: 3 }).map((_, index) => (
        <Card key={index}>
          <CardHeader className="border-b">
            <div className="flex flex-col gap-3">
              <Skeleton className="h-3 w-52" />
              <Skeleton className="h-6 w-2/3" />
              <Skeleton className="h-3 w-40" />
            </div>
          </CardHeader>
          <CardContent className="grid gap-3">
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-3 w-4/5" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
