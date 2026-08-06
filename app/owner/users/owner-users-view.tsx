"use client";

import {
  CheckIcon,
  ClipboardIcon,
  KeyRoundIcon,
  LinkIcon,
  PlusIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  Trash2Icon,
  UserPlusIcon,
  UsersIcon,
  XIcon
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle
} from "@/components/ui/empty";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput
} from "@/components/ui/input-group";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  MetricStrip,
  MonoValue,
  PanelHeader,
  PanelMain,
} from "../../components/panel-shell";

type User = {
  id: string;
  email: string;
  display_name: string;
  role: string;
  is_owner: boolean;
  created_at: string;
  disabled_at?: string;
};

type Team = {
  id: string;
  slug: string;
  name: string;
  created_at: string;
  updated_at: string;
};

type PageInfo = {
  limit: number;
  offset: number;
  has_more: boolean;
  next_cursor?: string;
  previous_cursor?: string;
};

type TeamWithMembers = Team & {
  members: User[];
  member_count: number;
  members_page: PageInfo;
};

type Credential = {
  id: string;
  user_id: string;
  name: string;
  purpose: string;
  key_masked: string;
  token_prefix: string;
  created_at: string;
  updated_at: string;
  last_used_at?: string;
  revoked_at?: string;
};

type Invitation = {
  id: string;
  created_by_user_id: string;
  created_at: string;
  expires_at: string;
  consumed_at?: string;
  consumed_by_user_id?: string;
  revoked_at?: string;
  teams: Team[];
};

type CreatedInvitation = {
  invitation: Invitation;
  token: string;
  signup_url: string;
};

function invitationStatus(invitation: Invitation) {
  if (invitation.consumed_at) return "Used";
  if (invitation.revoked_at) return "Revoked";
  if (new Date(invitation.expires_at).getTime() <= Date.now()) return "Expired";
  return "Active";
}

function formatDate(value?: string) {
  if (!value) return "Never";
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}

function initials(value: string) {
  return value
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
}

async function responseJSON(response: Response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error ?? `HTTP ${response.status}`);
  }
  return data;
}

const initialPage: PageInfo = { limit: 25, offset: 0, has_more: false };
const invitationExpiryOptions = [
  { label: "1 hour", value: "60" },
  { label: "1 day", value: String(24 * 60) },
  { label: "7 days", value: String(7 * 24 * 60) },
  { label: "30 days", value: String(30 * 24 * 60) }
];

function mergeByID<T extends { id: string }>(current: T[], incoming: T[]) {
  const merged = new Map(current.map((item) => [item.id, item]));
  for (const item of incoming) merged.set(item.id, item);
  return [...merged.values()];
}

export function OwnerUsersView() {
  const router = useRouter();
  const [users, setUsers] = useState<User[]>([]);
  const [usersPage, setUsersPage] = useState<PageInfo>(initialPage);
  const [teams, setTeams] = useState<TeamWithMembers[]>([]);
  const [teamsPage, setTeamsPage] = useState<PageInfo>(initialPage);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [credentialsPage, setCredentialsPage] = useState<PageInfo>(initialPage);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [invitationsPage, setInvitationsPage] = useState<PageInfo>(initialPage);
  const [userTeamsByUser, setUserTeamsByUser] = useState<Record<string, Team[]>>({});
  const [userTeamPages, setUserTeamPages] = useState<Record<string, PageInfo>>({});
  const [created, setCreated] = useState<CreatedInvitation | null>(null);
  const [expiryMinutes, setExpiryMinutes] = useState(7 * 24 * 60);
  const [selectedInvitationTeamIDs, setSelectedInvitationTeamIDs] = useState<string[]>([]);
  const [newTeamName, setNewTeamName] = useState("");
  const [newTeamSlug, setNewTeamSlug] = useState("");
  const [teamNameDrafts, setTeamNameDrafts] = useState<Record<string, string>>({});
  const [memberDrafts, setMemberDrafts] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [purgeTarget, setPurgeTarget] = useState<User | null>(null);

  const activeInvitations = useMemo(
    () => invitations.filter((invitation) => invitationStatus(invitation) === "Active").length,
    [invitations]
  );

  const credentialsByUser = useMemo(() => {
    const result = new Map<string, Credential[]>();
    for (const credential of credentials) {
      const userCredentials = result.get(credential.user_id) ?? [];
      userCredentials.push(credential);
      result.set(credential.user_id, userCredentials);
    }
    return result;
  }, [credentials]);

  const load = useCallback(async () => {
    setError(null);
    try {
      const [usersResponse, invitationsResponse, teamsResponse, credentialsResponse] = await Promise.all([
        fetch("/api/owner/users?limit=25", { cache: "no-store" }),
        fetch("/api/owner/invitations?limit=25", { cache: "no-store" }),
        fetch("/api/owner/teams?limit=25", { cache: "no-store" }),
        fetch("/api/owner/credentials?limit=50", { cache: "no-store" })
      ]);
      if ([usersResponse, invitationsResponse, teamsResponse, credentialsResponse].some((response) => response.status === 401 || response.status === 403)) {
        router.replace("/login?next=/owner/users");
        return;
      }
      const [usersData, invitationsData, teamsData, credentialsData] = await Promise.all([
        responseJSON(usersResponse),
        responseJSON(invitationsResponse),
        responseJSON(teamsResponse),
        responseJSON(credentialsResponse)
      ]);
      const nextTeams = (teamsData.teams ?? []) as TeamWithMembers[];
      setUsers(usersData.users ?? []);
      setUsersPage(usersData.page ?? initialPage);
      setInvitations(invitationsData.invitations ?? []);
      setInvitationsPage(invitationsData.page ?? initialPage);
      setTeams(nextTeams);
      setTeamsPage(teamsData.page ?? initialPage);
      setCredentials(credentialsData.credentials ?? []);
      setCredentialsPage(credentialsData.page ?? initialPage);
      setUserTeamsByUser({});
      setUserTeamPages({});
      setTeamNameDrafts(Object.fromEntries(nextTeams.map((team) => [team.id, team.name])));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [router]);

  async function loadMoreUsers() {
    if (!usersPage.next_cursor) return;
    setBusy("users:more");
    try {
      const data = await responseJSON(await fetch(`/api/owner/users?limit=25&cursor=${encodeURIComponent(usersPage.next_cursor)}`, { cache: "no-store" }));
      setUsers((current) => mergeByID(current, data.users ?? []));
      setUsersPage(data.page ?? initialPage);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function loadMoreTeams() {
    if (!teamsPage.next_cursor) return;
    setBusy("teams:more");
    try {
      const data = await responseJSON(await fetch(`/api/owner/teams?limit=25&cursor=${encodeURIComponent(teamsPage.next_cursor)}`, { cache: "no-store" }));
      const incoming = (data.teams ?? []) as TeamWithMembers[];
      setTeams((current) => mergeByID(current, incoming));
      setTeamsPage(data.page ?? initialPage);
      setTeamNameDrafts((current) => ({ ...current, ...Object.fromEntries(incoming.map((team) => [team.id, team.name])) }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function loadMoreInvitations() {
    if (!invitationsPage.next_cursor) return;
    setBusy("invitations:more");
    try {
      const data = await responseJSON(await fetch(`/api/owner/invitations?limit=25&cursor=${encodeURIComponent(invitationsPage.next_cursor)}`, { cache: "no-store" }));
      setInvitations((current) => mergeByID(current, data.invitations ?? []));
      setInvitationsPage(data.page ?? initialPage);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function loadMoreCredentials() {
    if (!credentialsPage.next_cursor) return;
    setBusy("credentials:more");
    try {
      const data = await responseJSON(await fetch(`/api/owner/credentials?limit=50&cursor=${encodeURIComponent(credentialsPage.next_cursor)}`, { cache: "no-store" }));
      setCredentials((current) => mergeByID(current, data.credentials ?? []));
      setCredentialsPage(data.page ?? initialPage);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function loadMoreTeamMembers(team: TeamWithMembers) {
    if (!team.members_page.next_cursor) return;
    setBusy(`team:members:${team.id}`);
    try {
      const data = await responseJSON(await fetch(`/api/owner/teams/${encodeURIComponent(team.id)}/members?limit=10&cursor=${encodeURIComponent(team.members_page.next_cursor)}`, { cache: "no-store" }));
      setTeams((current) => current.map((candidate) => candidate.id === team.id
        ? { ...candidate, members: mergeByID(candidate.members, data.members ?? []), members_page: data.page ?? initialPage }
        : candidate));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function loadUserTeams(userID: string, cursor = "") {
    setBusy(`user:teams:${userID}`);
    try {
      const suffix = cursor ? `&cursor=${encodeURIComponent(cursor)}` : "";
      const data = await responseJSON(await fetch(`/api/owner/users/${encodeURIComponent(userID)}/teams?limit=10${suffix}`, { cache: "no-store" }));
      setUserTeamsByUser((current) => ({ ...current, [userID]: mergeByID(cursor ? current[userID] ?? [] : [], data.teams ?? []) }));
      setUserTeamPages((current) => ({ ...current, [userID]: data.page ?? initialPage }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(() => { void load(); }, 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  async function createInvitation() {
    setBusy("invite:create");
    setError(null);
    setCopied(false);
    try {
      const response = await fetch("/api/owner/invitations", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          expires_in_minutes: expiryMinutes,
          team_ids: selectedInvitationTeamIDs
        })
      });
      const data = await responseJSON(response);
      const signupURL = typeof data.signup_url === "string" && data.signup_url.startsWith("/")
        ? `${window.location.origin}${data.signup_url}`
        : data.signup_url;
      setCreated({ ...data, signup_url: signupURL });
      setSelectedInvitationTeamIDs([]);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function revokeInvitation(id: string) {
    setBusy(`invite:${id}`);
    setError(null);
    try {
      await responseJSON(await fetch(`/api/owner/invitations/${encodeURIComponent(id)}`, { method: "DELETE" }));
      if (created?.invitation.id === id) setCreated(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function setDisabled(user: User, disabled: boolean) {
    setBusy(`user:${user.id}`);
    setError(null);
    try {
      const action = disabled ? "disable" : "enable";
      await responseJSON(await fetch(`/api/owner/users/${encodeURIComponent(user.id)}/${action}`, { method: "POST" }));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function purgeAttachments(user: User) {
    setBusy(`purge:${user.id}`);
    setError(null);
    setNotice(null);
    try {
      const data = await responseJSON(await fetch(`/api/owner/users/${encodeURIComponent(user.id)}/purge-attachments`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ limit: 50 })
      }));
      const purge = data.purge as { purged: number; failed: number; remaining: number; complete: boolean };
      setNotice(`Purged ${purge.purged} attachment${purge.purged === 1 ? "" : "s"}. ${purge.failed} failed; ${purge.remaining} remain.${purge.complete ? " Purge complete." : " Run the purge again to continue or retry failures."}`);
      setPurgeTarget(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function revokeCredential(credential: Credential) {
    setBusy(`credential:${credential.id}`);
    setError(null);
    try {
      await responseJSON(await fetch(`/api/owner/credentials/${encodeURIComponent(credential.id)}`, { method: "DELETE" }));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function createTeam() {
    setBusy("team:create");
    setError(null);
    try {
      await responseJSON(await fetch("/api/owner/teams", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ slug: newTeamSlug, name: newTeamName })
      }));
      setNewTeamName("");
      setNewTeamSlug("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function renameTeam(team: Team) {
    setBusy(`team:rename:${team.id}`);
    setError(null);
    try {
      await responseJSON(await fetch(`/api/owner/teams/${encodeURIComponent(team.id)}`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ name: teamNameDrafts[team.id] ?? team.name })
      }));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function addMember(team: TeamWithMembers, userID: string) {
    if (!userID) return;
    setBusy(`team:add:${team.id}`);
    setError(null);
    try {
      await responseJSON(await fetch(
        `/api/owner/teams/${encodeURIComponent(team.id)}/members/${encodeURIComponent(userID)}`,
        { method: "PUT" }
      ));
      setMemberDrafts((current) => ({ ...current, [team.id]: "" }));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function removeMember(teamID: string, userID: string) {
    setBusy(`team:remove:${teamID}:${userID}`);
    setError(null);
    try {
      await responseJSON(await fetch(
        `/api/owner/teams/${encodeURIComponent(teamID)}/members/${encodeURIComponent(userID)}`,
        { method: "DELETE" }
      ));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  function toggleInvitationTeam(teamID: string) {
    setSelectedInvitationTeamIDs((current) => current.includes(teamID)
      ? current.filter((id) => id !== teamID)
      : [...current, teamID]);
  }

  async function copySignupURL() {
    if (!created?.signup_url) return;
    await navigator.clipboard.writeText(created.signup_url);
    setCopied(true);
  }

  return (
    <>
      <PanelMain>
        <PanelHeader
          title="Users, teams, and invitations."
          description="Manage deployment-wide identity without collapsing actor attribution. Teams overlap freely, while every thread remains private until it is explicitly shared."
          aside={
            <MetricStrip
              items={[
                { label: "Loaded users", value: users.length },
                { label: "Loaded teams", value: teams.length },
                { label: "Active credentials", value: credentials.filter((credential) => !credential.revoked_at).length },
                { label: "Active invitations", value: activeInvitations }
              ]}
            />
          }
        />

        {error ? (
          <Alert variant="destructive">
            <AlertTitle>Owner action failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        {notice ? (
          <Alert>
            <ShieldCheckIcon />
            <AlertTitle>Attachment purge updated</AlertTitle>
            <AlertDescription>{notice}</AlertDescription>
          </Alert>
        ) : null}

        <Tabs defaultValue="users" className="gap-6">
          <TabsList variant="line" className="w-full justify-start overflow-x-auto border-b">
            <TabsTrigger value="users">
              <UsersIcon data-icon="inline-start" />
              Users
            </TabsTrigger>
            <TabsTrigger value="teams">
              <UsersIcon data-icon="inline-start" />
              Teams
            </TabsTrigger>
            <TabsTrigger value="invitations">
              <LinkIcon data-icon="inline-start" />
              Invitations
            </TabsTrigger>
          </TabsList>

          <TabsContent value="users" className="flex flex-col gap-6">
            <Card>
              <CardHeader className="border-b">
                <CardTitle>Deployment users</CardTitle>
                <CardDescription>
                  Review account state, team membership, and every user-owned credential without losing attribution history.
                </CardDescription>
                <CardAction className="flex flex-wrap gap-2">
                  <Button variant="outline" onClick={() => void load()} disabled={loading}>
                    {loading ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
                    Refresh
                  </Button>
                  {credentialsPage.next_cursor ? (
                    <Button variant="outline" onClick={() => void loadMoreCredentials()} disabled={busy === "credentials:more"}>
                      {busy === "credentials:more" ? <Spinner data-icon="inline-start" /> : <KeyRoundIcon data-icon="inline-start" />}
                      Load credentials
                    </Button>
                  ) : null}
                </CardAction>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                {loading ? <UserListSkeleton /> : null}
                {!loading && users.length === 0 ? (
                  <Empty className="border py-14">
                    <EmptyHeader>
                      <EmptyMedia variant="icon"><UsersIcon /></EmptyMedia>
                      <EmptyTitle>No users found</EmptyTitle>
                      <EmptyDescription>Create an invitation to add the first non-owner account.</EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                ) : null}
                {!loading ? users.map((user) => {
                  const userTeams = userTeamsByUser[user.id];
                  const userTeamsPage = userTeamPages[user.id];
                  const userCredentials = credentialsByUser.get(user.id) ?? [];
                  const activeUserCredentials = userCredentials.filter((credential) => !credential.revoked_at).length;
                  return (
                    <Card size="sm" key={user.id}>
                      <CardHeader className="border-b">
                        <div className="flex min-w-0 items-start gap-3">
                          <Avatar size="lg">
                            <AvatarFallback>{initials(user.display_name)}</AvatarFallback>
                          </Avatar>
                          <div className="flex min-w-0 flex-col gap-1">
                            <CardTitle className="flex flex-wrap items-center gap-2">
                              {user.display_name}
                              {user.is_owner ? <Badge>Owner</Badge> : null}
                              {user.disabled_at ? <Badge variant="destructive">Disabled</Badge> : <Badge variant="outline">Active</Badge>}
                            </CardTitle>
                            <CardDescription>{user.email}</CardDescription>
                          </div>
                        </div>
                        <CardAction className="flex flex-wrap gap-2">
                          {user.is_owner ? (
                            <Badge variant="secondary">Protected</Badge>
                          ) : (
                            <>
                              <Button
                                variant="outline"
                                disabled={busy === `user:${user.id}`}
                                onClick={() => void setDisabled(user, !user.disabled_at)}
                              >
                                {busy === `user:${user.id}` ? <Spinner data-icon="inline-start" /> : null}
                                {user.disabled_at ? "Enable" : "Disable"}
                              </Button>
                              {user.disabled_at ? (
                                <Button variant="destructive" onClick={() => setPurgeTarget(user)} disabled={busy === `purge:${user.id}`}>
                                  <Trash2Icon data-icon="inline-start" />
                                  Purge attachments
                                </Button>
                              ) : null}
                            </>
                          )}
                        </CardAction>
                      </CardHeader>
                      <CardContent className="flex flex-col gap-5">
                        <section className="flex flex-col gap-2" aria-label={`${user.display_name} teams`}>
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <span className="font-mono text-[0.65rem] tracking-[0.1em] text-muted-foreground uppercase">Teams</span>
                            {userTeams === undefined ? (
                              <Button
                                size="xs"
                                variant="ghost"
                                disabled={busy === `user:teams:${user.id}`}
                                onClick={() => void loadUserTeams(user.id)}
                              >
                                {busy === `user:teams:${user.id}` ? <Spinner data-icon="inline-start" /> : null}
                                View teams
                              </Button>
                            ) : null}
                          </div>
                          <div className="flex flex-wrap gap-1.5">
                            {userTeams === undefined ? <Badge variant="outline">Not loaded</Badge> : null}
                            {userTeams?.length === 0 ? <Badge variant="secondary">No teams</Badge> : null}
                            {userTeams?.map((team) => <Badge variant="outline" key={team.id}>{team.name}</Badge>)}
                            {userTeamsPage?.next_cursor ? (
                              <Button
                                size="xs"
                                variant="ghost"
                                disabled={busy === `user:teams:${user.id}`}
                                onClick={() => void loadUserTeams(user.id, userTeamsPage.next_cursor)}
                              >
                                {busy === `user:teams:${user.id}` ? <Spinner data-icon="inline-start" /> : null}
                                More teams
                              </Button>
                            ) : null}
                          </div>
                        </section>

                        <Separator />

                        <section className="flex flex-col gap-3" aria-label={`${user.display_name} credentials`}>
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <span className="font-mono text-[0.65rem] tracking-[0.1em] text-muted-foreground uppercase">Credentials</span>
                            <Badge variant="secondary">{activeUserCredentials} active</Badge>
                          </div>
                          {userCredentials.length === 0 ? (
                            <Empty className="border py-8">
                              <EmptyHeader>
                                <EmptyMedia variant="icon"><KeyRoundIcon /></EmptyMedia>
                                <EmptyTitle>No credentials created</EmptyTitle>
                                <EmptyDescription>This user has no API, MCP, CLI, or Raycast credentials yet.</EmptyDescription>
                              </EmptyHeader>
                            </Empty>
                          ) : (
                            <Table>
                              <TableHeader>
                                <TableRow>
                                  <TableHead>Name</TableHead>
                                  <TableHead>Purpose</TableHead>
                                  <TableHead>Token</TableHead>
                                  <TableHead>Last used</TableHead>
                                  <TableHead className="text-right">Action</TableHead>
                                </TableRow>
                              </TableHeader>
                              <TableBody>
                                {userCredentials.map((credential) => (
                                  <TableRow key={credential.id}>
                                    <TableCell className="whitespace-normal">
                                      <div className="flex min-w-48 flex-col gap-1">
                                        <span className="font-medium">{credential.name}</span>
                                        <MonoValue>{credential.id}</MonoValue>
                                      </div>
                                    </TableCell>
                                    <TableCell><Badge variant="outline">{credential.purpose}</Badge></TableCell>
                                    <TableCell><MonoValue>{credential.key_masked || `${credential.token_prefix}…`}</MonoValue></TableCell>
                                    <TableCell>{formatDate(credential.last_used_at)}</TableCell>
                                    <TableCell className="text-right">
                                      {credential.revoked_at ? (
                                        <Badge variant="destructive">Revoked</Badge>
                                      ) : (
                                        <Button
                                          variant="outline"
                                          size="xs"
                                          disabled={busy === `credential:${credential.id}`}
                                          onClick={() => void revokeCredential(credential)}
                                        >
                                          {busy === `credential:${credential.id}` ? <Spinner data-icon="inline-start" /> : null}
                                          Revoke
                                        </Button>
                                      )}
                                    </TableCell>
                                  </TableRow>
                                ))}
                              </TableBody>
                            </Table>
                          )}
                        </section>
                      </CardContent>
                      <CardFooter className="flex flex-wrap justify-between gap-3">
                        <MonoValue>{user.id}</MonoValue>
                        <span className="text-xs text-muted-foreground">Created {formatDate(user.created_at)}</span>
                      </CardFooter>
                    </Card>
                  );
                }) : null}
              </CardContent>
              {usersPage.next_cursor ? (
                <CardFooter>
                  <Button variant="outline" disabled={busy === "users:more"} onClick={() => void loadMoreUsers()}>
                    {busy === "users:more" ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                    Load more users
                  </Button>
                </CardFooter>
              ) : null}
            </Card>
          </TabsContent>

          <TabsContent value="teams" className="flex flex-col gap-6">
            <Card>
              <CardHeader className="border-b">
                <CardTitle>Create a team</CardTitle>
                <CardDescription>Use a stable slug and a reader-friendly display name. Users may belong to any number of teams.</CardDescription>
              </CardHeader>
              <CardContent>
                <FieldGroup className="md:grid md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] md:items-end">
                  <Field>
                    <FieldLabel htmlFor="new-team-name">Name</FieldLabel>
                    <Input id="new-team-name" value={newTeamName} onChange={(event) => setNewTeamName(event.target.value)} placeholder="Product Engineering" />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="new-team-slug">Slug</FieldLabel>
                    <Input id="new-team-slug" value={newTeamSlug} onChange={(event) => setNewTeamSlug(event.target.value.toLowerCase())} placeholder="product-engineering" />
                  </Field>
                  <Button type="button" onClick={() => void createTeam()} disabled={busy === "team:create" || !newTeamName.trim() || !newTeamSlug.trim()}>
                    {busy === "team:create" ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                    Create team
                  </Button>
                </FieldGroup>
              </CardContent>
            </Card>

            {!loading && teams.length === 0 ? (
              <Empty className="border py-16">
                <EmptyHeader>
                  <EmptyMedia variant="icon"><UsersIcon /></EmptyMedia>
                  <EmptyTitle>No teams yet</EmptyTitle>
                  <EmptyDescription>Users can remain teamless indefinitely, or you can create the first shared workspace above.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : null}

            <section className="grid gap-4 xl:grid-cols-2" aria-label="Teams">
              {loading ? Array.from({ length: 2 }).map((_, index) => <Skeleton className="h-96" key={index} />) : null}
              {!loading ? teams.map((team) => {
                const memberIDs = new Set(team.members.map((member) => member.id));
                const availableUsers = users.filter((user) => !user.disabled_at && !memberIDs.has(user.id));
                const memberOptions = availableUsers.map((user) => ({ label: user.display_name, value: user.id }));
                const selectedUserID = memberDrafts[team.id] || memberOptions[0]?.value || "";
                return (
                  <Card key={team.id}>
                    <CardHeader className="border-b">
                      <div className="flex min-w-0 flex-col gap-1">
                        <CardTitle>{team.name}</CardTitle>
                        <CardDescription>{team.slug}</CardDescription>
                      </div>
                      <CardAction><Badge variant="secondary">{team.member_count} {team.member_count === 1 ? "member" : "members"}</Badge></CardAction>
                    </CardHeader>
                    <CardContent className="flex flex-col gap-5">
                      <Field>
                        <FieldLabel htmlFor={`team-name-${team.id}`}>Display name</FieldLabel>
                        <InputGroup>
                          <InputGroupInput
                            id={`team-name-${team.id}`}
                            value={teamNameDrafts[team.id] ?? team.name}
                            onChange={(event) => setTeamNameDrafts((current) => ({ ...current, [team.id]: event.target.value }))}
                          />
                          <InputGroupAddon align="inline-end">
                            <InputGroupButton
                              variant="outline"
                              size="sm"
                              disabled={busy === `team:rename:${team.id}` || !(teamNameDrafts[team.id] ?? "").trim() || teamNameDrafts[team.id] === team.name}
                              onClick={() => void renameTeam(team)}
                            >
                              {busy === `team:rename:${team.id}` ? <Spinner data-icon="inline-start" /> : <CheckIcon data-icon="inline-start" />}
                              Save name
                            </InputGroupButton>
                          </InputGroupAddon>
                        </InputGroup>
                      </Field>

                      <FieldGroup className="sm:grid sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
                        <Field data-disabled={availableUsers.length === 0}>
                          <FieldLabel>Add a member</FieldLabel>
                          <Select
                            items={memberOptions}
                            value={selectedUserID || null}
                            disabled={availableUsers.length === 0}
                            onValueChange={(value) => {
                              if (value) setMemberDrafts((current) => ({ ...current, [team.id]: value }));
                            }}
                          >
                            <SelectTrigger className="w-full"><SelectValue placeholder={availableUsers.length === 0 ? "Everyone is a member" : "Choose a user"} /></SelectTrigger>
                            <SelectContent>
                              <SelectGroup>
                                {memberOptions.map((option) => <SelectItem value={option.value} key={option.value}>{option.label}</SelectItem>)}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </Field>
                        <Button type="button" onClick={() => void addMember(team, selectedUserID)} disabled={!selectedUserID || busy === `team:add:${team.id}`}>
                          {busy === `team:add:${team.id}` ? <Spinner data-icon="inline-start" /> : <UserPlusIcon data-icon="inline-start" />}
                          Add
                        </Button>
                      </FieldGroup>

                      <section className="flex flex-col gap-3" aria-label={`${team.name} members`}>
                        <div className="flex items-center justify-between gap-2">
                          <span className="font-mono text-[0.65rem] tracking-[0.1em] text-muted-foreground uppercase">Members</span>
                          <Badge variant="outline">{team.members.length} loaded</Badge>
                        </div>
                        {team.members.length === 0 ? (
                          <Empty className="border py-8">
                            <EmptyHeader>
                              <EmptyMedia variant="icon"><UsersIcon /></EmptyMedia>
                              <EmptyTitle>No members</EmptyTitle>
                              <EmptyDescription>Add any active deployment user to this team.</EmptyDescription>
                            </EmptyHeader>
                          </Empty>
                        ) : (
                          <Table>
                            <TableHeader>
                              <TableRow>
                                <TableHead>User</TableHead>
                                <TableHead>Email</TableHead>
                                <TableHead className="text-right">Remove</TableHead>
                              </TableRow>
                            </TableHeader>
                            <TableBody>
                              {team.members.map((member) => (
                                <TableRow key={member.id}>
                                  <TableCell className="font-medium">{member.display_name}</TableCell>
                                  <TableCell>{member.email}</TableCell>
                                  <TableCell className="text-right">
                                    <Button
                                      type="button"
                                      size="icon-xs"
                                      variant="ghost"
                                      aria-label={`Remove ${member.display_name} from ${team.name}`}
                                      disabled={busy === `team:remove:${team.id}:${member.id}`}
                                      onClick={() => void removeMember(team.id, member.id)}
                                    >
                                      {busy === `team:remove:${team.id}:${member.id}` ? <Spinner /> : <XIcon />}
                                    </Button>
                                  </TableCell>
                                </TableRow>
                              ))}
                            </TableBody>
                          </Table>
                        )}
                        {team.members_page.next_cursor ? (
                          <Button variant="outline" disabled={busy === `team:members:${team.id}`} onClick={() => void loadMoreTeamMembers(team)}>
                            {busy === `team:members:${team.id}` ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                            Load more members ({team.members.length}/{team.member_count})
                          </Button>
                        ) : null}
                      </section>
                    </CardContent>
                    <CardFooter className="flex flex-wrap justify-between gap-3">
                      <MonoValue>{team.id}</MonoValue>
                      <span className="text-xs text-muted-foreground">Updated {formatDate(team.updated_at)}</span>
                    </CardFooter>
                  </Card>
                );
              }) : null}
            </section>

            {teamsPage.next_cursor ? (
              <div className="flex justify-center">
                <Button variant="outline" disabled={busy === "teams:more"} onClick={() => void loadMoreTeams()}>
                  {busy === "teams:more" ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                  Load more teams
                </Button>
              </div>
            ) : null}
          </TabsContent>

          <TabsContent value="invitations" className="grid gap-6 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
            <Card>
              <CardHeader className="border-b">
                <CardTitle>Create a one-time signup link</CardTitle>
                <CardDescription>
                  Choose zero or more initial teams. The account, browser session, memberships, and invitation consumption commit together.
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-5">
                <Field>
                  <FieldLabel>Expires</FieldLabel>
                  <Select
                    items={invitationExpiryOptions}
                    value={String(expiryMinutes)}
                    onValueChange={(value) => { if (value) setExpiryMinutes(Number(value)); }}
                  >
                    <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {invitationExpiryOptions.map((option) => <SelectItem value={option.value} key={option.value}>{option.label}</SelectItem>)}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>

                <FieldSet>
                  <FieldLegend>Initial teams</FieldLegend>
                  <FieldDescription>
                    {selectedInvitationTeamIDs.length === 0 ? "No team access will be granted." : `${selectedInvitationTeamIDs.length} team${selectedInvitationTeamIDs.length === 1 ? "" : "s"} selected.`}
                  </FieldDescription>
                  {teams.length === 0 ? (
                    <Empty className="border py-8">
                      <EmptyHeader>
                        <EmptyMedia variant="icon"><UsersIcon /></EmptyMedia>
                        <EmptyTitle>No teams yet</EmptyTitle>
                        <EmptyDescription>This invitation will create a user with no initial team memberships.</EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  ) : (
                    <FieldGroup className="sm:grid sm:grid-cols-2">
                      {teams.map((team) => {
                        const checkboxID = `invitation-team-${team.id}`;
                        return (
                          <Field orientation="horizontal" className="border p-3" key={team.id}>
                            <Checkbox
                              id={checkboxID}
                              checked={selectedInvitationTeamIDs.includes(team.id)}
                              onCheckedChange={() => toggleInvitationTeam(team.id)}
                            />
                            <FieldContent>
                              <FieldLabel htmlFor={checkboxID}>{team.name}</FieldLabel>
                              <FieldDescription>{team.slug}</FieldDescription>
                            </FieldContent>
                          </Field>
                        );
                      })}
                    </FieldGroup>
                  )}
                </FieldSet>

                <Button type="button" onClick={() => void createInvitation()} disabled={busy === "invite:create"}>
                  {busy === "invite:create" ? <Spinner data-icon="inline-start" /> : <UserPlusIcon data-icon="inline-start" />}
                  Create invitation
                </Button>

                {created ? (
                  <Alert>
                    <LinkIcon />
                    <AlertTitle>One-time signup URL</AlertTitle>
                    <AlertDescription className="flex flex-col gap-3">
                      <InputGroup>
                        <InputGroupInput readOnly value={created.signup_url} aria-label="One-time signup URL" />
                        <InputGroupAddon align="inline-end">
                          <InputGroupButton size="icon-sm" aria-label="Copy signup URL" onClick={() => void copySignupURL()}>
                            {copied ? <CheckIcon /> : <ClipboardIcon />}
                          </InputGroupButton>
                        </InputGroupAddon>
                      </InputGroup>
                      <span className="flex flex-wrap gap-1.5">
                        {created.invitation.teams.length === 0 ? <Badge variant="secondary">No initial teams</Badge> : created.invitation.teams.map((team) => <Badge variant="outline" key={team.id}>{team.name}</Badge>)}
                      </span>
                    </AlertDescription>
                  </Alert>
                ) : null}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="border-b">
                <CardTitle>Invitation history</CardTitle>
                <CardDescription>Review active, consumed, revoked, and expired links without redisplaying their secrets.</CardDescription>
              </CardHeader>
              <CardContent>
                {!loading && invitations.length === 0 ? (
                  <Empty className="border py-14">
                    <EmptyHeader>
                      <EmptyMedia variant="icon"><LinkIcon /></EmptyMedia>
                      <EmptyTitle>No invitations yet</EmptyTitle>
                      <EmptyDescription>Create the first one-time signup link from this page.</EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Status</TableHead>
                        <TableHead>Expires</TableHead>
                        <TableHead>Initial teams</TableHead>
                        <TableHead className="text-right">Action</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {invitations.map((invitation) => {
                        const status = invitationStatus(invitation);
                        return (
                          <TableRow key={invitation.id}>
                            <TableCell className="whitespace-normal">
                              <div className="flex min-w-40 flex-col gap-1">
                                <Badge variant={status === "Active" ? "default" : status === "Revoked" ? "destructive" : "outline"}>{status}</Badge>
                                <MonoValue>{invitation.id}</MonoValue>
                              </div>
                            </TableCell>
                            <TableCell>{formatDate(invitation.expires_at)}</TableCell>
                            <TableCell>
                              <div className="flex min-w-44 flex-wrap gap-1.5">
                                {invitation.teams.length === 0 ? <Badge variant="secondary">None</Badge> : invitation.teams.map((team) => <Badge variant="outline" key={team.id}>{team.name}</Badge>)}
                              </div>
                            </TableCell>
                            <TableCell className="text-right">
                              {status === "Active" ? (
                                <Button variant="outline" size="xs" disabled={busy === `invite:${invitation.id}`} onClick={() => void revokeInvitation(invitation.id)}>
                                  {busy === `invite:${invitation.id}` ? <Spinner data-icon="inline-start" /> : null}
                                  Revoke
                                </Button>
                              ) : null}
                            </TableCell>
                          </TableRow>
                        );
                      })}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
              {invitationsPage.next_cursor ? (
                <CardFooter>
                  <Button variant="outline" disabled={busy === "invitations:more"} onClick={() => void loadMoreInvitations()}>
                    {busy === "invitations:more" ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                    Load more invitations
                  </Button>
                </CardFooter>
              ) : null}
            </Card>
          </TabsContent>
        </Tabs>
      </PanelMain>

      <AlertDialog open={Boolean(purgeTarget)} onOpenChange={(open) => { if (!open) setPurgeTarget(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia><Trash2Icon /></AlertDialogMedia>
            <AlertDialogTitle>Permanently purge attachments?</AlertDialogTitle>
            <AlertDialogDescription>
              Every attachment uploaded by {purgeTarget?.display_name ?? "this user"} will be deleted. Thread and message tombstones remain, and this action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={Boolean(purgeTarget && busy === `purge:${purgeTarget.id}`)}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={!purgeTarget || busy === `purge:${purgeTarget.id}`}
              onClick={() => { if (purgeTarget) void purgeAttachments(purgeTarget); }}
            >
              {purgeTarget && busy === `purge:${purgeTarget.id}` ? <Spinner data-icon="inline-start" /> : <Trash2Icon data-icon="inline-start" />}
              Purge attachments
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function UserListSkeleton() {
  return (
    <div className="flex flex-col gap-3" aria-label="Loading users" aria-busy="true">
      {Array.from({ length: 3 }).map((_, index) => (
        <Card size="sm" key={index}>
          <CardHeader className="border-b">
            <div className="flex items-center gap-3">
              <Skeleton className="size-10 rounded-full" />
              <div className="flex flex-col gap-2">
                <Skeleton className="h-4 w-36" />
                <Skeleton className="h-3 w-52" />
              </div>
            </div>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-24 w-full" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
