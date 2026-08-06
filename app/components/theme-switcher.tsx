"use client";

import { LaptopIcon, MoonIcon, SunIcon } from "lucide-react";
import { useEffect, useSyncExternalStore } from "react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

const STORAGE_KEY = "agentbox_theme";
const THEME_CHANGE_EVENT = "agentbox-theme-change";
type ThemeMode = "system" | "light" | "dark";
type ResolvedTheme = "light" | "dark";

function isThemeMode(value: string | null): value is ThemeMode {
  return value === "system" || value === "light" || value === "dark";
}

function readStoredTheme(): ThemeMode {
  if (typeof window === "undefined") return "system";
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    return isThemeMode(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

function prefersDark(): boolean {
  return typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/**
 * Writes both theme systems: `data-theme` for the legacy stylesheet and the
 * `dark` class for Tailwind/shadcn. Keep this in sync with the pre-paint
 * inline script in `app/layout.tsx`.
 */
function resolveTheme(mode: ThemeMode): ResolvedTheme {
  return mode === "system" ? (prefersDark() ? "dark" : "light") : mode;
}

function applyTheme(mode: ThemeMode, resolved = resolveTheme(mode)): ResolvedTheme {
  const root = document.documentElement;
  root.dataset.themePreference = mode;
  if (mode === "system") {
    root.removeAttribute("data-theme");
  } else {
    root.dataset.theme = mode;
  }
  root.classList.toggle("dark", resolved === "dark");
  return resolved;
}

function subscribeThemePreference(onStoreChange: () => void) {
  const onStorage = (event: StorageEvent) => {
    if (event.key === STORAGE_KEY) onStoreChange();
  };
  window.addEventListener("storage", onStorage);
  window.addEventListener(THEME_CHANGE_EVENT, onStoreChange);
  return () => {
    window.removeEventListener("storage", onStorage);
    window.removeEventListener(THEME_CHANGE_EVENT, onStoreChange);
  };
}

function subscribeSystemTheme(onStoreChange: () => void) {
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  media.addEventListener("change", onStoreChange);
  return () => media.removeEventListener("change", onStoreChange);
}

export function ThemeSwitcher({ compact = false }: { compact?: boolean }) {
  const mode = useSyncExternalStore<ThemeMode>(
    subscribeThemePreference,
    readStoredTheme,
    (): ThemeMode => "system"
  );
  const systemPrefersDark = useSyncExternalStore<boolean>(
    subscribeSystemTheme,
    prefersDark,
    () => false
  );
  const resolvedTheme: ResolvedTheme = mode === "system"
    ? (systemPrefersDark ? "dark" : "light")
    : mode;

  useEffect(() => {
    applyTheme(mode, resolvedTheme);
  }, [mode, resolvedTheme]);

  function selectMode(nextMode: ThemeMode) {
    try {
      window.localStorage.setItem(STORAGE_KEY, nextMode);
    } catch {
      // The theme still applies for this page even when storage is unavailable.
    }
    applyTheme(nextMode);
    window.dispatchEvent(new CustomEvent(THEME_CHANGE_EVENT, { detail: { mode: nextMode } }));
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
