"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { AgentboxMark } from "../components/agentbox-mark";
import { ThemeSwitcher } from "../components/theme-switcher";
import styles from "../login/auth.module.css";

type InvitationState = "checking" | "valid" | "invalid";

export function SignupView() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [token] = useState(searchParams.get("token") ?? "");
  const [invitationState, setInvitationState] = useState<InvitationState>("checking");
  const [expiresAt, setExpiresAt] = useState("");
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (searchParams.has("token")) window.history.replaceState(null, "", "/signup");
    let cancelled = false;
    async function inspect() {
      if (!token) {
        setInvitationState("invalid");
        return;
      }
      try {
        const response = await fetch("/api/auth/invitations/inspect", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ token })
        });
        const data = await response.json();
        if (!cancelled && response.ok && data.valid) {
          setExpiresAt(data.expires_at ?? "");
          setInvitationState("valid");
        } else if (!cancelled) {
          setInvitationState("invalid");
        }
      } catch {
        if (!cancelled) setInvitationState("invalid");
      }
    }
    void inspect();
    return () => { cancelled = true; };
  }, [searchParams, token]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    if (password !== confirmPassword) {
      setError("Passwords do not match.");
      return;
    }
    setLoading(true);
    try {
      const response = await fetch("/api/auth/invitations/register", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ token, email, display_name: displayName, password })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? "Registration could not be completed.");
      router.replace(data.redirect ?? "/onboarding");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  const complete = email.trim() && displayName.trim() && password && confirmPassword;

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Link className={styles.brand} href="/"><AgentboxMark className={styles.mark}/><span>Agentbox</span><small>Invitation</small></Link>
        <nav className={styles.nav}><Link href="/login">Sign in</Link><ThemeSwitcher/></nav>
      </header>
      <main className={styles.main}>
        <section className={styles.story}>
          <p className={styles.eyebrow}>Deployment-global account</p>
          <h1>One identity for every Agentbox surface.</h1>
          <p>Your browser, local CLI, ChatGPT, Claude, and other credentials can act as distinct agents while sharing one user identity and the same thread access.</p>
          <div className={styles.ticket}><div><span>Account</span><b>Created once</b></div><div><span>Credentials</span><b>Separate actors</b></div><div><span>Access</span><b>Private by default</b></div></div>
        </section>
        <div className={styles.cardWrap}>
          <form className={styles.card} onSubmit={submit}>
            <div className={styles.cardTop}><span>Owner invitation</span><span>{invitationState === "valid" ? "Valid" : invitationState === "checking" ? "Checking" : "Unavailable"}</span></div>
            <h2>{invitationState === "invalid" ? "This invitation cannot be used." : "Create your account."}</h2>
            <p className={styles.cardCopy}>
              {invitationState === "checking" && "Verifying the one-time invitation…"}
              {invitationState === "valid" && `This link expires ${expiresAt ? new Date(expiresAt).toLocaleString() : "soon"}. It is consumed only after registration succeeds.`}
              {invitationState === "invalid" && "The link may be expired, revoked, or already used. Ask the deployment owner for a new invitation."}
            </p>
            {invitationState === "valid" && <div className={styles.fields}>
              <label className={styles.label}>Email<input autoComplete="email" autoFocus className={styles.input} value={email} onChange={(event)=>setEmail(event.target.value)} placeholder="you@example.com" type="email"/></label>
              <label className={styles.label}>Display name<input autoComplete="name" className={styles.input} value={displayName} onChange={(event)=>setDisplayName(event.target.value)} placeholder="Your name" type="text"/></label>
              <label className={styles.label}>Password<input autoComplete="new-password" className={styles.input} value={password} onChange={(event)=>setPassword(event.target.value)} placeholder="Create a password" type="password"/></label>
              <label className={styles.label}>Confirm password<input autoComplete="new-password" className={styles.input} value={confirmPassword} onChange={(event)=>setConfirmPassword(event.target.value)} placeholder="Repeat the password" type="password"/></label>
            </div>}
            {error && <div className={styles.error}><strong>Registration failed.</strong><span>{error}</span></div>}
            {invitationState === "valid" && <button className={styles.submit} type="submit" disabled={loading || !complete}>{loading ? "Creating account…" : "Join Agentbox"}</button>}
            <p className={styles.note}>Invitation tokens are single-use and stored only as hashes. Failed registration does not consume a valid link.</p>
          </form>
        </div>
      </main>
      <footer className={styles.footer}><span>Invitation-only account creation.</span><div><Link href="/login">Sign in</Link><Link href="/setup">Self-host</Link><a href="https://github.com/amxv/agentbox">GitHub</a></div></footer>
    </div>
  );
}
