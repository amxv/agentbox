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
import { useEffect, useState } from "react";
import { Button, buttonVariants } from "@/components/ui/button";
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
import { AuthContext, fetchSession, signOutSession } from "./session";
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

export type AppNavProps = {
  title: string;
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
  const links = auth?.is_owner ? [...PRIMARY_LINKS, ...OWNER_LINKS] : PRIMARY_LINKS;
  const accountLabel = auth
    ? attributionLabel(auth.user_display_name, auth.actor_name)
    : "Loading session";

  async function handleSignOut() {
    setSigningOut(true);
    try {
      await signOutSession();
      window.location.href = "/login";
    } catch {
      setSigningOut(false);
    }
  }

  return (
    <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/85">
      <div className="mx-auto flex h-20 max-w-[1440px] items-center gap-5 px-5 sm:px-8 lg:px-10">
        <Link className="flex min-w-0 shrink-0 items-center gap-4" href="/threads" aria-label="Agentbox inbox">
          <AgentboxMark className="grid size-10 grid-cols-2 gap-1 border border-foreground bg-foreground p-1.5 [&>i]:block [&>i]:bg-background" />
          <span className="hidden min-w-0 flex-col sm:flex">
            <span className="font-heading text-base leading-none font-semibold tracking-[-0.025em]">Agentbox</span>
            <span className="mt-1.5 max-w-44 truncate font-mono text-[0.7rem] leading-none tracking-[0.12em] text-muted-foreground uppercase">
              {title}
            </span>
          </span>
        </Link>

        <div className="hidden min-w-0 flex-1 items-center gap-2 lg:flex">
          <nav className="flex min-w-0 items-center gap-2" aria-label="Main navigation">
            {PRIMARY_LINKS.map((link) => <DesktopNavLink key={link.href} link={link} pathname={pathname} />)}
          </nav>
          {auth?.is_owner ? (
            <>
              <span className="mx-3 h-8 w-px bg-border" aria-hidden="true" />
              <nav className="flex min-w-0 items-center gap-2" aria-label="Owner navigation">
                {OWNER_LINKS.map((link) => <DesktopNavLink key={link.href} link={link} pathname={pathname} owner />)}
              </nav>
            </>
          ) : null}
        </div>

        <div className="ml-auto hidden items-center gap-3 lg:flex">
          <ThemeSwitcher compact />
          {auth ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button variant="outline" className="max-w-56 justify-start" />
                }
              >
                <span className="flex size-5 shrink-0 items-center justify-center bg-foreground font-mono text-[0.58rem] font-semibold text-background">
                  {initials(auth.user_display_name)}
                </span>
                <span className="min-w-0 truncate">{accountLabel}</span>
                <ChevronDownIcon data-icon="inline-end" />
              </DropdownMenuTrigger>
              <AccountMenuContent auth={auth} signingOut={signingOut} onSignOut={handleSignOut} />
            </DropdownMenu>
          ) : null}
        </div>

        <div className="ml-auto flex items-center gap-3 lg:hidden">
          <ThemeSwitcher compact />
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button variant="outline" size="icon" />}>
              <MenuIcon />
              <span className="sr-only">Open navigation</span>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              <DropdownMenuLabel>
                <span className="flex flex-col gap-1">
                  <span className="font-heading text-sm font-semibold text-foreground">Agentbox</span>
                  <span className="font-mono text-[0.62rem] tracking-[0.1em] uppercase">{title}</span>
                </span>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                {links.map((link) => {
                  const Icon = link.icon;
                  const active = isActive(pathname, link.href);
                  return (
                    <DropdownMenuItem
                      key={link.href}
                      className={active ? "!bg-primary !text-primary-foreground focus:!bg-primary focus:!text-primary-foreground" : undefined}
                      render={<Link href={link.href} />}
                    >
                      <Icon />
                      {link.label}
                      {active ? <span className="ml-auto font-mono text-[0.58rem] tracking-[0.08em] uppercase">Current</span> : null}
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
              {auth ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuLabel>{accountLabel}</DropdownMenuLabel>
                  <DropdownMenuGroup>
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
      data-panel-nav-active={active ? "true" : "false"}
      className={cn(
        buttonVariants({ variant: active ? "default" : "ghost", size: "sm" }),
        "gap-2",
        active && "!text-primary-foreground hover:!text-primary-foreground",
        owner && !active && "opacity-70"
      )}
      href={link.href}
      aria-current={active ? "page" : undefined}
    >
      <Icon data-icon="inline-start" />
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
    <DropdownMenuContent align="end" className="w-64">
      <DropdownMenuLabel>
        <span className="flex flex-col gap-1">
          <span className="text-foreground">{auth.user_display_name || "Agentbox user"}</span>
          <span className="font-mono text-[0.62rem] tracking-[0.08em] uppercase">{auth.actor_name}</span>
        </span>
      </DropdownMenuLabel>
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
