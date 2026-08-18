import type { Metadata } from "next";
import {
  IBM_Plex_Mono,
  Instrument_Sans,
  Manrope,
  Newsreader,
  Space_Grotesk
} from "next/font/google";
import "./globals.css";
import { cn } from "@/lib/utils";
import { AppShell } from "./components/app-shell";

/** shadcn design-system fonts: body text and headings for the signed-in panel. */
const sansFont = Instrument_Sans({
  variable: "--font-panel-sans",
  subsets: ["latin"],
  weight: "variable",
  display: "swap"
});

const headingFont = Newsreader({
  variable: "--font-panel-heading",
  subsets: ["latin"],
  weight: "variable",
  axes: ["opsz"],
  display: "swap"
});

/** Legacy fonts, still referenced by the public/unauthenticated stylesheets. */
const displayFont = Space_Grotesk({
  variable: "--font-agentbox-display",
  subsets: ["latin"],
  weight: "variable",
  display: "swap"
});

const bodyFont = Manrope({
  variable: "--font-agentbox-body",
  subsets: ["latin"],
  weight: "variable",
  display: "swap"
});

const monoFont = IBM_Plex_Mono({
  variable: "--font-agentbox-mono",
  subsets: ["latin"],
  weight: ["400", "500", "600"],
  display: "swap"
});

export const metadata: Metadata = {
  title: "Agentbox",
  description: "A shared thread inbox for ChatGPT, local agents, and the files that move between them."
};

/**
 * Applies the stored theme before first paint.
 *
 * Two systems have to stay in sync: the legacy stylesheet keys dark mode off
 * `data-theme` (falling back to the OS media query), while Tailwind/shadcn keys
 * off a `dark` class. This resolves the preference once and writes both.
 */
function ThemeInitScript() {
  const code = `(() => {
  try {
    const key = "agentbox_theme";
    const stored = window.localStorage.getItem(key);
    const theme = stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
    const root = document.documentElement;
    root.dataset.themePreference = theme;
    if (theme === "system") {
      root.removeAttribute("data-theme");
    } else {
      root.dataset.theme = theme;
    }
    const resolved = theme === "system"
      ? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")
      : theme;
    root.classList.toggle("dark", resolved === "dark");
  } catch {
  }
})();`;

  return <script dangerouslySetInnerHTML={{ __html: code }} />;
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={cn(sansFont.variable, headingFont.variable, displayFont.variable, bodyFont.variable, monoFont.variable)}
    >
      <head>
        <ThemeInitScript />
      </head>
      <body><AppShell>{children}</AppShell></body>
    </html>
  );
}
