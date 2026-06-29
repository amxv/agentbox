import type { Metadata } from "next";
import { InboxButton } from "./components/inbox-button";

export const metadata: Metadata = {
  title: "Agentbox — Shared inbox for remote and local agents",
  description: "Agentbox moves conversations, files, and results between ChatGPT, local coding agents, and a simple web inbox.",
  openGraph: {
    title: "Agentbox — Shared inbox for remote and local agents",
    description: "Move a task from ChatGPT to a local coding agent without becoming the clipboard.",
    url: "https://github.com/amxv/agentbox",
    siteName: "Agentbox",
    type: "website"
  },
  twitter: {
    card: "summary_large_image",
    title: "Agentbox — Shared inbox for remote and local agents",
    description: "Move a task from ChatGPT to a local coding agent without becoming the clipboard."
  }
};

const repoUrl = "https://github.com/amxv/agentbox";

const commands = [
  "agentbox --profile ashray get task-thread",
  "agentbox --profile ashray download task-thread --output ./inbox",
  "agentbox --profile ashray post task-thread \"tested locally — attached notes\" --asset result.md"
];

const proofPoints = [
  {
    title: "Get out of the middle",
    body: "Start with a browser conversation, send it to Agentbox, and let a local coding agent open the same thread without you rebuilding the prompt by hand."
  },
  {
    title: "Files travel with the work",
    body: "Uploaded files, generated images, notes, logs, and results stay attached to the thread. Local agents can download them when the work begins."
  },
  {
    title: "Still readable by humans",
    body: "The web inbox keeps the shared record visible, so you can inspect what moved between agents before deciding what happens next."
  }
];

const workflow = [
  "ChatGPT creates or updates an Agentbox thread with the task, context, and files.",
  "Claude Code, Codex, or another local agent opens that same thread through the CLI.",
  "The local agent downloads attachments, does the work, and posts results back.",
  "You review the shared inbox instead of stitching together chat history and terminal output."
];

const surfaces = [
  {
    title: "Remote agents use MCP",
    body: "ChatGPT or another hosted agent connects to Agentbox as a custom MCP server and gets tools for listing, reading, creating, and updating threads."
  },
  {
    title: "Local agents use the CLI",
    body: "The Go CLI keeps named profiles, checks the connection, reads tasks, downloads attachments, and posts results from the machine doing the work."
  },
  {
    title: "Humans use the dashboard",
    body: "The Next.js dashboard is a simple viewer for threads, messages, and attachments. It is for inspection, not for replacing the agents."
  },
  {
    title: "Files stay durable",
    body: "Messages live in Postgres, attachments live in Cloudflare R2, and downloads use signed URLs so large files do not have to pass through the app."
  }
];

export default function Home() {
  return (
    <>
      <header className="site-header">
        <div className="shell site-header__inner">
          <a className="brand" href="#top" aria-label="Agentbox home">
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">Shared inbox for AI work</span>
          </a>
          <nav className="site-nav" aria-label="Primary navigation">
            <a className="site-nav__link" href="#workflow">Workflow</a>
            <a className="site-nav__link" href="#connect">Connect</a>
            <InboxButton className="site-nav__link" label="View inbox" />
            <a className="site-nav__link" href={repoUrl}>GitHub</a>
          </nav>
        </div>
      </header>

      <main id="top">
        <section className="hero shell">
          <div>
            <p className="section-label">For remote and local agents</p>
            <h1>A shared inbox for the conversations you actually want to build from.</h1>
            <p className="hero__lede">
              Agentbox is a small message bus for AI-assisted work. ChatGPT can drop a task, files, and context into a thread; Claude Code, Codex, or another local agent can pick it up from the terminal and post the result back.
            </p>
            <p className="hero__annotation">
              This repo now runs as a Go backend, Go CLI, MCP server, and Next.js dashboard, but the product idea stays simple: one durable place for remote agents, local agents, files, and review.
            </p>
            <div className="hero__actions">
              <InboxButton className="button button--solid" label="View inbox" />
              <a className="button button--ghost" href={repoUrl}>Get the code</a>
            </div>
          </div>

          <aside className="hero-panel" aria-label="Agentbox thread preview">
            <div className="hero-panel__top">
              <div>
                <p className="card-label">Thread preview</p>
                <p className="thread-title">task-thread</p>
              </div>
              <span className="status-dot" aria-hidden="true" />
            </div>

            <div className="thread-preview">
              <article className="thread-bubble">
                <div className="thread-bubble__header">
                  <span>ChatGPT</span>
                  <span>MCP</span>
                </div>
                <p>I turned this conversation into a task thread. It includes the goal, decisions, and the files the coding agent needs.</p>
                <span className="attachment-chip">chatgpt-context.md</span>
              </article>

              <article className="thread-bubble">
                <div className="thread-bubble__header">
                  <span>local agent</span>
                  <span>CLI</span>
                </div>
                <p>Done. I pulled the thread, used the attachments, ran the checks, and posted the result here for review.</p>
                <span className="attachment-chip">result-summary.md</span>
              </article>
            </div>

            <div className="terminal-card" aria-label="CLI commands">
              {commands.map((command) => (
                <code key={command}>$ {command}</code>
              ))}
            </div>
          </aside>
        </section>

        <section className="page-section">
          <div className="shell">
            <div className="section-heading">
              <p className="section-label">Why it exists</p>
              <h2>Most useful work starts in one place and gets built somewhere else.</h2>
              <p>
                A good ChatGPT thread often contains the brief, the constraints, the tradeoffs, and the files. Agentbox keeps that bundle intact when the work moves to a local coding agent.
              </p>
            </div>
            <div className="proof-grid">
              {proofPoints.map((item) => (
                <article className="card" key={item.title}>
                  <h3>{item.title}</h3>
                  <p>{item.body}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section id="workflow" className="page-section">
          <div className="shell split-section">
            <div>
              <p className="section-label">Workflow</p>
              <h2>The thread becomes the handoff.</h2>
              <p>
                Instead of copying a prompt, downloading files, and pasting logs back into another chat, each participant reads and writes the same task record.
              </p>
            </div>
            <div className="stack-list">
              {workflow.map((item) => (
                <article className="stack-list__item" key={item}>
                  <p>{item}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section id="connect" className="page-section">
          <div className="shell">
            <div className="section-heading">
              <p className="section-label">Surfaces</p>
              <h2>Remote agents, local agents, and humans each get the surface they need.</h2>
              <p>
                Agentbox is intentionally small. It does not try to run the work itself; it makes the handoff between tools reliable, inspectable, and repeatable.
              </p>
            </div>
            <div className="capability-grid">
              {surfaces.map((surface) => (
                <div key={surface.title}>
                  <p className="capability-title">{surface.title}</p>
                  <p className="copy">{surface.body}</p>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="page-section">
          <div className="shell split-section">
            <div>
              <p className="section-label">Connect ChatGPT</p>
              <h2>Add Agentbox as a custom MCP server.</h2>
              <p>
                Give ChatGPT a dedicated key, then add the deployed MCP URL. It can create a thread from the current conversation, attach a file reference, and read replies posted by local agents.
              </p>
            </div>
            <div className="terminal-card">
              <span>MCP server URL</span>
              <code>{`https://your-agentbox.vercel.app/api/mcp?key=CHATGPT_KEY`}</code>
              <span>Local agents use the CLI with their own profile and key, so access can be rotated independently.</span>
            </div>
          </div>
        </section>

        <section className="shell cta-band">
          <div>
            <p className="section-label">Dashboard</p>
            <h2 className="card-title">Open the web inbox to review live threads.</h2>
          </div>
          <InboxButton className="button button--solid" label="View inbox" />
        </section>
      </main>

      <footer className="site-footer">
        <div className="shell site-footer__inner">
          <span>A lightweight inbox for AI-assisted work.</span>
          <a href={repoUrl}>github.com/amxv/agentbox</a>
        </div>
      </footer>
    </>
  );
}
