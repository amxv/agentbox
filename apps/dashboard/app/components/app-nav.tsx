"use client";

import type { LucideIcon } from "lucide-react";
import {
  BoxesIcon,
  ChevronDownIcon,
  InboxIcon,
  KeyRoundIcon,
  LogOutIcon,
  MenuIcon,
  PlugZapIcon,
  RadarIcon,
  SearchIcon,
  ShieldIcon,
  UsersIcon
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { AgentboxMark } from "./agentbox-mark";
import { attributionLabel } from "./attribution";
import { usePanelSession } from "./panel-session";
import { signOutSession, type AuthContext } from "./session";
import { ThemeSwitcher } from "./theme-switcher";

type NavLink = {
  href: string;
  label: string;
  shortLabel: string;
  icon: LucideIcon;
};

const PRIMARY_LINKS: NavLink[] = [
  { href: "/threads", label: "Inbox", shortLabel: "Inbox", icon: InboxIcon },
  { href: "/keys", label: "Credentials", shortLabel: "Keys", icon: KeyRoundIcon },
  { href: "/onboarding", label: "Connect agents", shortLabel: "Connect", icon: PlugZapIcon },
  { href: "/raycast", label: "Raycast", shortLabel: "Raycast", icon: RadarIcon }
];

// Keep Raycast available in the menu without spending permanent navbar space.
const NAVBAR_LINKS = PRIMARY_LINKS.filter((link) => link.href !== "/raycast");

const OWNER_LINKS: NavLink[] = [
  { href: "/owner/users", label: "Users & teams", shortLabel: "Users", icon: UsersIcon },
  { href: "/owner/content", label: "All content", shortLabel: "Audit", icon: SearchIcon }
];

function isActive(pathname: string | null, href: string) {
  if (!pathname) return false;
  if (href === "/threads") return pathname === "/threads" || pathname.startsWith("/threads/");
  if (href === "/owner/content") return pathname === "/owner/content" || pathname.startsWith("/owner/content/");
  return pathname === href;
}

function initials(value?: string) {
  const normalized = value?.trim();
  if (!normalized) return "AB";
  return normalized
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
}

export function AppNav() {
  const pathname = usePathname();
  const { auth, loading, clear } = usePanelSession();
  const [signingOut, setSigningOut] = useState(false);
  const links = auth?.is_owner ? [...PRIMARY_LINKS, ...OWNER_LINKS] : PRIMARY_LINKS;
  const accountLabel = auth
    ? attributionLabel(auth.user_display_name, auth.actor_name)
    : "Loading account";

  async function handleSignOut() {
    setSigningOut(true);
    try {
      await signOutSession();
      clear();
      window.location.href = "/login";
    } catch {
      setSigningOut(false);
    }
  }

  return (
    <header className="panel-nav-shell sticky top-0 z-40">
      <div className="mx-auto flex h-14 max-w-[1480px] items-center gap-3 px-3 sm:h-16 sm:px-7 lg:h-[4.5rem] lg:gap-4 lg:px-8">
        <Link className="panel-brand" href="/threads" aria-label="Agentbox inbox">
          <AgentboxMark className="panel-brand-mark" />
          <span className="panel-brand-name">Agentbox</span>
        </Link>

        <div className="hidden min-w-0 flex-1 items-center gap-3 lg:flex">
          <nav className="panel-nav-links" aria-label="Main navigation">
            {NAVBAR_LINKS.map((link) => (
              <DesktopNavLink key={link.href} link={link} pathname={pathname} />
            ))}
          </nav>

          {loading || auth?.is_owner ? (
            <>
              <span className="h-7 w-px shrink-0 bg-border" aria-hidden="true" />
              <nav
                className={cn("panel-nav-links", loading && "invisible")}
                aria-label="Owner navigation"
                aria-hidden={loading ? "true" : undefined}
              >
                {OWNER_LINKS.map((link) => (
                  <DesktopNavLink key={link.href} link={link} pathname={pathname} owner />
                ))}
              </nav>
            </>
          ) : null}
        </div>

        <div className="ml-auto hidden shrink-0 items-center gap-3 lg:flex">
          <ThemeSwitcher compact />
          {auth ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="outline" className="w-64 justify-between" />}
              >
                <span className="flex min-w-0 items-center gap-2.5">
                  <span className="panel-account-avatar">{initials(auth.user_display_name)}</span>
                  <span className="truncate">{accountLabel}</span>
                </span>
                <ChevronDownIcon data-icon="inline-end" />
              </DropdownMenuTrigger>
              <AccountMenuContent auth={auth} signingOut={signingOut} onSignOut={handleSignOut} />
            </DropdownMenu>
          ) : (
            <Button variant="outline" className="w-64 justify-start" disabled>
              <span className="panel-account-avatar">AB</span>
              <span className="truncate">{accountLabel}</span>
            </Button>
          )}
        </div>

        <div className="ml-auto flex shrink-0 items-center gap-2 lg:hidden">
          <div className="hidden sm:block"><ThemeSwitcher compact /></div>
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button variant="outline" size="icon" className="size-10 sm:size-9" />}>
              <MenuIcon />
              <span className="sr-only">Open navigation</span>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-[min(19rem,calc(100vw-1rem))]">
              <DropdownMenuGroup>
                <DropdownMenuLabel>
                  <span className="flex items-center gap-2.5 text-foreground">
                    <AgentboxMark className="panel-menu-mark" />
                    <span className="font-semibold">Agentbox</span>
                  </span>
                </DropdownMenuLabel>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                {links.map((link) => {
                  const Icon = link.icon;
                  const active = isActive(pathname, link.href);
                  return (
                    <DropdownMenuItem
                      key={link.href}
                      data-current={active ? "true" : undefined}
                      render={<Link href={link.href} />}
                    >
                      <Icon />
                      {link.label}
                      {active ? <span className="ml-auto text-xs text-muted-foreground">Current</span> : null}
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
              <div className="flex items-center justify-between gap-3 border-t px-3 py-3 sm:hidden">
                <span className="text-xs font-medium text-muted-foreground">Appearance</span>
                <ThemeSwitcher compact />
              </div>
              {auth ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>{accountLabel}</DropdownMenuLabel>
                    <DropdownMenuItem variant="destructive" disabled={signingOut} onClick={() => void handleSignOut()}>
                      <LogOutIcon />
                      {signingOut ? "Signing out" : "Sign out"}
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      <nav className="panel-mobile-nav flex gap-1 overflow-x-auto border-t border-border/60 px-3 py-1.5 sm:px-7 lg:hidden" aria-label="Quick navigation">
        {NAVBAR_LINKS.map((link) => {
          const Icon = link.icon;
          const active = isActive(pathname, link.href);
          return (
            <Link
              key={link.href}
              href={link.href}
              aria-current={active ? "page" : undefined}
              className={cn(
                "flex min-h-10 shrink-0 items-center justify-center gap-2 rounded-md px-3 text-xs font-medium text-muted-foreground transition-colors",
                active ? "bg-[var(--panel-active-bg)] text-[var(--panel-active-foreground)]" : "hover:bg-muted hover:text-foreground"
              )}
            >
              <Icon className="size-4" aria-hidden="true" />
              {link.shortLabel}
            </Link>
          );
        })}
      </nav>
    </header>
  );
}

function DesktopNavLink({
  link,
  pathname,
  owner = false
}: {
  link: NavLink;
  pathname: string | null;
  owner?: boolean;
}) {
  const Icon = link.icon;
  const active = isActive(pathname, link.href);
  return (
    <Link
      className={cn("panel-nav-link", active && "is-active", owner && "is-owner")}
      href={link.href}
      aria-current={active ? "page" : undefined}
    >
      <Icon />
      <span className="hidden xl:inline">{link.label}</span>
      <span className="xl:hidden">{link.shortLabel}</span>
    </Link>
  );
}

function AccountMenuContent({
  auth,
  signingOut,
  onSignOut
}: {
  auth: AuthContext;
  signingOut: boolean;
  onSignOut: () => Promise<void>;
}) {
  return (
    <DropdownMenuContent align="end" className="w-72">
      <DropdownMenuGroup>
        <DropdownMenuLabel>
          <span className="flex flex-col gap-1">
            <span className="font-medium text-foreground">{auth.user_display_name || "Agentbox user"}</span>
            <span className="text-xs text-muted-foreground">{auth.actor_name}</span>
          </span>
        </DropdownMenuLabel>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuItem render={<Link href="/threads" />}>
          <BoxesIcon />
          Open inbox
        </DropdownMenuItem>
        {auth.is_owner ? (
          <DropdownMenuItem render={<Link href="/owner/users" />}>
            <ShieldIcon />
            Owner controls
          </DropdownMenuItem>
        ) : null}
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuItem variant="destructive" disabled={signingOut} onClick={() => void onSignOut()}>
          <LogOutIcon />
          {signingOut ? "Signing out" : "Sign out"}
        </DropdownMenuItem>
      </DropdownMenuGroup>
    </DropdownMenuContent>
  );
}
