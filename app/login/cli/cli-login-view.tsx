"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { fetchSession } from "../../components/session";
import { ThemeSwitcher } from "../../components/theme-switcher";

function callbackURL(redirectURI: string, params: Record<string, string>) {
  const target = new URL(redirectURI);
  for (const [key, value] of Object.entries(params)) {
    target.searchParams.set(key, value);
  }
  return target.toString();
}

export function CLILoginView() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const state = searchParams.get("state") ?? "";
  const redirectURI = searchParams.get("redirect_uri") ?? "";
  const [status, setStatus] = useState("Authorizing CLI access...");
  const [error, setError] = useState<string | null>(null);
  const next = useMemo(() => {
    const current = `/login/cli?${searchParams.toString()}`;
    return `/login?next=${encodeURIComponent(current)}`;
  }, [searchParams]);

  useEffect(() => {
    async function authorize() {
      if (!state || !redirectURI) {
        setError("Missing CLI login parameters.");
        setStatus("Unable to authorize CLI access.");
        return;
      }
      try {
        const session = await fetchSession();
        if (!session) {
          router.replace(next);
          return;
        }
        const response = await fetch("/api/auth/cli/authorize", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ state, redirect_uri: redirectURI })
        });
        const data = await response.json() as { code?: string; error?: string };
        if (!response.ok || !data.code) throw new Error(data.error ?? `HTTP ${response.status}`);
        window.location.assign(callbackURL(redirectURI, { code: data.code, state }));
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        setError(message);
        setStatus("Unable to authorize CLI access.");
        if (redirectURI) {
          window.location.assign(callbackURL(redirectURI, { error: message, state }));
        }
      }
    }
    void authorize();
  }, [next, redirectURI, router, state]);

  return (
    <div className="dashboard-page">
      <header className="site-header">
        <div className="shell site-header__inner">
          <Link className="brand" href="/">
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">CLI login</span>
          </Link>
          <nav className="site-nav" aria-label="CLI login navigation">
            <Link className="site-nav__link" href="/threads">Threads</Link>
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <main className="dashboard-main shell login-main">
        <section className="sign-in-card login-card">
          <div>
            <p className="section-label">Local CLI</p>
            <h1 className="card-title">{status}</h1>
            <p className="copy">This page will return to the waiting agentbox command after authorization.</p>
          </div>
          {error && (
            <div className="error-card">
              <strong>CLI login failed.</strong>
              <span>{error}</span>
            </div>
          )}
        </section>
      </main>
    </div>
  );
}
