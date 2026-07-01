import type { Metadata } from "next";
import "./globals.css";

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
      <body>
        <ThemeInitScript />
        {children}
      </body>
    </html>
  );
}
