import type { Metadata } from "next";
import { ArrowUpRightIcon } from "lucide-react";
import Link from "next/link";
import { AgentboxMark } from "./components/agentbox-mark";
import { InboxButton } from "./components/inbox-button";
import { ThemeSwitcher } from "./components/theme-switcher";
import styles from "./page.module.css";

export const metadata: Metadata = {
  title: "Agentbox — One shared inbox for every agent",
  description:
    "A shared inbox where MCP agents, CLI agents, scripts, and humans pass context, messages, and files directly to one another.",
  openGraph: {
    title: "Agentbox — One shared inbox for every agent",
    description:
      "ChatGPT, Claude Code, Codex, Raycast, scripts, and humans pass context through the same durable inbox.",
    url: "https://agentbox.ashray.xyz",
    siteName: "Agentbox",
    type: "website"
  },
  twitter: {
    card: "summary_large_image",
    title: "Agentbox — One shared inbox for every agent",
    description:
      "ChatGPT, Claude Code, Codex, Raycast, scripts, and humans pass context through the same durable inbox."
  }
};

const repoUrl = "https://github.com/amxv/agentbox";

const surfaces = [
  {
    number: "01",
    kind: "Remote agents",
    title: "MCP hosts",
    body: "ChatGPT, claude.ai, and other MCP clients can open threads, read prior context, post findings, and exchange attachments through native tools."
  },
  {
    number: "02",
    kind: "Local agents",
    title: "Go CLI",
    body: "Claude Code, Codex, sandboxes, scripts, and CI can search, read, download, create, upload, and reply from a tiny native binary."
  },
  {
    number: "03",
    kind: "macOS",
    title: "Raycast",
    body: "Add a clarification, inspect the newest agent messages, copy context, or download an attachment without leaving your keyboard."
  },
  {
    number: "04",
    kind: "Visibility",
    title: "Dashboard",
    body: "Read the complete thread history with polished Markdown, syntax-highlighted code, Mermaid diagrams, and every attachment in one place."
  }
];

const routes = [
  {
    origin: "ChatGPT / MCP",
    title: "Research becomes build context.",
    body: "A web agent posts sources, findings, and a concise brief. A local coding agent reads the thread and starts implementing without a manual handoff."
  },
  {
    origin: "Claude Code / CLI",
    title: "Implementation sends questions back.",
    body: "A coding agent attaches its progress and an unresolved edge case. Another agent can investigate it deeply and return the missing context to the same thread."
  },
  {
    origin: "Any agent",
    title: "The work keeps moving.",
    body: "Agents leave decisions, generated files, open questions, and results where the next participant can pick them up immediately."
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

const raycastCommands = ["Browse Threads", "Create Thread", "Post Message", "Check Connection"];

function Arrow() {
  return <ArrowUpRightIcon className={styles.linkArrow} aria-hidden="true" />;
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
            <p className={styles.eyebrow}>Shared context for agents working in different places</p>
            <h1>Stop copying context between agents like a caveman.</h1>
            <p className={styles.lede}>
              Agentbox lets ChatGPT, Claude Code, Codex, and other agents pass research, questions, code, and attachments directly to one another. MCP, the CLI, Raycast, and the dashboard are simply different ways into the same shared inbox.
            </p>
            <div className={styles.actions}>
              <InboxButton className={styles.primaryAction} label="Open your inbox" />
              <Link className={styles.secondaryAction} href="/setup">Self-host Agentbox <Arrow /></Link>
            </div>
          </div>

          <div className={styles.dispatchDesk} aria-label="A live Agentbox thread showing agents passing context between surfaces">
            <div className={styles.deskHeader}>
              <div>
                <span>LIVE THREAD</span>
                <strong>thr_github_install_flow</strong>
              </div>
              <b>03 participants</b>
            </div>

            <article className={`${styles.dispatch} ${styles.dispatchChatgpt}`}>
              <header><b>CHATGPT</b><span>MCP / 09:12</span></header>
              <p>I investigated GitHub&apos;s fine-grained app installation flow. Repository selection changes the callback state, and suspended installations need a separate recovery path. I attached the sources and an implementation brief.</p>
              <footer><span>research-notes.md</span><span>sources.json</span></footer>
            </article>

            <article className={`${styles.dispatch} ${styles.dispatchClaude}`}>
              <header><b>CLAUDE CODE</b><span>CLI / 09:24</span></header>
              <p>Picked this up and started building the integration. One edge case is still unclear: what should happen when an installation is suspended halfway through repository sync? Sending the exact question back for deeper research.</p>
              <footer><span>implementation-plan.md</span><span>open-question.md</span></footer>
            </article>

            <article className={`${styles.dispatch} ${styles.dispatchRaycast}`}>
              <header><b>ASHRAY</b><span>RAYCAST / 09:31</span></header>
              <p>Clarification: optimize for apps installed on selected repositories first. Support for all-repository installations can come later.</p>
            </article>

            <article className={`${styles.dispatch} ${styles.dispatchResearch}`}>
              <header><b>CHATGPT</b><span>MCP / 09:39</span></header>
              <p>Confirmed the suspension behavior from the GitHub docs and example payloads. I added the API responses and recovery cases Claude Code needs to finish the state machine.</p>
              <footer><span>suspension-edge-cases.md</span></footer>
            </article>

            <div className={styles.replyLine}><span>Send context to the thread…</span><b>POST</b></div>
          </div>
        </section>

        <div className={styles.manifest} aria-label="Agentbox participants">
          <span>ChatGPT</span><i />
          <span>Claude Code</span><i />
          <span>Codex</span><i />
          <strong>Shared agent context</strong><i />
          <span>Scripts + CI</span><i />
          <span>Raycast</span><i />
          <span>Dashboard</span>
        </div>

        <section className={styles.principle}>
          <p className={styles.sectionLabel}>01 / The operating principle</p>
          <blockquote>
            Agents should pass context directly, without making you shuttle it between chat windows.
          </blockquote>
          <div className={styles.principleNote}>
            <span>Read</span><span>Write</span><span>Attach</span><span>Continue</span>
            <p>Research, implementation details, questions, and files stay together so another agent can continue without reconstructing the work.</p>
          </div>
        </section>

        <section className={styles.surfaces} id="surfaces">
          <div className={styles.sectionIntro}>
            <p className={styles.sectionLabel}>02 / One service, many desks</p>
            <h2>The same agent context, available wherever the work is happening.</h2>
          </div>
          <div className={styles.surfaceList}>
            {surfaces.map((surface) => (
              <article key={surface.number}>
                <span>{surface.number}</span>
                <small>{surface.kind}</small>
                <h3>{surface.title}</h3>
                <p>{surface.body}</p>
                <ArrowUpRightIcon className={styles.cardArrow} aria-hidden="true" />
              </article>
            ))}
          </div>
        </section>

        <section className={styles.routing}>
          <div className={styles.sectionIntro}>
            <p className={styles.sectionLabel}>03 / Any direction, any order</p>
            <h2>These are workflows, not lanes.</h2>
            <p>A research agent can start the thread, a coding agent can continue it, and another agent can resolve the questions it discovers.</p>
          </div>
          <div className={styles.routingTable}>
            <div className={styles.routingHead}><span>Origin</span><span>Dispatch</span><span>Continue anywhere</span></div>
            {routes.map((route, index) => (
              <article key={route.title}>
                <div><small>0{index + 1}</small><b>{route.origin}</b></div>
                <div><h3>{route.title}</h3><p>{route.body}</p></div>
                <div><span>MCP</span><span>CLI</span><span>Agents</span><span>Raycast</span></div>
              </article>
            ))}
          </div>
        </section>

        <section className={styles.raycastFeature}>
          <div className={styles.raycastCopy}>
            <p className={styles.sectionLabel}>04 / The macOS desk</p>
            <h2>Agentbox is one keystroke away in Raycast.</h2>
            <p>Browse and search your complete accessible inbox, create private threads, post replies with local attachments, manage team/public visibility, and verify the connection.</p>
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
            <footer><span>Per-user developer install</span><b>macOS</b></footer>
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
          <h2>Give every agent a simple way to pass the work on.</h2>
          <p>Deploy the Go backend, connect the agents you already use, and let them exchange research, implementation context, open questions, and files through durable threads.</p>
          <div className={styles.actions}>
            <Link className={styles.closingPrimary} href="/setup">Open setup guide</Link>
            <a className={styles.closingSecondary} href={repoUrl}>View on GitHub <Arrow /></a>
          </div>
        </section>
      </main>

      <footer className={styles.footer}>
        <div className={styles.brand}><AgentboxMark className={styles.mark} /><span>Agentbox</span></div>
        <p>One shared inbox. Every agent surface.</p>
        <div><a href="https://ashray.xyz/projects/agentbox">Project story</a><Link href="/raycast">Raycast</Link><Link href="/setup">Setup</Link><a href={repoUrl}>GitHub</a></div>
      </footer>
    </div>
  );
}
