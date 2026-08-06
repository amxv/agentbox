"use client";

import type { ReactNode } from "react";
import { useEffect, useState } from "react";

const STORAGE_KEY = "agentbox_theme";
type ThemeMode = "system" | "light" | "dark";

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
function applyTheme(mode: ThemeMode) {
  const root = document.documentElement;
  root.dataset.themePreference = mode;
  if (mode === "system") {
    root.removeAttribute("data-theme");
  } else {
    root.dataset.theme = mode;
  }
  const resolved = mode === "system" ? (prefersDark() ? "dark" : "light") : mode;
  root.classList.toggle("dark", resolved === "dark");
}

export function ThemeSwitcher() {
  const [mode, setMode] = useState<ThemeMode>("system");

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      const stored = readStoredTheme();
      setMode(stored);
      applyTheme(stored);
    }, 0);
    return () => window.clearTimeout(timeout);
  }, []);

  // While following the OS, re-resolve when the OS preference flips.
  useEffect(() => {
    if (mode !== "system") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => applyTheme("system");
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [mode]);

  function selectMode(nextMode: ThemeMode) {
    setMode(nextMode);
    window.localStorage.setItem(STORAGE_KEY, nextMode);
    applyTheme(nextMode);
    window.dispatchEvent(new CustomEvent("agentbox-theme-change", { detail: { mode: nextMode } }));
  }

  return (
    <div className="theme-switcher" role="group" aria-label="Color theme">
      <ThemeButton active={mode === "system"} label="Use system theme" onClick={() => selectMode("system")}>
        <SystemIcon />
      </ThemeButton>
      <ThemeButton active={mode === "light"} label="Use light theme" onClick={() => selectMode("light")}>
        <SunIcon />
      </ThemeButton>
      <ThemeButton active={mode === "dark"} label="Use dark theme" onClick={() => selectMode("dark")}>
        <MoonIcon />
      </ThemeButton>
    </div>
  );
}

function ThemeButton({ active, label, onClick, children }: { active: boolean; label: string; onClick: () => void; children: ReactNode }) {
  return (
    <button
      aria-label={label}
      aria-pressed={active}
      className="theme-switcher__button"
      title={label}
      type="button"
      onClick={onClick}
    >
      {children}
    </button>
  );
}

function SystemIcon() {
  return (
    <svg aria-hidden="true" fill="none" height="16" viewBox="0 0 24 24" width="16">
      <rect height="12" rx="2" stroke="currentColor" strokeWidth="2" width="18" x="3" y="4" />
      <path d="M8 20h8" stroke="currentColor" strokeLinecap="round" strokeWidth="2" />
      <path d="M12 16v4" stroke="currentColor" strokeLinecap="round" strokeWidth="2" />
    </svg>
  );
}

function SunIcon() {
  return (
    <svg aria-hidden="true" fill="none" height="16" viewBox="0 0 24 24" width="16">
      <circle cx="12" cy="12" r="4" stroke="currentColor" strokeWidth="2" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" stroke="currentColor" strokeLinecap="round" strokeWidth="2" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg aria-hidden="true" fill="none" height="16" viewBox="0 0 24 24" width="16">
      <path d="M20.5 14.5A8.5 8.5 0 0 1 9.5 3.5 8.5 8.5 0 1 0 20.5 14.5Z" stroke="currentColor" strokeLinejoin="round" strokeWidth="2" />
    </svg>
  );
}
