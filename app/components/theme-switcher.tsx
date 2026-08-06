"use client";

import { LaptopIcon, MoonIcon, SunIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

const STORAGE_KEY = "agentbox_theme";
type ThemeMode = "system" | "light" | "dark";
type ResolvedTheme = "light" | "dark";

function isThemeMode(value: string | null): value is ThemeMode {
  return value === "system" || value === "light" || value === "dark";
}

function readStoredTheme(): ThemeMode {
  if (typeof window === "undefined") return "system";
  const stored = window.localStorage.getItem(STORAGE_KEY);
  return isThemeMode(stored) ? stored : "system";
}

function prefersDark() {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/**
 * Writes both theme systems: `data-theme` for the legacy stylesheet and the
 * `dark` class for Tailwind/shadcn. Keep this in sync with the pre-paint
 * inline script in `app/layout.tsx`.
 */
function resolveTheme(mode: ThemeMode): ResolvedTheme {
  return mode === "system" ? (prefersDark() ? "dark" : "light") : mode;
}

function applyTheme(mode: ThemeMode): ResolvedTheme {
  const root = document.documentElement;
  root.dataset.themePreference = mode;
  if (mode === "system") {
    root.removeAttribute("data-theme");
  } else {
    root.dataset.theme = mode;
  }
  const resolved = resolveTheme(mode);
  root.classList.toggle("dark", resolved === "dark");
  return resolved;
}

export function ThemeSwitcher({ compact = false }: { compact?: boolean }) {
  const [mode, setMode] = useState<ThemeMode>("system");
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>("light");

  useEffect(() => {
    const stored = readStoredTheme();
    setMode(stored);
    setResolvedTheme(applyTheme(stored));
  }, []);

  useEffect(() => {
    if (mode !== "system") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setResolvedTheme(applyTheme("system"));
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [mode]);

  function selectMode(nextMode: ThemeMode) {
    setMode(nextMode);
    window.localStorage.setItem(STORAGE_KEY, nextMode);
    setResolvedTheme(applyTheme(nextMode));
    window.dispatchEvent(new CustomEvent("agentbox-theme-change", { detail: { mode: nextMode } }));
  }

  return (
    <ToggleGroup
      className={cn("app-theme-switcher", compact && "app-theme-switcher--compact")}
      data-resolved-theme={resolvedTheme}
      aria-label="Color theme"
      value={[mode]}
      variant="outline"
      size="sm"
      spacing={0}
      onValueChange={(value) => {
        const nextMode = value[0];
        if (isThemeMode(nextMode)) selectMode(nextMode);
      }}
    >
      <ToggleGroupItem
        className="app-theme-switcher__item"
        data-active={mode === "system" ? "true" : undefined}
        value="system"
        aria-label="Use system theme"
        title="System theme"
      >
        <LaptopIcon data-icon="inline-start" />
        {!compact ? <span className="sr-only">System</span> : null}
      </ToggleGroupItem>
      <ToggleGroupItem
        className="app-theme-switcher__item"
        data-active={mode === "light" ? "true" : undefined}
        value="light"
        aria-label="Use light theme"
        title="Light theme"
      >
        <SunIcon data-icon="inline-start" />
        {!compact ? <span className="sr-only">Light</span> : null}
      </ToggleGroupItem>
      <ToggleGroupItem
        className="app-theme-switcher__item"
        data-active={mode === "dark" ? "true" : undefined}
        value="dark"
        aria-label="Use dark theme"
        title="Dark theme"
      >
        <MoonIcon data-icon="inline-start" />
        {!compact ? <span className="sr-only">Dark</span> : null}
      </ToggleGroupItem>
    </ToggleGroup>
  );
}
