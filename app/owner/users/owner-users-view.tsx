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

type Invitation = {
  id: string;
  created_by_user_id: string;
  created_at: string;
  expires_at: string;
  consumed_at?: string;
  consumed_by_user_id?: string;
  revoked_at?: string;
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

export function OwnerUsersView() {
  const router = useRouter();
  const [users, setUsers] = useState<User[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [created, setCreated] = useState<CreatedInvitation | null>(null);
  const [expiryMinutes, setExpiryMinutes] = useState(7 * 24 * 60);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const activeInvitations = useMemo(
    () => invitations.filter((invitation) => invitationStatus(invitation) === "Active").length,
    [invitations]
  );

  const load = useCallback(async () => {
    setError(null);
    try {
      const [usersResponse, invitationsResponse] = await Promise.all([
        fetch("/api/owner/users", { cache: "no-store" }),
        fetch("/api/owner/invitations", { cache: "no-store" })
      ]);
      if (usersResponse.status === 401 || usersResponse.status === 403) {
        router.replace("/login?next=/owner/users");
        return;
      }
      const usersData = await usersResponse.json();
      const invitationsData = await invitationsResponse.json();
      if (!usersResponse.ok) throw new Error(usersData.error ?? `HTTP ${usersResponse.status}`);
      if (!invitationsResponse.ok) throw new Error(invitationsData.error ?? `HTTP ${invitationsResponse.status}`);
      setUsers(usersData.users ?? []);
      setInvitations(invitationsData.invitations ?? []);
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
    setBusy("create");
    setError(null);
    setCopied(false);
    try {
      const response = await fetch("/api/owner/invitations", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ expires_in_minutes: expiryMinutes })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      const signupURL = typeof data.signup_url === "string" && data.signup_url.startsWith("/")
        ? `${window.location.origin}${data.signup_url}`
        : data.signup_url;
      setCreated({ ...data, signup_url: signupURL });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function revokeInvitation(id: string) {
    setBusy(id);
    setError(null);
    try {
      const response = await fetch(`/api/owner/invitations/${encodeURIComponent(id)}`, { method: "DELETE" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      if (created?.invitation.id === id) setCreated(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  async function setDisabled(user: User, disabled: boolean) {
    setBusy(user.id);
    setError(null);
    try {
      const action = disabled ? "disable" : "enable";
      const response = await fetch(`/api/owner/users/${encodeURIComponent(user.id)}/${action}`, { method: "POST" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
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
        <nav className={styles.nav}><Link href="/threads">Inbox</Link><Link href="/keys">Credentials</Link><ThemeSwitcher/></nav>
      </header>
      <main className={styles.main}>
        <section className={styles.hero}>
          <div><p className={styles.eyebrow}>Deployment administration</p><h1>Users and invitations.</h1><p>Account creation is invitation-only. Disabling a user immediately revokes every browser session and API credential without deleting their historical attribution.</p></div>
          <div className={styles.metrics}><div><span>Users</span><b>{users.length}</b></div><div><span>Active invitations</span><b>{activeInvitations}</b></div><div><span>Owner</span><b>Permanent</b></div></div>
        </section>

        {error && <div className={styles.error}><strong>Owner action failed.</strong><span>{error}</span></div>}

        <section className={styles.invitePanel}>
          <div><p className={styles.sectionLabel}>Invite a user</p><h2>Create a one-time signup link.</h2><p>The token is shown once, stored only as a hash, and consumed only after the new account and browser session commit.</p></div>
          <div className={styles.inviteControls}>
            <label>Expires<select value={expiryMinutes} onChange={(event)=>setExpiryMinutes(Number(event.target.value))}><option value={60}>1 hour</option><option value={24*60}>1 day</option><option value={7*24*60}>7 days</option><option value={30*24*60}>30 days</option></select></label>
            <button type="button" onClick={createInvitation} disabled={busy === "create"}>{busy === "create" ? "Creating…" : "Create invitation"}</button>
          </div>
          {created && <div className={styles.secret}><div><span>One-time signup URL</span><code>{created.signup_url}</code></div><button type="button" onClick={copySignupURL}>{copied ? "Copied" : "Copy"}</button></div>}
        </section>

        <section className={styles.grid}>
          <div className={styles.panel}>
            <div className={styles.panelHeading}><div><p className={styles.sectionLabel}>Accounts</p><h2>Deployment users</h2></div><button className={styles.ghost} type="button" onClick={()=>void load()} disabled={loading}>Refresh</button></div>
            <div className={styles.rows}>
              {loading && <p className={styles.empty}>Loading users…</p>}
              {!loading && users.map((user) => <article className={styles.userRow} key={user.id}>
                <div className={styles.avatar}>{user.display_name.slice(0, 1).toUpperCase()}</div>
                <div className={styles.identity}><div><strong>{user.display_name}</strong>{user.is_owner && <span className={styles.ownerBadge}>Owner</span>}{user.disabled_at && <span className={styles.disabledBadge}>Disabled</span>}</div><span>{user.email}</span><code>{user.id}</code></div>
                <div className={styles.rowAction}>{user.is_owner ? <span>Protected</span> : <button type="button" disabled={busy === user.id} onClick={()=>setDisabled(user, !user.disabled_at)}>{busy === user.id ? "Saving…" : user.disabled_at ? "Enable" : "Disable"}</button>}</div>
              </article>)}
            </div>
          </div>

          <div className={styles.panel}>
            <div className={styles.panelHeading}><div><p className={styles.sectionLabel}>Links</p><h2>Invitation history</h2></div></div>
            <div className={styles.rows}>
              {!loading && invitations.length === 0 && <p className={styles.empty}>No invitations yet.</p>}
              {invitations.map((invitation) => {
                const status = invitationStatus(invitation);
                return <article className={styles.invitationRow} key={invitation.id}><div><div className={styles.invitationTop}><strong>{status}</strong><code>{invitation.id}</code></div><span>Expires {new Date(invitation.expires_at).toLocaleString()}</span></div>{status === "Active" && <button className={styles.ghost} type="button" disabled={busy === invitation.id} onClick={()=>revokeInvitation(invitation.id)}>{busy === invitation.id ? "Revoking…" : "Revoke"}</button>}</article>;
              })}
            </div>
          </div>
        </section>
      </main>
    </div>
  );
}
