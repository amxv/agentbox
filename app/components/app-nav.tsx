"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { AgentboxMark } from "./agentbox-mark";
import { attributionLabel } from "./attribution";
import { AuthContext, fetchSession, signOutSession } from "./session";
import { ThemeSwitcher } from "./theme-switcher";

type NavLink = {
  href: string;
  label: string;
};

const PRIMARY_LINKS: NavLink[] = [
  { href: "/threads", label: "Inbox" },
  { href: "/keys", label: "Credentials" },
  { href: "/onboarding", label: "Connect agents" },
  { href: "/raycast", label: "Raycast" }
];

const OWNER_LINKS: NavLink[] = [
  { href: "/owner/users", label: "Users & teams" },
  { href: "/owner/content", label: "All content" }
];

function isActive(pathname: string | null, href: string) {
  if (!pathname) return false;
  if (href === "/threads") return pathname === "/threads" || pathname.startsWith("/threads/");
  if (href === "/owner/content") return pathname === "/owner/content" || pathname.startsWith("/owner/content/");
  return pathname === href;
}

export type AppNavProps = {
  /** Subtitle rendered under the Agentbox wordmark. */
  title: string;
  /**
   * Session for the current page. When omitted the nav resolves its own
   * session so a page that does not already load one can still render
   * owner-only links, attribution, and sign out.
   */
  auth?: AuthContext | null;
};

export function AppNav({ title, auth: providedAuth }: AppNavProps) {
  const pathname = usePathname();
  const [resolvedAuth, setResolvedAuth] = useState<AuthContext | null>(null);
  const [signingOut, setSigningOut] = useState(false);

  const shouldResolveOwnSession = providedAuth === undefined;

  useEffect(() => {
    if (!shouldResolveOwnSession) return;
    const controller = new AbortController();
    fetchSession(controller.signal)
      .then(setResolvedAuth)
      .catch(() => {
        // A failed session lookup must not break page navigation.
      });
    return () => controller.abort();
  }, [shouldResolveOwnSession]);

  const auth = shouldResolveOwnSession ? resolvedAuth : providedAuth;

  async function handleSignOut() {
    setSigningOut(true);
    try {
      await signOutSession();
      window.location.href = "/login";
    } catch {
      setSigningOut(false);
    }
  }

  const links = auth?.is_owner ? [...PRIMARY_LINKS, ...OWNER_LINKS] : PRIMARY_LINKS;

  return (
    <header className="site-header">
      <div className="shell site-header__inner">
        <Link className="brand app-nav__brand" href="/threads">
          <AgentboxMark className="app-nav__mark" />
          <span>
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">{title}</span>
          </span>
        </Link>
        <nav className="site-nav app-nav" aria-label="Main navigation">
          {links.map((link) => {
            const active = isActive(pathname, link.href);
            return (
              <Link
                key={link.href}
                className={`site-nav__link${active ? " site-nav__link--active" : ""}`}
                href={link.href}
                aria-current={active ? "page" : undefined}
              >
                {link.label}
              </Link>
            );
          })}
          {auth && (
            <span className="session-chip">
              {attributionLabel(auth.user_display_name, auth.actor_name)}
            </span>
          )}
          {auth && (
            <button
              className="site-nav__link"
              type="button"
              onClick={() => void handleSignOut()}
              disabled={signingOut}
            >
              {signingOut ? "Signing out…" : "Sign out"}
            </button>
          )}
          <ThemeSwitcher />
        </nav>
      </div>
    </header>
  );
}
