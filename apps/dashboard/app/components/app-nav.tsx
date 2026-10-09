"use client";

import type { LucideIcon } from "lucide-react";
import {
  ChevronDownIcon,
  InboxIcon,
  KeyRoundIcon,
  LogOutIcon,
  MenuIcon,
  PlugZapIcon,
  RadarIcon,
  SearchIcon,
  UsersIcon
} from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { attributionLabel } from "./attribution";
import { usePanelSession } from "./panel-session";
import { signOutSession, type AuthContext } from "./session";
import { ThemeSwitcher } from "./theme-switcher";

type NavLink = {
  href: string;
  label: string;
  icon: LucideIcon;
};

const PRIMARY_LINKS: NavLink[] = [
  { href: "/threads", label: "Inbox", icon: InboxIcon },
  { href: "/keys", label: "Credentials", icon: KeyRoundIcon },
  { href: "/onboarding", label: "Connect agents", icon: PlugZapIcon },
  { href: "/raycast", label: "Raycast", icon: RadarIcon }
];

const OWNER_LINKS: NavLink[] = [
  { href: "/owner/users", label: "Users & teams", icon: UsersIcon },
  { href: "/owner/content", label: "All content", icon: SearchIcon }
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
  const isReadingPage = pathname?.startsWith("/threads/") || pathname?.startsWith("/owner/content/");
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
      <div className={cn(
        "mx-auto flex h-14 items-center gap-3 px-3 sm:h-16 sm:px-7 lg:h-[4.5rem] lg:gap-4 lg:px-8",
        isReadingPage ? "max-w-[1240px]" : "max-w-[1440px]"
      )}>
        <Link className="panel-brand" href="/threads" aria-label="Agentbox inbox">
          <Image className="panel-brand-mark" src="/icon.svg" width={32} height={32} alt="" unoptimized />
          <span className="panel-brand-name">Agentbox</span>
        </Link>

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
              <NavigationMenuContent auth={auth} links={links} pathname={pathname} signingOut={signingOut} onSignOut={handleSignOut} />
            </DropdownMenu>
          ) : (
            <Button variant="outline" className="w-64 justify-start" disabled>
              <span className="panel-account-avatar">AB</span>
              <span className="truncate">{loading ? "Loading account" : accountLabel}</span>
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
            <NavigationMenuContent auth={auth} links={links} pathname={pathname} signingOut={signingOut} onSignOut={handleSignOut} />
          </DropdownMenu>
        </div>
      </div>
    </header>
  );
}

function NavigationMenuContent({
  auth,
  links,
  pathname,
  signingOut,
  onSignOut
}: {
  auth: AuthContext | null;
  links: NavLink[];
  pathname: string | null;
  signingOut: boolean;
  onSignOut: () => Promise<void>;
}) {
  return (
    <DropdownMenuContent align="end" className="w-[min(19rem,calc(100vw-1rem))]">
      <DropdownMenuGroup>
        <DropdownMenuLabel>
          <span className="flex items-center gap-2.5 text-foreground">
            <Image className="panel-menu-mark" src="/icon.svg" width={24} height={24} alt="" unoptimized />
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
            <DropdownMenuLabel>{attributionLabel(auth.user_display_name, auth.actor_name)}</DropdownMenuLabel>
            <DropdownMenuItem variant="destructive" disabled={signingOut} onClick={() => void onSignOut()}>
              <LogOutIcon />
              {signingOut ? "Signing out" : "Sign out"}
            </DropdownMenuItem>
          </DropdownMenuGroup>
        </>
      ) : null}
    </DropdownMenuContent>
  );
}
