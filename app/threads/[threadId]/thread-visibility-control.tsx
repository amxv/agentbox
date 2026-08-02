"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import styles from "./thread-visibility-control.module.css";

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

async function responseJSON(response: Response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error ?? `HTTP ${response.status}`);
  }
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
    if (rotate && !window.confirm("Rotate this public link? The current URL will stop working immediately.")) {
      return;
    }
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
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      await load();
    } finally {
      setPublicBusy(null);
    }
  }

  async function revokePublicLink() {
    if (!window.confirm("Revoke this public link? Anyone using the current URL will lose access immediately.")) {
      return;
    }
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
  const label = loading ? "Visibility" : sharedCount === 0 ? "Private" : sharedCount === 1 ? visibility?.shared_teams[0]?.name ?? "1 team" : `${sharedCount} teams`;

  return (
    <div className={styles.root}>
      <button
        className={`${styles.trigger} ${sharedCount > 0 ? styles.shared : ""}`}
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        aria-controls="thread-visibility-panel"
      >
        <span className={styles.triggerIcon}>{sharedCount > 0 ? "◌" : "●"}</span>
        <span><small>Visibility</small><strong>{label}</strong></span>
      </button>

      {open && (
        <section className={styles.panel} id="thread-visibility-panel" aria-label="Thread visibility">
          <div className={styles.heading}>
            <div><p>Thread access</p><h2>Private, plus selected teams.</h2></div>
            <button type="button" onClick={() => { reset(); setOpen(false); }} aria-label="Close visibility control">×</button>
          </div>
          <p className={styles.explainer}>The owner always retains access. Current members of every selected team can read, post, upload, download, and change this visibility.</p>

          {error && <div className={styles.error}>{error}</div>}

          <label className={`${styles.option} ${selectedTeamIDs.length === 0 ? styles.selected : ""}`}>
            <input type="radio" name="thread-visibility-mode" checked={selectedTeamIDs.length === 0} onChange={() => setSelectedTeamIDs([])}/>
            <span><strong>Private</strong><small>Only the owner and their credentials.</small></span>
          </label>

          <div className={styles.teamSection}>
            <div className={styles.sectionLabel}><span>Team access</span><small>{selectedTeamIDs.length} selected</small></div>
            {loading && <p className={styles.empty}>Loading teams…</p>}
            {!loading && availableTeams.length === 0 && <p className={styles.empty}>You do not belong to any teams yet.</p>}
            <div className={styles.teamList}>
              {availableTeams.map((team) => {
                const currentShare = currentTeamIDs.has(team.id);
                const callerTeam = myTeamIDs.has(team.id);
                return (
                  <label className={`${styles.teamOption} ${selectedTeamIDs.includes(team.id) ? styles.selected : ""}`} key={team.id}>
                    <input type="checkbox" checked={selectedTeamIDs.includes(team.id)} onChange={() => toggleTeam(team.id)}/>
                    <span className={styles.teamIdentity}><strong>{team.name}</strong><code>{team.slug}</code></span>
                    <span className={styles.teamContext}>{currentShare && !callerTeam ? "Current share" : currentShare ? "Shared" : "Your team"}</span>
                  </label>
                );
              })}
            </div>
          </div>

          <div className={styles.publicSection}>
            <div className={styles.sectionLabel}><span>Public read-only link</span><small>{publicLink ? "Live" : "Off"}</small></div>
            <p className={styles.publicCopy}>Anyone with the live URL can read this thread and download its attachments. They cannot post, upload, or change visibility.</p>
            {!publicLink && (
              <button className={styles.publicCreate} type="button" onClick={() => void createPublicLink(false)} disabled={publicBusy !== null || saving}>
                {publicBusy === "create" ? "Creating…" : "Create public link"}
              </button>
            )}
            {publicLink && (
              <div className={styles.publicMetadata}>
                <div><span>Credential</span><code>{publicLink.token_prefix}…</code></div>
                <div><span>Updated</span><strong>{new Date(publicLink.updated_at).toLocaleString()}</strong></div>
              </div>
            )}
            {generatedPublicURL && (
              <div className={styles.generatedURL}>
                <div><span>Live URL</span><code>{generatedPublicURL}</code></div>
                <div><button type="button" onClick={() => void copyPublicURL()}>{copied ? "Copied" : "Copy URL"}</button><a href={generatedPublicURL} target="_blank" rel="noreferrer">Open</a></div>
                <p>This URL remains available to authenticated thread participants until it is rotated or revoked.</p>
              </div>
            )}
            {publicLink && (
              <div className={styles.publicActions}>
                <button type="button" onClick={() => void createPublicLink(true)} disabled={publicBusy !== null || saving}>{publicBusy === "rotate" ? "Rotating…" : "Rotate URL"}</button>
                <button type="button" onClick={() => void revokePublicLink()} disabled={publicBusy !== null || saving}>{publicBusy === "revoke" ? "Revoking…" : "Revoke"}</button>
              </div>
            )}
          </div>

          <div className={styles.actions}>
            <button className={styles.secondary} type="button" onClick={reset} disabled={!dirty || saving}>Reset</button>
            <button className={styles.primary} type="button" onClick={() => void save()} disabled={!dirty || saving}>{saving ? "Saving…" : "Save visibility"}</button>
          </div>
          <p className={styles.warning}>Removing the team that currently grants your access may return you to the inbox immediately.</p>
        </section>
      )}
    </div>
  );
}
