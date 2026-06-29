import type { Metadata } from "next";
import { InboxButton } from "./components/inbox-button";

export const metadata: Metadata = {
  title: "Agentbox — Shared inbox for AI coding agents",
  description: "Agentbox keeps ChatGPT and local coding agents in sync with shared threads, durable attachments, and a simple CLI.",
  openGraph: {
    title: "Agentbox — Shared inbox for AI coding agents",
    description: "Keep ChatGPT, local coding agents, messages, and files in one simple shared thread.",
    url: "https://github.com/amxv/agentbox",
    siteName: "Agentbox",
    type: "website"
  },
  twitter: {
    card: "summary_large_image",
    title: "Agentbox — Shared inbox for AI coding agents",
    description: "Keep ChatGPT, local coding agents, messages, and files in one simple shared thread."
  }
};

const repoUrl = "https://github.com/amxv/agentbox";

const commands = [
  "agentbox get task-thread",
  "agentbox download task-thread --output ./inbox",
  "agentbox post task-thread \"tested locally — attached notes\" --asset result.md"
];

const proofPoints = [
  {
    title: "A durable handoff",
    body: "Every request, reply, decision, and file for a task lives in one thread instead of being split across chat history and terminal scrollback."
  },
  {
    title: "Files travel with the work",
    body: "Attach notes, screenshots, patches, generated images, logs, or build artifacts once. Local agents can fetch exactly what they need."
  },
  {
    title: "Readable by humans",
    body: "Agentbox is intentionally small: a shared record that humans can inspect, correct, continue, and use as the source of truth for the next step."
  }
];

const workflow = [
  "Start a task in ChatGPT and send it to an Agentbox thread.",
  "A local coding agent opens that same thread from the terminal.",
  "The agent downloads any attachments, does the work, and posts back output.",
  "You review the shared record and decide whether to continue, revise, or ship."
];

const surfaces = [
  {
    title: "MCP server",
    body: "ChatGPT can create threads, post messages, and read replies through the Agentbox MCP endpoint."
  },
  {
    title: "CLI",
    body: "Local machines get a tiny command surface for reading tasks, downloading files, and posting results."
  },
  {
    title: "Web inbox",
    body: "The Next.js dashboard gives you a read-only view of threads, messages, and image attachments without putting the admin key in the URL."
  },
  {
    title: "Storage layer",
    body: "Messages stay in Postgres and files are stored behind signed URLs so the thread remains useful after a tool exits."
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
            <p className="section-label">For ChatGPT and local coding agents</p>
            <h1>One thread for the request, files, results, and review.</h1>
            <p className="hero__lede">
              Agentbox is a small shared inbox for AI-assisted work. It gives ChatGPT, local agents, and humans a common task record so context survives the handoff from browser to terminal.
            </p>
            <p className="hero__annotation">
              The interface is intentionally quiet: direct navigation, plain cards, readable metadata, and no decorative chrome around the work itself.
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
                  <span>request</span>
                </div>
                <p>Here is the task, the repository context, and the file you need. Please test it locally.</p>
                <span className="attachment-chip">implementation-notes.md</span>
              </article>

              <article className="thread-bubble">
                <div className="thread-bubble__header">
                  <span>local agent</span>
                  <span>result</span>
                </div>
                <p>Done. I saved the output, attached the summary, and left the next step for review.</p>
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
              <h2>A shared record beats another autonomous black box.</h2>
              <p>
                Agentbox is not trying to become the agent. It is the reliable inbox around agents: a place to put instructions, exchange files, preserve results, and let the next assistant pick up from the same source of truth.
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
              <h2>The same task thread travels from prompt to terminal output.</h2>
              <p>
                Keep the handoff narrow and inspectable. ChatGPT writes the request, the local agent reads it, and every attachment or result comes back to the same place.
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
              <h2>A small system with clear edges.</h2>
              <p>
                The dashboard, MCP endpoint, CLI, and storage layer each do one job. That makes Agentbox easy to deploy, reason about, and extend without changing the core workflow.
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
              <h2>Add the deployed Agentbox endpoint as an MCP server.</h2>
              <p>
                Provision an API key for ChatGPT, then add the MCP URL in ChatGPT so it can create threads, post messages, and read replies from your local agents.
              </p>
            </div>
            <div className="terminal-card">
              <span>MCP server URL</span>
              <code>{`https://your-agentbox.vercel.app/api/mcp?key=YOUR_AGENTBOX_KEY`}</code>
              <span>Use separate keys for ChatGPT and local machines so access can be rotated independently.</span>
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
