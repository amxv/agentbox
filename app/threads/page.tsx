import Link from "next/link";
import type { Metadata } from "next";
import { adminQuery, requireAdminFromSearch } from "@/src/core/admin";
import { listThreads } from "@/src/core/db";

export const metadata: Metadata = {
  title: "Agentbox Threads",
  description: "Read-only Agentbox thread viewer."
};

type Props = {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

function first(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

export default async function ThreadsPage({ searchParams }: Props) {
  const params = await searchParams;
  const adminKey = requireAdminFromSearch(new URLSearchParams({ admin_key: first(params.admin_key) ?? "" }));
  const threads = await listThreads(100);

  return (
    <main style={styles.shell}>
      <header style={styles.header}>
        <Link href={`/?${adminQuery(adminKey)}`} style={styles.back}>Agentbox</Link>
        <div>
          <p style={styles.eyebrow}>Read-only viewer</p>
          <h1 style={styles.title}>Threads</h1>
          <p style={styles.copy}>A simple browser view for inspecting task history, messages, and attachments.</p>
        </div>
      </header>

      <section style={styles.list}>
        {threads.length === 0 ? (
          <p style={styles.empty}>No threads yet.</p>
        ) : threads.map((thread) => (
          <Link key={thread.id} href={`/threads/${thread.id}?${adminQuery(adminKey)}`} style={styles.threadCard}>
            <span style={styles.threadTitle}>{thread.title}</span>
            <span style={styles.threadMeta}>{thread.id}</span>
            <span style={styles.threadMeta}>Updated {new Date(thread.updated_at).toLocaleString()}</span>
          </Link>
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
    fontSize: "clamp(40px, 6vw, 72px)",
    letterSpacing: "-0.055em",
    lineHeight: 0.95,
    fontFamily: "ui-serif, Georgia, Cambria, Times New Roman, Times, serif"
  },
  copy: {
    margin: 0,
    maxWidth: 640,
    color: "#5e574f",
    fontSize: 17,
    lineHeight: 1.6
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
  }
};
