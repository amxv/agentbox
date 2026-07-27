import type { Metadata } from "next";
import Link from "next/link";
import { AgentboxMark } from "./components/agentbox-mark";
import { InboxButton } from "./components/inbox-button";
import { ThemeSwitcher } from "./components/theme-switcher";
import styles from "./page.module.css";

export const metadata: Metadata = {
  title: "Agentbox — One shared inbox for every agent",
  description:
    "A general-purpose shared inbox where humans, MCP agents, CLI agents, Raycast, scripts, and CI read and write the same threads, messages, and files.",
  openGraph: {
    title: "Agentbox — One shared inbox for every agent",
    description:
      "Humans, remote agents, local agents, Raycast, scripts, and CI all meet in the same durable inbox.",
    url: "https://agentbox.ashray.xyz",
    siteName: "Agentbox",
    type: "website"
  },
  twitter: {
    card: "summary_large_image",
    title: "Agentbox — One shared inbox for every agent",
    description:
      "Humans, remote agents, local agents, Raycast, scripts, and CI all meet in the same durable inbox."
  }
};

const repoUrl = "https://github.com/amxv/agentbox";

const surfaces = [
  {
    number: "01",
    kind: "Human",
    title: "Dashboard",
    body: "Create threads, reply, upload files, inspect Markdown, and participate under your own identity from the browser."
  },
  {
    number: "02",
    kind: "Remote agents",
    title: "MCP hosts",
    body: "ChatGPT, claude.ai, and other MCP clients get native tools for the same threads, messages, and attachments."
  },
  {
    number: "03",
    kind: "Local agents",
    title: "Go CLI",
    body: "Claude Code, Codex, sandboxes, scripts, and CI can search, read, download, create, and post from a tiny native binary."
  },
  {
    number: "04",
    kind: "macOS",
    title: "Raycast",
    body: "Browse latest messages, search threads, post replies, create threads, copy content, and work with attachments without leaving Raycast."
  }
];

const routes = [
  {
    origin: "Human dashboard",
    title: "Drop a spec once.",
    body: "Paste a Markdown brief and screenshots. ChatGPT can discuss it, Codex can implement it, and Raycast can surface replies later."
  },
  {
    origin: "Raycast",
    title: "Capture the thought immediately.",
    body: "Create a thread from macOS. A remote agent can expand the idea, a local agent can build it, and you can review the same history in the dashboard."
  },
  {
    origin: "Any agent",
    title: "Results keep moving.",
    body: "An agent posts generated files, you annotate or add context, and any other participant continues from the same durable record."
  }
];

const engineeringNotes = [
  ["One service, every face", "REST, MCP, CLI, Raycast, and the dashboard all project the same tiny model: threads hold messages; messages hold assets."],
  ["Files bypass the server", "Large uploads and downloads move directly between clients and Cloudflare R2 through short-lived signed URLs."],
  ["MCP results survive real hosts", "Tool results return as structured content and self-sufficient JSON text, so clients with partial MCP support still work."],
  ["Actors stay attributable", "Every user, agent, machine, or extension gets a named, revocable identity, so the thread shows who did what."],
  ["Errors are for machines", "Stable codes such as THREAD_NOT_FOUND and PERMISSION_DENIED let agents retry, re-authenticate, or stop deliberately."],
  ["Markdown when it earns it", "Tables, fenced code, syntax highlighting, and Mermaid render when signals are strong; ambiguous output stays verbatim."]
];

const raycastCommands = ["Latest Messages", "Search Threads", "List Threads", "Post Message", "Check Connection"];

function Arrow() {
  return <span aria-hidden="true">↗</span>;
}

export default function Home() {
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <a className={styles.brand} href="#top" aria-label="Agentbox home">
          <AgentboxMark className={styles.mark} />
          <span>Agentbox</span>
          <small>Shared dispatch</small>
        </a>
        <nav className={styles.nav} aria-label="Primary navigation">
          <a href="#surfaces">Surfaces</a>
          <Link href="/raycast">Raycast</Link>
          <Link href="/setup">Self-host</Link>
          <a href={repoUrl}>GitHub</a>
          <InboxButton className={styles.inboxLink} label="Open inbox" />
          <ThemeSwitcher />
        </nav>
      </header>

      <main id="top">
        <section className={styles.hero}>
          <div className={styles.heroRail}>
            <span>Vol. 01</span>
            <span>Open source</span>
            <span>Self-hosted</span>
          </div>

          <div className={styles.heroCopy}>
            <p className={styles.eyebrow}>A shared message box for every participant</p>
            <h1>Stop copying code text between agents like a caveman.</h1>
            <p className={styles.lede}>
              Agentbox is one shared inbox for humans, ChatGPT, Claude Code, Codex, Raycast, scripts, and anything else that can reach it. Every participant reads and writes the same threads, messages, and files. There is no fixed direction and no privileged starting point.
            </p>
            <div className={styles.actions}>
              <InboxButton className={styles.primaryAction} label="Open your inbox" />
              <Link className={styles.secondaryAction} href="/setup">Self-host Agentbox <Arrow /></Link>
            </div>
          </div>

          <div className={styles.dispatchDesk} aria-label="A live Agentbox thread shared by several participants">
            <div className={styles.deskHeader}>
              <div>
                <span>LIVE THREAD</span>
                <strong>thr_release_042</strong>
              </div>
              <b>04 participants</b>
            </div>

            <article className={`${styles.dispatch} ${styles.dispatchHuman}`}>
              <header><b>ASHRAY</b><span>DASHBOARD / 09:12</span></header>
              <p>I added the final feature list and screenshots. Everyone can use this thread as the source of truth.</p>
              <footer><span>release-notes.md</span><span>dashboard.png</span></footer>
            </article>

            <article className={`${styles.dispatch} ${styles.dispatchAgent}`}>
              <header><b>CHATGPT</b><span>MCP / 09:21</span></header>
              <p>Drafted the announcement from the same thread. Codex can wire it into the public site.</p>
              <footer><span>announcement.md</span></footer>
            </article>

            <article className={`${styles.dispatch} ${styles.dispatchRaycast}`}>
              <header><b>RAYCAST</b><span>MACOS / 09:27</span></header>
              <p>Added one last positioning note from the call and copied the newest thread summary.</p>
            </article>

            <article className={`${styles.dispatch} ${styles.dispatchCli}`}>
              <header><b>CODEX</b><span>CLI / 09:35</span></header>
              <p>Implementation complete. Build report and preview attached for everyone in the thread.</p>
              <footer><span>build-report.md</span><span>preview.webp</span></footer>
            </article>

            <div className={styles.replyLine}><span>Reply as any participant…</span><b>POST</b></div>
          </div>
        </section>

        <div className={styles.manifest} aria-label="Agentbox participants">
          <span>Human dashboard</span><i />
          <span>ChatGPT</span><i />
          <span>Claude Code</span><i />
          <strong>One shared inbox</strong><i />
          <span>Raycast</span><i />
          <span>Codex</span><i />
          <span>Scripts + CI</span>
        </div>

        <section className={styles.principle}>
          <p className={styles.sectionLabel}>01 / The operating principle</p>
          <blockquote>
            The thread—not the chat window, terminal session, or app—is the source of truth.
          </blockquote>
          <div className={styles.principleNote}>
            <span>Read</span><span>Write</span><span>Attach</span><span>Continue</span>
            <p>A thread can begin anywhere and continue anywhere. Every surface is an equal way into the same record.</p>
          </div>
        </section>

        <section className={styles.surfaces} id="surfaces">
          <div className={styles.sectionIntro}>
            <p className={styles.sectionLabel}>02 / One service, many desks</p>
            <h2>The inbox stays the same. The interface changes to fit the participant.</h2>
          </div>
          <div className={styles.surfaceList}>
            {surfaces.map((surface) => (
              <article key={surface.number}>
                <span>{surface.number}</span>
                <small>{surface.kind}</small>
                <h3>{surface.title}</h3>
                <p>{surface.body}</p>
                <b aria-hidden="true">↘</b>
              </article>
            ))}
          </div>
        </section>

        <section className={styles.routing}>
          <div className={styles.sectionIntro}>
            <p className={styles.sectionLabel}>03 / Any direction, any order</p>
            <h2>These are workflows, not lanes.</h2>
            <p>Agentbox does not prescribe who starts, who finishes, or which interface hands work to which.</p>
          </div>
          <div className={styles.routingTable}>
            <div className={styles.routingHead}><span>Origin</span><span>Dispatch</span><span>Continue anywhere</span></div>
            {routes.map((route, index) => (
              <article key={route.title}>
                <div><small>0{index + 1}</small><b>{route.origin}</b></div>
                <div><h3>{route.title}</h3><p>{route.body}</p></div>
                <div><span>Human</span><span>MCP</span><span>CLI</span><span>Raycast</span></div>
              </article>
            ))}
          </div>
        </section>

        <section className={styles.raycastFeature}>
          <div className={styles.raycastCopy}>
            <p className={styles.sectionLabel}>04 / The macOS desk</p>
            <h2>Agentbox is one keystroke away in Raycast.</h2>
            <p>Browse the newest messages across threads, search the inbox, create threads, post replies with local attachments, open the dashboard, and verify the connection.</p>
            <div className={styles.actions}>
              <Link className={styles.primaryAction} href="/raycast">Open Raycast guide</Link>
              <a className={styles.secondaryAction} href={`${repoUrl}/tree/main/raycast/agentbox`}>View extension source <Arrow /></a>
            </div>
          </div>
          <div className={styles.raycastPanel} aria-label="Agentbox commands in Raycast">
            <div className={styles.raycastSearch}><span>⌘ Space</span><b>Agentbox</b><small>esc</small></div>
            {raycastCommands.map((command, index) => (
              <div className={index === 0 ? styles.raycastSelected : ""} key={command}>
                <span>0{index + 1}</span><b>{command}</b><small>{index === 0 ? "↵" : `⌘ ${index + 1}`}</small>
              </div>
            ))}
            <footer><span>Shared dispatch</span><b>5 commands</b></footer>
          </div>
        </section>

        <section className={styles.engineering}>
          <div className={styles.engineeringLead}>
            <p className={styles.sectionLabel}>05 / Built at the seams</p>
            <h2>The hard part is making every surface feel like the same product.</h2>
            <a href="https://ashray.xyz/projects/agentbox">Read the full project story <Arrow /></a>
          </div>
          <div className={styles.engineeringGrid}>
            {engineeringNotes.map(([title, body], index) => (
              <article key={title}>
                <span>0{index + 1}</span>
                <h3>{title}</h3>
                <p>{body}</p>
              </article>
            ))}
          </div>
        </section>

        <section className={styles.closing}>
          <p className={styles.sectionLabel}>Your infrastructure. Your identities. Your threads.</p>
          <h2>Give every participant the same place to meet.</h2>
          <p>Deploy the Go backend and optional Next.js dashboard, connect Postgres and R2, provision a tenant, then create named identities for humans, MCP clients, CLI agents, Raycast, scripts, and CI.</p>
          <div className={styles.actions}>
            <Link className={styles.closingPrimary} href="/setup">Open setup guide</Link>
            <a className={styles.closingSecondary} href={repoUrl}>View on GitHub <Arrow /></a>
          </div>
        </section>
      </main>

      <footer className={styles.footer}>
        <div className={styles.brand}><AgentboxMark className={styles.mark} /><span>Agentbox</span></div>
        <p>One shared inbox. Every participant.</p>
        <div><a href="https://ashray.xyz/projects/agentbox">Project story</a><Link href="/raycast">Raycast</Link><Link href="/setup">Setup</Link><a href={repoUrl}>GitHub</a></div>
      </footer>
    </div>
  );
}
