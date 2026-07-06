"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { fetchSession } from "../components/session";
import { ThemeSwitcher } from "../components/theme-switcher";

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
    <div className="dashboard-page">
      <header className="site-header">
        <div className="shell site-header__inner">
          <Link className="brand" href="/">
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">Sign in</span>
          </Link>
          <nav className="site-nav" aria-label="Login navigation">
            <Link className="site-nav__link" href="/">Home</Link>
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <main className="dashboard-main shell login-main">
        <form className="sign-in-card login-card" onSubmit={submit}>
          <div>
            <p className="section-label">Tenant access</p>
            <h1 className="card-title">Sign in to Agentbox</h1>
            <p className="copy">Use the account provisioned by your deployment admin. Public signup is disabled.</p>
          </div>
          <label className="form-label">
            Email
            <input
              autoComplete="email"
              autoFocus
              className="form-input"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              placeholder="you@example.com"
              type="email"
            />
          </label>
          <label className="form-label">
            Password
            <input
              autoComplete="current-password"
              className="form-input"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              placeholder="Password"
              type="password"
            />
          </label>
          <label className="form-label">
            Tenant ID
            <input
              className="form-input"
              value={tenantID}
              onChange={(event) => setTenantID(event.target.value)}
              placeholder="Optional unless your email is in multiple tenants"
              type="text"
            />
          </label>
          {error && (
            <div className="error-card">
              <strong>Could not sign in.</strong>
              <span>{error}</span>
            </div>
          )}
          <button className="button button--solid" type="submit" disabled={loading || checkingSession || !email.trim() || !password}>
            {loading ? "Signing in..." : "Sign in"}
          </button>
        </form>
      </main>
    </div>
  );
}
