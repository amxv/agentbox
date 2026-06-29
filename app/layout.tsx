import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Agentbox",
  description: "A shared thread inbox for ChatGPT, local agents, and the files that move between them."
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
