"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { AgentboxLogo } from "../../components/agentbox-logo";
import { ThemeSwitcher } from "../../components/theme-switcher";
import styles from "../../login/auth.module.css";

export function OwnerSetupView() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [token, setToken] = useState(searchParams.get("token") ?? "");
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (searchParams.has("token")) window.history.replaceState(null, "", "/owner/setup");
  }, [searchParams]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    if (password !== confirmPassword) {
      setError("Passwords do not match.");
      return;
    }
    setLoading(true);
    try {
      const response = await fetch("/api/auth/owner/setup", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ token, email, display_name: displayName, password })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      router.replace("/owner/users");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  const complete = token.trim() && email.trim() && displayName.trim() && password && confirmPassword;

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Link className={styles.brand} href="/"><AgentboxLogo className={styles.mark}/><span>Agentbox</span></Link>
        <nav className={styles.nav} aria-label="Account navigation"><Link href="/setup">Self-host</Link><ThemeSwitcher compact/></nav>
      </header>
      <main className={styles.main}>
        <div className={styles.cardWrap}>
          <form className={styles.card} onSubmit={submit}>
            <p className={styles.eyebrow}>Deployment owner</p>
            <h1>Set up your account.</h1>
            <p className={styles.cardCopy}>For recovery, use the same owner email as before.</p>
            <div className={styles.fields}>
              <label className={styles.label}>Setup token<input autoComplete="off" autoFocus={!token} className={styles.input} value={token} onChange={(event)=>setToken(event.target.value)} placeholder="agos_…" type="password"/></label>
              <label className={styles.label}>Owner email<input autoComplete="email" autoFocus={Boolean(token)} className={styles.input} value={email} onChange={(event)=>setEmail(event.target.value)} placeholder="owner@example.com" type="email"/></label>
              <label className={styles.label}>Display name<input autoComplete="name" className={styles.input} value={displayName} onChange={(event)=>setDisplayName(event.target.value)} placeholder="Owner name" type="text"/></label>
              <label className={styles.label}>Password<input autoComplete="new-password" className={styles.input} value={password} onChange={(event)=>setPassword(event.target.value)} placeholder="New password" type="password"/></label>
              <label className={styles.label}>Confirm password<input autoComplete="new-password" className={styles.input} value={confirmPassword} onChange={(event)=>setConfirmPassword(event.target.value)} placeholder="Repeat password" type="password"/></label>
            </div>
            {error && <div className={styles.error} role="alert"><strong>Owner setup failed.</strong><span>{error}</span></div>}
            <button className={styles.submit} type="submit" disabled={loading || !complete}>{loading ? "Securing owner…" : "Create owner session"}</button>
            <p className={styles.note}>One-time token. Keep it private.</p>
          </form>
        </div>
      </main>
      <footer className={styles.footer}><Link href="/login">Sign in</Link><Link href="/setup">Setup guide</Link></footer>
    </div>
  );
}
