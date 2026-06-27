import Link from "next/link";
import type { Metadata } from "next";
import { adminQuery, requireAdminFromSearch } from "@/src/core/admin";
import { getThread } from "@/src/core/db";

export const metadata: Metadata = {
  title: "Agentbox Thread",
  description: "Read-only Agentbox thread details."
};

type Props = {
  params: Promise<{ threadId: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

function first(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

export default async function ThreadPage({ params, searchParams }: Props) {
  const { threadId } = await params;
  const query = await searchParams;
  const adminKey = requireAdminFromSearch(new URLSearchParams({ admin_key: first(query.admin_key) ?? "" }));
  const thread = await getThread(threadId);

  if (!thread) {
    return (
      <main style={styles.shell}>
        <Link href={`/threads?${adminQuery(adminKey)}`} style={styles.back}>← Threads</Link>
        <p style={styles.empty}>Thread not found.</p>
      </main>
    );
  }

  return (
    <main style={styles.shell}>
      <header style={styles.header}>
        <Link href={`/threads?${adminQuery(adminKey)}`} style={styles.back}>← Threads</Link>
        <div>
          <p style={styles.eyebrow}>Read-only thread</p>
          <h1 style={styles.title}>{thread.title}</h1>
          <p style={styles.copy}>{thread.id} · Updated {new Date(thread.updated_at).toLocaleString()}</p>
        </div>
      </header>

      <section style={styles.messages}>
        {thread.messages.map((message) => (
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
  }
};
