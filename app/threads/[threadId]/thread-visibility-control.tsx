"use client";

import {
  CheckIcon,
  ClipboardIcon,
  Globe2Icon,
  LockKeyholeIcon,
  RotateCwIcon,
  SaveIcon,
  ShieldAlertIcon,
  Trash2Icon,
  UsersIcon
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput
} from "@/components/ui/input-group";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger
} from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";
import { Spinner } from "@/components/ui/spinner";

type Team = {
  id: string;
  slug: string;
  name: string;
};

type ThreadVisibility = {
  thread_id: string;
  owner_user_id: string;
  shared_teams: Team[];
  available_teams: Team[];
  public: boolean;
  public_link?: ThreadPublicLink;
  public_url?: string;
};

type ThreadPublicLink = {
  thread_id: string;
  token_prefix: string;
  created_at: string;
  updated_at: string;
};

type PublicConfirmation = "rotate" | "revoke";

async function responseJSON(response: Response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data;
}

export function ThreadVisibilityControl({ threadId }: { threadId: string }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [visibility, setVisibility] = useState<ThreadVisibility | null>(null);
  const [myTeams, setMyTeams] = useState<Team[]>([]);
  const [selectedTeamIDs, setSelectedTeamIDs] = useState<string[]>([]);
  const [publicLink, setPublicLink] = useState<ThreadPublicLink | null>(null);
  const [generatedPublicURL, setGeneratedPublicURL] = useState("");
  const [publicBusy, setPublicBusy] = useState<"create" | "rotate" | "revoke" | null>(null);
  const [copied, setCopied] = useState(false);
  const [publicConfirmation, setPublicConfirmation] = useState<PublicConfirmation | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const visibilityResponse = await fetch(`/api/threads/${encodeURIComponent(threadId)}/visibility`, { cache: "no-store" });
      if (visibilityResponse.status === 401 || visibilityResponse.status === 403) {
        router.replace(`/login?next=${encodeURIComponent(`/threads/${threadId}`)}`);
        return;
      }
      if (visibilityResponse.status === 404) {
        router.replace("/threads");
        return;
      }
      const visibilityData = await responseJSON(visibilityResponse);
      const nextVisibility = visibilityData.visibility as ThreadVisibility;
      setVisibility(nextVisibility);
      setMyTeams(nextVisibility.available_teams ?? []);
      setSelectedTeamIDs(nextVisibility.shared_teams.map((team) => team.id));
      setPublicLink(nextVisibility.public_link ?? null);
      setGeneratedPublicURL(nextVisibility.public_url ?? "");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [router, threadId]);

  useEffect(() => {
    const timer = window.setTimeout(() => { void load(); }, 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  const availableTeams = useMemo(() => {
    const byID = new Map<string, Team>();
    for (const team of visibility?.shared_teams ?? []) byID.set(team.id, team);
    for (const team of myTeams) byID.set(team.id, team);
    return [...byID.values()].sort((left, right) => left.name.localeCompare(right.name) || left.slug.localeCompare(right.slug));
  }, [myTeams, visibility]);

  const myTeamIDs = useMemo(() => new Set(myTeams.map((team) => team.id)), [myTeams]);
  const currentTeamIDs = useMemo(() => new Set((visibility?.shared_teams ?? []).map((team) => team.id)), [visibility]);
  const dirty = useMemo(() => {
    const current = [...currentTeamIDs].sort();
    const selected = [...new Set(selectedTeamIDs)].sort();
    return current.length !== selected.length || current.some((id, index) => id !== selected[index]);
  }, [currentTeamIDs, selectedTeamIDs]);

  function toggleTeam(teamID: string) {
    setSelectedTeamIDs((current) => current.includes(teamID)
      ? current.filter((id) => id !== teamID)
      : [...current, teamID]);
  }

  function reset() {
    setSelectedTeamIDs((visibility?.shared_teams ?? []).map((team) => team.id));
    setError(null);
  }

  async function save() {
    setSaving(true);
    setError(null);
    try {
      const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/visibility`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          add_teams: [...new Set(selectedTeamIDs)].filter((id) => !currentTeamIDs.has(id)),
          remove_teams: [...currentTeamIDs].filter((id) => !selectedTeamIDs.includes(id))
        })
      });
      if (response.status === 404) {
        router.replace("/threads");
        return;
      }
      const data = await responseJSON(response);
      const nextVisibility = data.visibility as ThreadVisibility;
      setVisibility(nextVisibility);
      setMyTeams(nextVisibility.available_teams ?? []);
      setSelectedTeamIDs(nextVisibility.shared_teams.map((team) => team.id));
      setPublicLink(nextVisibility.public_link ?? null);
      setGeneratedPublicURL(nextVisibility.public_url ?? "");

      const accessCheck = await fetch(`/api/threads/${encodeURIComponent(threadId)}`, { cache: "no-store" });
      if (accessCheck.status === 404 || accessCheck.status === 403) {
        router.replace("/threads");
        router.refresh();
        return;
      }
      setOpen(false);
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function createPublicLink(rotate: boolean) {
    setPublicBusy(rotate ? "rotate" : "create");
    setError(null);
    setCopied(false);
    try {
      const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/visibility`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(rotate ? { regenerate_public_link: true } : { public: true })
      });
      if (response.status === 404) {
        router.replace("/threads");
        return;
      }
      const data = await responseJSON(response);
      const nextVisibility = data.visibility as ThreadVisibility;
      setVisibility(nextVisibility);
      setMyTeams(nextVisibility.available_teams ?? []);
      setSelectedTeamIDs(nextVisibility.shared_teams.map((team) => team.id));
      setPublicLink(nextVisibility.public_link ?? null);
      setGeneratedPublicURL(nextVisibility.public_url ?? "");
      setPublicConfirmation(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      await load();
    } finally {
      setPublicBusy(null);
    }
  }

  async function revokePublicLink() {
    setPublicBusy("revoke");
    setError(null);
    try {
      const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/visibility`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ public: false })
      });
      if (response.status === 404) {
        router.replace("/threads");
        return;
      }
      const data = await responseJSON(response);
      const nextVisibility = data.visibility as ThreadVisibility;
      setVisibility(nextVisibility);
      setMyTeams(nextVisibility.available_teams ?? []);
      setSelectedTeamIDs(nextVisibility.shared_teams.map((team) => team.id));
      setPublicLink(nextVisibility.public_link ?? null);
      setGeneratedPublicURL(nextVisibility.public_url ?? "");
      setCopied(false);
      setPublicConfirmation(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPublicBusy(null);
    }
  }

  async function copyPublicURL() {
    if (!generatedPublicURL) return;
    await navigator.clipboard.writeText(generatedPublicURL);
    setCopied(true);
  }

  const sharedCount = visibility?.shared_teams.length ?? 0;
  const isPublic = visibility?.public ?? false;
  const teamLabel = sharedCount === 0 ? "" : sharedCount === 1 ? visibility?.shared_teams[0]?.name ?? "1 team" : `${sharedCount} teams`;
  const label = loading ? "Visibility" : isPublic && teamLabel ? `${teamLabel} + public` : isPublic ? "Public" : teamLabel || "Private";
  const privateOnly = selectedTeamIDs.length === 0 && !isPublic;

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger render={<Button variant="outline" />}>
          {isPublic ? <Globe2Icon data-icon="inline-start" /> : sharedCount > 0 ? <UsersIcon data-icon="inline-start" /> : <LockKeyholeIcon data-icon="inline-start" />}
          {label}
        </PopoverTrigger>
        <PopoverContent align="end" className="w-[min(38rem,calc(100vw-2rem))] gap-4 p-4">
          <PopoverHeader>
            <PopoverTitle>Thread visibility</PopoverTitle>
            <PopoverDescription>
              The owner always retains access. Selected teams can read, post, upload, download, and change visibility.
            </PopoverDescription>
          </PopoverHeader>

          {error ? (
            <Alert variant="destructive">
              <ShieldAlertIcon />
              <AlertTitle>Visibility action failed</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <Alert>
            {privateOnly ? <LockKeyholeIcon /> : <UsersIcon />}
            <AlertTitle>{privateOnly ? "Private to the owner" : `${selectedTeamIDs.length} team${selectedTeamIDs.length === 1 ? "" : "s"} selected`}</AlertTitle>
            <AlertDescription>
              {isPublic ? "The public read-only link remains live until it is revoked below." : "Public access is currently off."}
            </AlertDescription>
          </Alert>

          <FieldSet>
            <FieldLegend>Team access</FieldLegend>
            <FieldDescription>Select every team that should participate in this thread.</FieldDescription>
            {loading ? <div className="flex justify-center py-8"><Spinner /></div> : null}
            {!loading && availableTeams.length === 0 ? (
              <Empty className="border py-10">
                <EmptyHeader>
                  <EmptyMedia variant="icon"><UsersIcon /></EmptyMedia>
                  <EmptyTitle>No teams available</EmptyTitle>
                  <EmptyDescription>You do not belong to any teams yet.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : null}
            {!loading && availableTeams.length > 0 ? (
              <FieldGroup className="grid sm:grid-cols-2">
                {availableTeams.map((team) => {
                  const checkboxID = `thread-team-${threadId}-${team.id}`;
                  const currentShare = currentTeamIDs.has(team.id);
                  const callerTeam = myTeamIDs.has(team.id);
                  return (
                    <Field orientation="horizontal" className="border p-3" key={team.id}>
                      <Checkbox
                        id={checkboxID}
                        checked={selectedTeamIDs.includes(team.id)}
                        onCheckedChange={() => toggleTeam(team.id)}
                      />
                      <FieldContent>
                        <FieldLabel htmlFor={checkboxID}>{team.name}</FieldLabel>
                        <FieldDescription>
                          {team.slug} · {currentShare && !callerTeam ? "current share" : currentShare ? "shared" : "your team"}
                        </FieldDescription>
                      </FieldContent>
                    </Field>
                  );
                })}
              </FieldGroup>
            ) : null}
          </FieldSet>

          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="outline" type="button" onClick={reset} disabled={!dirty || saving}>Reset</Button>
            <Button type="button" onClick={() => void save()} disabled={!dirty || saving}>
              {saving ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
              Save team access
            </Button>
          </div>

          <Separator />

          <section className="flex flex-col gap-3" aria-label="Public read-only link">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="flex flex-col gap-1">
                <h3 className="font-heading text-sm font-semibold">Public read-only link</h3>
                <p className="text-xs/relaxed text-muted-foreground">Anyone with the live URL can read the thread and download attachments. They cannot post or change visibility.</p>
              </div>
              <Badge variant={publicLink ? "default" : "outline"}>{publicLink ? "Live" : "Off"}</Badge>
            </div>

            {!publicLink ? (
              <Button type="button" onClick={() => void createPublicLink(false)} disabled={publicBusy !== null || saving}>
                {publicBusy === "create" ? <Spinner data-icon="inline-start" /> : <Globe2Icon data-icon="inline-start" />}
                Create public link
              </Button>
            ) : null}

            {publicLink ? (
              <div className="flex flex-wrap gap-2">
                <Badge variant="secondary">{publicLink.token_prefix}…</Badge>
                <Badge variant="outline">Updated {new Date(publicLink.updated_at).toLocaleString()}</Badge>
              </div>
            ) : null}

            {generatedPublicURL ? (
              <InputGroup>
                <InputGroupInput readOnly value={generatedPublicURL} aria-label="Public thread URL" />
                <InputGroupAddon align="inline-end">
                  <InputGroupButton size="icon-sm" aria-label="Copy public URL" onClick={() => void copyPublicURL()}>
                    {copied ? <CheckIcon /> : <ClipboardIcon />}
                  </InputGroupButton>
                  <InputGroupButton size="sm" variant="outline" render={<a href={generatedPublicURL} target="_blank" rel="noreferrer" />}>
                    Open
                  </InputGroupButton>
                </InputGroupAddon>
              </InputGroup>
            ) : null}

            {publicLink ? (
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" type="button" onClick={() => setPublicConfirmation("rotate")} disabled={publicBusy !== null || saving}>
                  <RotateCwIcon data-icon="inline-start" />
                  Rotate URL
                </Button>
                <Button variant="destructive" type="button" onClick={() => setPublicConfirmation("revoke")} disabled={publicBusy !== null || saving}>
                  <Trash2Icon data-icon="inline-start" />
                  Revoke
                </Button>
              </div>
            ) : null}
          </section>

          <Alert variant="destructive">
            <AlertTitle>Access can change immediately</AlertTitle>
            <AlertDescription>Removing the team that currently grants your access may return you to the inbox after saving.</AlertDescription>
          </Alert>
        </PopoverContent>
      </Popover>

      <AlertDialog open={Boolean(publicConfirmation)} onOpenChange={(open) => { if (!open) setPublicConfirmation(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>{publicConfirmation === "rotate" ? <RotateCwIcon /> : <Trash2Icon />}</AlertDialogMedia>
            <AlertDialogTitle>{publicConfirmation === "rotate" ? "Rotate the public URL?" : "Revoke the public URL?"}</AlertDialogTitle>
            <AlertDialogDescription>
              {publicConfirmation === "rotate"
                ? "The current public URL will stop working immediately and a replacement will be generated."
                : "Anyone using the current public URL will lose access immediately."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={publicBusy !== null}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant={publicConfirmation === "revoke" ? "destructive" : "default"}
              disabled={!publicConfirmation || publicBusy !== null}
              onClick={() => {
                if (publicConfirmation === "rotate") void createPublicLink(true);
                if (publicConfirmation === "revoke") void revokePublicLink();
              }}
            >
              {publicBusy ? <Spinner data-icon="inline-start" /> : publicConfirmation === "rotate" ? <RotateCwIcon data-icon="inline-start" /> : <Trash2Icon data-icon="inline-start" />}
              {publicConfirmation === "rotate" ? "Rotate URL" : "Revoke URL"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
