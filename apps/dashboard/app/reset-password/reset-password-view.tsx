"use client";

import Link from "next/link";
import { FormEvent, useEffect, useRef, useState } from "react";
import { AgentboxMark } from "../components/agentbox-mark";
import { ThemeSwitcher } from "../components/theme-switcher";
import styles from "../login/auth.module.css";

type LinkState = "checking" | "valid" | "invalid" | "done";

export function ResetPasswordView() {
  const tokenRef = useRef("");
  const [state, setState] = useState<LinkState>("checking");
  const [expiresAt, setExpiresAt] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // The secret is delivered in the fragment: no HTTP access logs or referrer headers.
    // Remove it from the visible URL before making any requests.
    const params = new URLSearchParams(window.location.hash.slice(1));
    // React Strict Mode replays effects in development after the fragment has
    // been cleared. Keep the secret across that replay without restoring it
    // to the URL or making the user request another link.
    const secret = params.get("token") ?? tokenRef.current;
    if (params.has("token")) window.history.replaceState(null, "", "/reset-password");
    tokenRef.current = secret;
    let cancelled = false;
    async function inspect() {
      if (!secret) { setState("invalid"); return; }
      try {
        const response = await fetch("/api/auth/password-reset/inspect", {
          method: "POST",
          headers: { "content-type": "application/json" },
          cache: "no-store",
          body: JSON.stringify({ token: secret })
        });
        const data = await response.json();
        if (cancelled) return;
        if (response.ok && data.valid) {
          setExpiresAt(data.expires_at ?? "");
          setState("valid");
        } else {
          setState("invalid");
        }
      } catch {
        if (!cancelled) setState("invalid");
      }
    }
    void inspect();
    return () => { cancelled = true; };
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    if (password !== confirmPassword) { setError("Passwords do not match."); return; }
    if (password.length < 12) { setError("Use at least 12 characters."); return; }
    setSaving(true);
    try {
      const response = await fetch("/api/auth/password-reset/complete", {
        method: "POST",
        headers: { "content-type": "application/json" },
        cache: "no-store",
        body: JSON.stringify({ token: tokenRef.current, password })
      });
      const data = await response.json();
      if (!response.ok) {
        if (data.code === "INVALID_PASSWORD_RESET_TOKEN") setState("invalid");
        throw new Error(data.error ?? "The password could not be updated.");
      }
      setPassword("");
      setConfirmPassword("");
      tokenRef.current = "";
      setState("done");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Link className={styles.brand} href="/"><AgentboxMark className={styles.mark}/><span>Agentbox</span><small>Password reset</small></Link>
        <nav className={styles.nav}><Link href="/login">Sign in</Link><ThemeSwitcher/></nav>
      </header>
      <main className={styles.main}>
        <section className={styles.story}>
          <p className={styles.eyebrow}>Private account recovery</p>
          <h1>Back to your shared inbox.</h1>
          <p>Set a new web dashboard password without changing your existing threads, team access, or agent credentials.</p>
          <div className={styles.ticket}>
            <div><span>Link</span><b>Single use</b></div>
            <div><span>Access</span><b>Only from your deployment owner</b></div>
            <div><span>Agents</span><b>Existing API keys unchanged</b></div>
          </div>
        </section>
        <div className={styles.cardWrap}>
          <form className={styles.card} onSubmit={submit}>
            <div className={styles.cardTop}><span>Account recovery</span><span>{state === "checking" ? "Checking" : state === "valid" ? "Valid link" : state === "done" ? "Complete" : "Unavailable"}</span></div>
            <h2>{state === "done" ? "Password updated." : state === "invalid" ? "This link cannot be used." : "Reset your password."}</h2>
            <p className={styles.cardCopy}>
              {state === "checking" && "Verifying your one-time reset link…"}
              {state === "valid" && `Choose a new password of at least 12 characters. This link expires ${expiresAt ? new Date(expiresAt).toLocaleString() : "soon"}.`}
              {state === "invalid" && "The link may have expired, been replaced, or already been used. Ask the deployment owner to generate another link."}
              {state === "done" && "Sign in with your new password. Your previous web sessions were signed out; your connected agents will keep working."}
            </p>
            {state === "valid" && (
              <div className={styles.fields}>
                <label className={styles.label}>New password<input className={styles.input} autoFocus type="password" autoComplete="new-password" minLength={12} maxLength={1024} value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
                <label className={styles.label}>Confirm password<input className={styles.input} type="password" autoComplete="new-password" minLength={12} maxLength={1024} value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} required /></label>
              </div>
            )}
            {error && <div className={styles.error} role="alert"><strong>Could not reset password.</strong><span>{error}</span></div>}
            {state === "valid" && <button className={styles.submit} type="submit" disabled={saving || !password || !confirmPassword}>{saving ? "Updating password…" : "Set new password"}</button>}
            {state === "done" && <Link className={styles.submit} style={{display:"grid",placeItems:"center"}} href="/login">Go to sign in</Link>}
            <p className={styles.note}>For your security, opening this page removes the token from the address bar. Each link works only once.</p>
          </form>
        </div>
      </main>
      <footer className={styles.footer}><span>One identity across Agentbox.</span><div><Link href="/login">Sign in</Link><a href="https://github.com/amxv/agentbox">GitHub</a></div></footer>
    </div>
  );
}
