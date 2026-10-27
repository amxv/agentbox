"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { AgentboxLogo } from "../components/agentbox-logo";
import { fetchSession } from "../components/session";
import { ThemeSwitcher } from "../components/theme-switcher";
import styles from "./auth.module.css";

export function LoginView() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const next = searchParams.get("next") || "/threads";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [checkingSession, setCheckingSession] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function checkSession() {
      try {
        const session = await fetchSession();
        if (session) { router.replace(next); return; }
      } catch {} finally { setCheckingSession(false); }
    }
    void checkSession();
  }, [next, router]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setLoading(true); setError(null);
    try {
      const response = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ email, password })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      router.replace(next);
    } catch (err) { setError(err instanceof Error ? err.message : String(err)); }
    finally { setLoading(false); }
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Link className={styles.brand} href="/"><AgentboxLogo className={styles.mark}/><span>Agentbox</span></Link>
        <nav className={styles.nav} aria-label="Account navigation"><Link href="/">Home</Link><ThemeSwitcher compact/></nav>
      </header>
      <main className={styles.main}>
        <div className={styles.cardWrap}>
          <form className={styles.card} onSubmit={submit}>
            <p className={styles.eyebrow}>Welcome back</p>
            <h1>Sign in.</h1>
            <p className={styles.cardCopy}>Your shared inbox is right where you left it.</p>
            <div className={styles.fields}>
              <label className={styles.label}>Email<input autoComplete="email" autoFocus className={styles.input} value={email} onChange={(e)=>setEmail(e.target.value)} placeholder="you@example.com" type="email" required/></label>
              <label className={styles.label}>Password<input autoComplete="current-password" className={styles.input} value={password} onChange={(e)=>setPassword(e.target.value)} placeholder="Your password" type="password" required/></label>
            </div>
            {error && <div className={styles.error} role="alert"><strong>Could not sign in.</strong><span>{error}</span></div>}
            <button className={styles.submit} type="submit" disabled={loading || checkingSession || !email.trim() || !password}>{loading ? "Signing in…" : checkingSession ? "Checking session…" : "Sign in"}</button>
            <p className={styles.note}>Invitation only. Ask your deployment owner for access.</p>
          </form>
        </div>
      </main>
      <footer className={styles.footer}><Link href="/setup">Self-host Agentbox</Link><a href="https://github.com/amxv/agentbox">GitHub</a></footer>
    </div>
  );
}
