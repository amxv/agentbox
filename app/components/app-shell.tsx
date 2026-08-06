"use client";

import type { ReactNode } from "react";
import { usePathname } from "next/navigation";
import { AppNav } from "./app-nav";
import { PanelPage } from "./panel-shell";
import { PanelSessionProvider } from "./panel-session";

const PANEL_ROUTES = [
  "/threads",
  "/keys",
  "/onboarding",
  "/raycast",
  "/owner/users",
  "/owner/content"
] as const;

function isPanelRoute(pathname: string) {
  return PANEL_ROUTES.some((route) => pathname === route || pathname.startsWith(`${route}/`));
}

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();

  if (!isPanelRoute(pathname)) return children;

  return (
    <PanelSessionProvider>
      <PanelPage>
        <AppNav />
        {children}
      </PanelPage>
    </PanelSessionProvider>
  );
}
