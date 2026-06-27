"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

const STORAGE_KEY = "agentbox_admin_key";

type Asset = {
  id: string;
  file_name: string;
  mime_type: string | null;
  size_bytes: number;
};

type Message = {
  id: string;
  author: string;
  body: string;
  created_at: string;
  assets: Asset[];
};

type Thread = {
  id: string;
  title: string;
  updated_at: string;
  messages: Message[];
};

export function ThreadView({ threadId }: { threadId: string }) {
  const router = useRouter();
  const [thread, setThread] = useState<Thread | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const key = window.localStorage.getItem(STORAGE_KEY);
    if (!key) {
      router.replace("/threads");
      return;
    }

    async function loadThread(adminKey: string) {
      setLoading(true);
      setError(null);
      try {
        const response = await fetch(`/api/viewer/threads/${encodeURIComponent(threadId)}`, {
          headers: { "x-agentbox-admin-key": adminKey }
        });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
        setThread(data.thread);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    }

    void loadThread(key);
  }, [router, threadId]);

  return (
    <main style={styles.shell}>
      <header style={styles.header}>
        <Link href="/threads" style={styles.back}>← Inbox</Link>
        <div>
          <p style={styles.eyebrow}>Read-only thread</p>
          <h1 style={styles.title}>{thread?.title ?? "Thread"}</h1>
          <p style={styles.copy}>{thread?.id ?? threadId}{thread ? ` · Updated ${new Date(thread.updated_at).toLocaleString()}` : ""}</p>
        </div>
      </header>

      <section style={styles.messages}>
        {loading && <p style={styles.empty}>Loading thread…</p>}
        {error && (
          <div style={styles.errorCard}>
            <strong>Could not load thread.</strong>
            <span>{error}</span>
          </div>
        )}
        {!loading && !error && thread?.messages.length === 0 && <p style={styles.empty}>No messages yet.</p>}
        {!loading && !error && thread?.messages.map((message) => (
          <article key={message.id} style={styles.messageCard}>
            <div style={styles.messageHeader}>
              <strong>{message.author}</strong>
              <span>{new Date(message.created_at).toLocaleString()}</span>
            </div>
            <pre style={styles.body}>{message.body || "(empty message)"}</pre>
            {message.assets.length > 0 && (
              <div style={styles.assets}>
                <span style={styles.assetLabel}>Attachments</span>
                {message.assets.map((asset) => (
                  <div key={asset.id} style={styles.assetRow}>
                    <span>{asset.file_name}</span>
                    <span>{asset.mime_type ?? "unknown type"} · {asset.size_bytes} bytes</span>
                  </div>
                ))}
              </div>
            )}
          </article>
        ))}
      </section>
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
    fontSize: "clamp(34px, 5vw, 62px)",
    letterSpacing: "-0.055em",
    lineHeight: 0.95,
    fontFamily: "ui-serif, Georgia, Cambria, Times New Roman, Times, serif"
  },
  copy: {
    margin: 0,
    color: "#5e574f",
    fontSize: 15
  },
  messages: {
    maxWidth: 980,
    margin: "0 auto",
    display: "grid",
    gap: 14
  },
  messageCard: {
    display: "grid",
    gap: 14,
    padding: 18,
    border: "1px solid rgba(39, 31, 22, 0.1)",
    borderRadius: 20,
    background: "rgba(255, 251, 243, 0.72)"
  },
  messageHeader: {
    display: "flex",
    justifyContent: "space-between",
    gap: 16,
    color: "#756d63",
    fontSize: 13
  },
  body: {
    margin: 0,
    whiteSpace: "pre-wrap",
    overflowWrap: "break-word",
    color: "#29231d",
    lineHeight: 1.55,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace",
    fontSize: 13
  },
  assets: {
    display: "grid",
    gap: 8,
    borderTop: "1px solid rgba(39, 31, 22, 0.08)",
    paddingTop: 12
  },
  assetLabel: {
    color: "#a24f2f",
    fontSize: 12,
    fontWeight: 800,
    letterSpacing: "0.1em",
    textTransform: "uppercase"
  },
  assetRow: {
    display: "flex",
    justifyContent: "space-between",
    gap: 12,
    color: "#5e574f",
    fontSize: 13
  },
  empty: {
    maxWidth: 980,
    margin: "24px auto",
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
