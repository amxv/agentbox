import type { Metadata } from "next";
import { Space_Grotesk, Manrope, IBM_Plex_Mono } from "next/font/google";
import "./globals.css";

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
  } catch {
  }
})();`;

  return <script dangerouslySetInnerHTML={{ __html: code }} />;
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className={`${displayFont.variable} ${bodyFont.variable} ${monoFont.variable}`}>
        <ThemeInitScript />
        {children}
      </body>
    </html>
  );
}
