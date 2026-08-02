"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { AgentboxMark } from "../../components/agentbox-mark";
import { ThemeSwitcher } from "../../components/theme-switcher";
import styles from "./owner-users.module.css";

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

type TeamWithMembers = Team & {
  members: User[];
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

async function responseJSON(response: Response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error ?? `HTTP ${response.status}`);
  }
  return data;
}

export function OwnerUsersView() {
  const router = useRouter();
  const [users, setUsers] = useState<User[]>([]);
  const [teams, setTeams] = useState<TeamWithMembers[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
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

  const activeInvitations = useMemo(
    () => invitations.filter((invitation) => invitationStatus(invitation) === "Active").length,
    [invitations]
  );

  const teamsByUser = useMemo(() => {
    const result = new Map<string, Team[]>();
    for (const team of teams) {
      for (const member of team.members) {
        const memberTeams = result.get(member.id) ?? [];
        memberTeams.push(team);
        result.set(member.id, memberTeams);
      }
    }
    return result;
  }, [teams]);

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
        fetch("/api/owner/users", { cache: "no-store" }),
        fetch("/api/owner/invitations", { cache: "no-store" }),
        fetch("/api/owner/teams", { cache: "no-store" }),
        fetch("/api/owner/credentials", { cache: "no-store" })
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
      setInvitations(invitationsData.invitations ?? []);
      setTeams(nextTeams);
      setCredentials(credentialsData.credentials ?? []);
      setTeamNameDrafts(Object.fromEntries(nextTeams.map((team) => [team.id, team.name])));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [router]);

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
    if (!window.confirm(`Permanently delete every attachment uploaded by ${user.display_name}? Thread and message tombstones will remain.`)) return;
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
    <div className={styles.page}>
      <header className={styles.header}>
        <Link className={styles.brand} href="/threads"><AgentboxMark className={styles.mark}/><span>Agentbox</span><small>Owner</small></Link>
        <nav className={styles.nav}><Link href="/owner/content">Content</Link><Link href="/threads">Inbox</Link><Link href="/keys">Credentials</Link><ThemeSwitcher/></nav>
      </header>
      <main className={styles.main}>
        <section className={styles.hero}>
          <div><p className={styles.eyebrow}>Deployment administration</p><h1>Users, teams, invitations.</h1><p>Manage deployment-wide identity without collapsing actor attribution. Teams overlap freely, while every thread remains private until it is explicitly shared.</p></div>
          <div className={styles.metrics}><div><span>Users</span><b>{users.length}</b></div><div><span>Teams</span><b>{teams.length}</b></div><div><span>Active credentials</span><b>{credentials.filter((credential) => !credential.revoked_at).length}</b></div><div><span>Active invitations</span><b>{activeInvitations}</b></div></div>
        </section>

        {error && <div className={styles.error}><strong>Owner action failed.</strong><span>{error}</span></div>}
        {notice && <div className={styles.notice}><strong>Attachment purge updated.</strong><span>{notice}</span></div>}

        <section className={styles.invitePanel}>
          <div><p className={styles.sectionLabel}>Invite a user</p><h2>Create a one-time signup link.</h2><p>Choose zero or more initial teams. The account, browser session, memberships, and invitation consumption commit together.</p></div>
          <div className={styles.inviteControls}>
            <label>Expires<select value={expiryMinutes} onChange={(event)=>setExpiryMinutes(Number(event.target.value))}><option value={60}>1 hour</option><option value={24*60}>1 day</option><option value={7*24*60}>7 days</option><option value={30*24*60}>30 days</option></select></label>
            <button type="button" onClick={createInvitation} disabled={busy === "invite:create"}>{busy === "invite:create" ? "Creating…" : "Create invitation"}</button>
          </div>
          <div className={styles.teamPicker}>
            <div><span>Initial teams</span><small>{selectedInvitationTeamIDs.length === 0 ? "No team access" : `${selectedInvitationTeamIDs.length} selected`}</small></div>
            <div className={styles.teamOptions}>
              {teams.length === 0 && <p>No teams yet. This invitation will create a zero-team user.</p>}
              {teams.map((team) => <label className={styles.teamOption} key={team.id}><input type="checkbox" checked={selectedInvitationTeamIDs.includes(team.id)} onChange={()=>toggleInvitationTeam(team.id)}/><span><strong>{team.name}</strong><code>{team.slug}</code></span></label>)}
            </div>
          </div>
          {created && <div className={styles.secret}><div><span>One-time signup URL</span><code>{created.signup_url}</code>{created.invitation.teams.length > 0 && <div className={styles.tags}>{created.invitation.teams.map((team)=><em className={styles.teamTag} key={team.id}>{team.name}</em>)}</div>}</div><button type="button" onClick={copySignupURL}>{copied ? "Copied" : "Copy"}</button></div>}
        </section>

        <section className={styles.teamPanel}>
          <div className={styles.teamPanelHeader}>
            <div><p className={styles.sectionLabel}>Teams</p><h2>Overlapping groups, stable identities.</h2><p>A user may join any number of teams. Renaming changes only the display name; the stable ID and slug remain intact.</p></div>
            <div className={styles.teamCreate}>
              <label>Name<input value={newTeamName} onChange={(event)=>setNewTeamName(event.target.value)} placeholder="Product Engineering"/></label>
              <label>Slug<input value={newTeamSlug} onChange={(event)=>setNewTeamSlug(event.target.value.toLowerCase())} placeholder="product-engineering"/></label>
              <button type="button" onClick={createTeam} disabled={busy === "team:create" || !newTeamName.trim() || !newTeamSlug.trim()}>{busy === "team:create" ? "Creating…" : "Create team"}</button>
            </div>
          </div>
          <div className={styles.teamCards}>
            {!loading && teams.length === 0 && <p className={styles.empty}>No teams yet. Users can remain teamless indefinitely.</p>}
            {teams.map((team) => {
              const memberIDs = new Set(team.members.map((member) => member.id));
              const availableUsers = users.filter((user) => !user.disabled_at && !memberIDs.has(user.id));
              const selectedUserID = memberDrafts[team.id] || availableUsers[0]?.id || "";
              return <article className={styles.teamCard} key={team.id}>
                <div className={styles.teamCardTop}><div><code>{team.slug}</code><span>{team.id}</span></div><strong>{team.members.length} {team.members.length === 1 ? "member" : "members"}</strong></div>
                <div className={styles.teamNameEditor}><input aria-label={`Rename ${team.name}`} value={teamNameDrafts[team.id] ?? team.name} onChange={(event)=>setTeamNameDrafts((current)=>({...current,[team.id]:event.target.value}))}/><button type="button" onClick={()=>renameTeam(team)} disabled={busy === `team:rename:${team.id}` || !(teamNameDrafts[team.id] ?? "").trim() || teamNameDrafts[team.id] === team.name}>{busy === `team:rename:${team.id}` ? "Saving…" : "Save name"}</button></div>
                <div className={styles.memberList}>
                  {team.members.length === 0 && <p>No members.</p>}
                  {team.members.map((member)=><div className={styles.memberChip} key={member.id}><span><strong>{member.display_name}</strong><small>{member.email}</small></span><button type="button" aria-label={`Remove ${member.display_name} from ${team.name}`} disabled={busy === `team:remove:${team.id}:${member.id}`} onClick={()=>removeMember(team.id,member.id)}>×</button></div>)}
                </div>
                <div className={styles.memberAdder}>
                  <select aria-label={`Add a member to ${team.name}`} value={selectedUserID} onChange={(event)=>setMemberDrafts((current)=>({...current,[team.id]:event.target.value}))} disabled={availableUsers.length === 0}>{availableUsers.length === 0 ? <option value="">Everyone is a member</option> : availableUsers.map((user)=><option key={user.id} value={user.id}>{user.display_name}{user.disabled_at ? " · disabled" : ""}</option>)}</select>
                  <button type="button" onClick={()=>addMember(team,selectedUserID)} disabled={!selectedUserID || busy === `team:add:${team.id}`}>{busy === `team:add:${team.id}` ? "Adding…" : "Add"}</button>
                </div>
              </article>;
            })}
          </div>
        </section>

        <section className={styles.grid}>
          <div className={styles.panel}>
            <div className={styles.panelHeading}><div><p className={styles.sectionLabel}>Accounts</p><h2>Deployment users</h2></div><button className={styles.ghost} type="button" onClick={()=>void load()} disabled={loading}>Refresh</button></div>
            <div className={styles.rows}>
              {loading && <p className={styles.empty}>Loading users…</p>}
              {!loading && users.map((user) => {
                const userTeams = teamsByUser.get(user.id) ?? [];
                const userCredentials = credentialsByUser.get(user.id) ?? [];
                return <article className={styles.userRow} key={user.id}>
                  <div className={styles.avatar}>{user.display_name.slice(0, 1).toUpperCase()}</div>
                  <div className={styles.identity}><div><strong>{user.display_name}</strong>{user.is_owner && <span className={styles.ownerBadge}>Owner</span>}{user.disabled_at && <span className={styles.disabledBadge}>Disabled</span>}</div><span>{user.email}</span><div className={styles.tags}>{userTeams.length === 0 ? <em className={styles.noTeam}>No teams</em> : userTeams.map((team)=><em className={styles.teamTag} key={team.id}>{team.name}</em>)}</div><code>{user.id}</code></div>
                  <div className={styles.rowAction}>{user.is_owner ? <span>Protected</span> : <><button type="button" disabled={busy === `user:${user.id}`} onClick={()=>setDisabled(user, !user.disabled_at)}>{busy === `user:${user.id}` ? "Saving…" : user.disabled_at ? "Enable" : "Disable"}</button>{user.disabled_at && <button className={styles.danger} type="button" disabled={busy === `purge:${user.id}`} onClick={()=>purgeAttachments(user)}>{busy === `purge:${user.id}` ? "Purging…" : "Purge attachments"}</button>}</>}</div>
                  <div className={styles.credentialList}>
                    <div className={styles.credentialHeading}><span>Credentials</span><small>{userCredentials.filter((credential) => !credential.revoked_at).length} active</small></div>
                    {userCredentials.length === 0 && <p>No credentials created.</p>}
                    {userCredentials.map((credential) => <div className={styles.credentialRow} key={credential.id}>
                      <div><div><strong>{credential.name}</strong><em>{credential.purpose}</em>{credential.revoked_at && <em className={styles.revokedBadge}>Revoked</em>}</div><code>{credential.key_masked || `${credential.token_prefix}…`}</code><span>Created {new Date(credential.created_at).toLocaleString()} · {credential.last_used_at ? `Last used ${new Date(credential.last_used_at).toLocaleString()}` : "Never used"}</span></div>
                      {!credential.revoked_at && <button className={styles.ghost} type="button" disabled={busy === `credential:${credential.id}`} onClick={()=>revokeCredential(credential)}>{busy === `credential:${credential.id}` ? "Revoking…" : "Revoke"}</button>}
                    </div>)}
                  </div>
                </article>;
              })}
            </div>
          </div>

          <div className={styles.panel}>
            <div className={styles.panelHeading}><div><p className={styles.sectionLabel}>Links</p><h2>Invitation history</h2></div></div>
            <div className={styles.rows}>
              {!loading && invitations.length === 0 && <p className={styles.empty}>No invitations yet.</p>}
              {invitations.map((invitation) => {
                const status = invitationStatus(invitation);
                return <article className={styles.invitationRow} key={invitation.id}><div><div className={styles.invitationTop}><strong>{status}</strong><code>{invitation.id}</code></div><span>Expires {new Date(invitation.expires_at).toLocaleString()}</span><div className={styles.tags}>{invitation.teams.length === 0 ? <em className={styles.noTeam}>No initial teams</em> : invitation.teams.map((team)=><em className={styles.teamTag} key={team.id}>{team.name}</em>)}</div></div>{status === "Active" && <button className={styles.ghost} type="button" disabled={busy === `invite:${invitation.id}`} onClick={()=>revokeInvitation(invitation.id)}>{busy === `invite:${invitation.id}` ? "Revoking…" : "Revoke"}</button>}</article>;
              })}
            </div>
          </div>
        </section>
      </main>
    </div>
  );
}
