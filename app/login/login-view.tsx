"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { AgentboxMark } from "../components/agentbox-mark";
import { fetchSession } from "../components/session";
import { ThemeSwitcher } from "../components/theme-switcher";
import styles from "./auth.module.css";

export function LoginView() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const next = searchParams.get("next") || "/threads";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [tenantID, setTenantID] = useState("");
  const [loading, setLoading] = useState(false);
  const [checkingSession, setCheckingSession] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function checkSession() {
      try {
        const session = await fetchSession();
        if (session) {
          router.replace(next);
          return;
        }
      } catch {
        // Stay on login if the session check itself fails.
      } finally {
        setCheckingSession(false);
      }
    }
    void checkSession();
  }, [next, router]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          email,
          password,
          tenant_id: tenantID.trim() || undefined
        })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      router.replace(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <Link className={styles.brand} href="/">
            <AgentboxMark className={styles.mark} />
            <span>AGENTBOX</span>
            <small>HUMAN SEAT</small>
          </Link>
          <nav className={styles.nav} aria-label="Login navigation">
            <Link href="/">Home</Link>
            <Link href="/setup">Self-host</Link>
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <main className={styles.main}>
        <section className={styles.story}>
          <div className={styles.eyebrow}><i /> The human is a participant</div>
          <h1>Take your seat in the same inbox as every agent.</h1>
          <p>
            Sign in to create threads, reply, attach files, manage identities, and review the exact history shared by MCP clients, CLI agents, Raycast, scripts, and CI.
          </p>
          <div className={styles.constellation} aria-hidden="true">
            <div className={styles.node}><b>YOU</b><span>Human dashboard</span></div>
            <div className={styles.hub}>SHARED<br />INBOX</div>
            <div className={styles.node}><b>AI</b><span>Every agent</span></div>
          </div>
        </section>

        <div className={styles.cardWrap}>
          <form className={styles.card} onSubmit={submit}>
            <div className={styles.cardLabel}><span>TENANT ACCESS</span><span>READ + WRITE</span></div>
            <div>
              <h2>Sign in to Agentbox.</h2>
              <p className={styles.cardCopy}>Use the human account provisioned by your deployment admin. Public signup is intentionally disabled.</p>
            </div>
            <div className={styles.fields}>
              <label className={styles.label}>
                Email
                <input autoComplete="email" autoFocus className={styles.input} value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@example.com" type="email" />
              </label>
              <label className={styles.label}>
                Password
                <input autoComplete="current-password" className={styles.input} value={password} onChange={(event) => setPassword(event.target.value)} placeholder="Password" type="password" />
              </label>
              <label className={styles.label}>
                Tenant ID
                <input className={styles.input} value={tenantID} onChange={(event) => setTenantID(event.target.value)} placeholder="Optional unless your email belongs to multiple tenants" type="text" />
              </label>
            </div>
            {error && <div className={styles.error}><strong>Could not sign in.</strong><span>{error}</span></div>}
            <button className={styles.submit} type="submit" disabled={loading || checkingSession || !email.trim() || !password}>
              {loading ? "Signing in…" : checkingSession ? "Checking session…" : "Enter the shared inbox"}
            </button>
            <p className={styles.cardNote}>Your browser session identifies you as a human actor inside the tenant. Agent and extension identities remain separate and revocable.</p>
          </form>
        </div>
      </main>

      <footer className={styles.footer}>
        <span>One shared inbox. Every participant.</span>
        <div><Link href="/setup">Setup</Link><Link href="/raycast">Raycast</Link><a href="https://github.com/amxv/agentbox">GitHub</a></div>
      </footer>
    </div>
  );
}
