"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";

const STORAGE_KEY = "agentbox_admin_key";

type Thread = {
  id: string;
  title: string;
  updated_at: string;
  created_by: string;
};

export function InboxView() {
  const [key, setKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });
  const [draftKey, setDraftKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });
  const [threads, setThreads] = useState<Thread[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!key) return;

    async function loadThreads(adminKey: string) {
      setLoading(true);
      setError(null);
      try {
        const response = await fetch("/api/viewer/threads", {
          headers: { "x-agentbox-admin-key": adminKey }
        });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
        setThreads(data.threads ?? []);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    }

    void loadThreads(key);
  }, [key]);

  function saveKey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = draftKey.trim();
    if (!trimmed) return;
    window.localStorage.setItem(STORAGE_KEY, trimmed);
    setKey(trimmed);
  }

  function signOut() {
    window.localStorage.removeItem(STORAGE_KEY);
    setKey("");
    setDraftKey("");
    setThreads([]);
    setError(null);
  }

  return (
    <main style={styles.shell}>
      <header style={styles.header}>
        <Link href="/" style={styles.back}>Agentbox</Link>
        <div style={styles.headerRow}>
          <div>
            <p style={styles.eyebrow}>Read-only viewer</p>
            <h1 style={styles.title}>Inbox</h1>
            <p style={styles.copy}>Inspect task threads, messages, and attachments without putting your admin key in the URL.</p>
          </div>
          {key && <button type="button" style={styles.secondaryButton} onClick={signOut}>Forget key</button>}
        </div>
      </header>

      {!key ? (
        <form style={styles.signInCard} onSubmit={saveKey}>
          <div>
            <p style={styles.eyebrow}>Sign in</p>
            <h2 style={styles.cardTitle}>Enter your admin key</h2>
            <p style={styles.copy}>The key is saved in this browser and sent as a request header to the viewer API.</p>
          </div>
          <input
            value={draftKey}
            onChange={(event) => setDraftKey(event.target.value)}
            placeholder="ADMIN_KEY"
            type="password"
            style={styles.input}
          />
          <button type="submit" style={styles.primaryButton}>View inbox</button>
        </form>
      ) : (
        <section style={styles.list}>
          {loading && <p style={styles.empty}>Loading threads…</p>}
          {error && (
            <div style={styles.errorCard}>
              <strong>Could not load inbox.</strong>
              <span>{error}</span>
            </div>
          )}
          {!loading && !error && threads.length === 0 && <p style={styles.empty}>No threads yet.</p>}
          {!loading && !error && threads.map((thread) => (
            <Link key={thread.id} href={`/threads/${thread.id}`} style={styles.threadCard}>
              <span style={styles.threadTitle}>{thread.title}</span>
              <span style={styles.threadMeta}>{thread.id}</span>
              <span style={styles.threadMeta}>Updated {new Date(thread.updated_at).toLocaleString()}</span>
            </Link>
          ))}
        </section>
      )}
    </main>
  );
}

const styles: Record<string, React.CSSProperties> = {
  shell: {
    minHeight: "100vh",
    padding: "32px",
    background: "#f6f1e8",
    color: "#1c1915",
    fontFamily: "Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
  },
  header: {
    maxWidth: 980,
    margin: "0 auto 24px",
    display: "grid",
    gap: 18
  },
  headerRow: {
    display: "flex",
    justifyContent: "space-between",
    gap: 20,
    alignItems: "flex-start"
  },
  back: {
    width: "fit-content",
    color: "#a24f2f",
    textDecoration: "none",
    fontWeight: 700
  },
  eyebrow: {
    margin: "0 0 8px",
    color: "#a24f2f",
    fontSize: 12,
    fontWeight: 800,
    letterSpacing: "0.12em",
    textTransform: "uppercase"
  },
  title: {
    margin: "0 0 10px",
    fontSize: "clamp(40px, 6vw, 72px)",
    letterSpacing: "-0.055em",
    lineHeight: 0.95,
    fontFamily: "ui-serif, Georgia, Cambria, Times New Roman, Times, serif"
  },
  cardTitle: {
    margin: "0 0 10px",
    fontSize: 32,
    letterSpacing: "-0.045em",
    lineHeight: 1,
    fontFamily: "ui-serif, Georgia, Cambria, Times New Roman, Times, serif"
  },
  copy: {
    margin: 0,
    maxWidth: 640,
    color: "#5e574f",
    fontSize: 17,
    lineHeight: 1.6
  },
  signInCard: {
    maxWidth: 520,
    margin: "0 auto",
    display: "grid",
    gap: 16,
    padding: 24,
    border: "1px solid rgba(39, 31, 22, 0.1)",
    borderRadius: 24,
    background: "rgba(255, 251, 243, 0.72)"
  },
  input: {
    width: "100%",
    border: "1px solid rgba(39, 31, 22, 0.18)",
    borderRadius: 14,
    padding: "13px 14px",
    background: "#fff",
    color: "#1c1915",
    font: "inherit"
  },
  primaryButton: {
    width: "fit-content",
    border: 0,
    borderRadius: 999,
    padding: "12px 18px",
    background: "#1c1915",
    color: "#fffaf0",
    cursor: "pointer",
    fontWeight: 800
  },
  secondaryButton: {
    border: "1px solid rgba(39, 31, 22, 0.14)",
    borderRadius: 999,
    padding: "10px 14px",
    background: "rgba(255, 251, 243, 0.72)",
    color: "#1c1915",
    cursor: "pointer",
    fontWeight: 800
  },
  list: {
    maxWidth: 980,
    margin: "0 auto",
    display: "grid",
    gap: 12
  },
  threadCard: {
    display: "grid",
    gap: 6,
    padding: 18,
    border: "1px solid rgba(39, 31, 22, 0.1)",
    borderRadius: 20,
    background: "rgba(255, 251, 243, 0.72)",
    color: "inherit",
    textDecoration: "none"
  },
  threadTitle: {
    fontSize: 20,
    fontWeight: 800
  },
  threadMeta: {
    color: "#756d63",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace",
    fontSize: 12
  },
  empty: {
    color: "#756d63"
  },
  errorCard: {
    display: "grid",
    gap: 4,
    border: "1px solid rgba(162, 79, 47, 0.28)",
    borderRadius: 18,
    padding: 16,
    background: "rgba(162, 79, 47, 0.08)",
    color: "#7b371f"
  }
};
