"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { AgentboxLogo } from "../components/agentbox-logo";
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
        <Link className={styles.brand} href="/"><AgentboxLogo className={styles.mark}/><span>Agentbox</span></Link>
        <nav className={styles.nav} aria-label="Account navigation"><Link href="/login">Sign in</Link><ThemeSwitcher compact/></nav>
      </header>
      <main className={styles.main}>
        <div className={styles.cardWrap}>
          <form className={styles.card} onSubmit={submit}>
            <p className={styles.eyebrow}>Invitation only</p>
            <h1>{invitationState === "invalid" ? "Invalid invitation." : "Create an account."}</h1>
            <p className={styles.cardCopy} aria-live="polite">
              {invitationState === "checking" && "Verifying the one-time invitation…"}
              {invitationState === "valid" && `This link expires ${expiresAt ? new Date(expiresAt).toLocaleString() : "soon"}. It is consumed only after registration succeeds.`}
              {invitationState === "invalid" && "The link may be expired, revoked, or already used. Ask the deployment owner for a new invitation."}
            </p>
            {invitationState === "valid" && <div className={styles.fields}>
              <label className={styles.label}>Email<input autoComplete="email" autoFocus className={styles.input} value={email} onChange={(event)=>setEmail(event.target.value)} placeholder="you@example.com" type="email" required/></label>
              <label className={styles.label}>Display name<input autoComplete="name" className={styles.input} value={displayName} onChange={(event)=>setDisplayName(event.target.value)} placeholder="Your name" type="text" required/></label>
              <label className={styles.label}>Password<input autoComplete="new-password" className={styles.input} value={password} onChange={(event)=>setPassword(event.target.value)} placeholder="Create a password" type="password" required/></label>
              <label className={styles.label}>Confirm password<input autoComplete="new-password" className={styles.input} value={confirmPassword} onChange={(event)=>setConfirmPassword(event.target.value)} placeholder="Repeat your password" type="password" required/></label>
            </div>}
            {error && <div className={styles.error} role="alert"><strong>Registration failed.</strong><span>{error}</span></div>}
            {invitationState === "valid" && <button className={styles.submit} type="submit" disabled={loading || !complete}>{loading ? "Creating account…" : "Create account"}</button>}
            <p className={styles.note}>{invitationState === "invalid" ? "Need a new invitation? Contact your deployment owner." : <>Already registered? <Link href="/login">Sign in</Link></>}</p>
          </form>
        </div>
      </main>
      <footer className={styles.footer}><Link href="/setup">Self-host Agentbox</Link><a href="https://github.com/amxv/agentbox">GitHub</a></footer>
    </div>
  );
}
