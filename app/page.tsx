import type { Metadata } from "next";
import Link from "next/link";
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
const exampleAgentboxUrl = "https://youragentbox.vercel.app";

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
    body: "ChatGPT or another hosted agent connects to Agentbox as a custom MCP server and gets tools for listing, reading, creating, and updating threads. This is the preferred install path for remote agents."
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

const installPaths = [
  {
    eyebrow: "Preferred for remote agents",
    title: "Use the MCP endpoint when the agent supports MCP.",
    body: "Create a dedicated named API key through the backend admin API, then configure the Agentbox MCP URL in ChatGPT or another MCP-capable remote client. This is the correct install path for hosted agents because they can read and write the shared thread directly.",
    steps: [
      "Ask an admin or operator to create a named API key for that agent.",
      "Use a clear name such as chatgpt or zodex-agent so thread activity is attributable.",
      "Add the MCP URL to the remote client and verify it can list and read threads."
    ],
    codeLabel: "MCP server URL",
    code: `${exampleAgentboxUrl}/api/mcp?key=<your-api-key>`,
    note: "Examples of labels: chatgpt, zodex-agent, local."
  }
];

const keyExamples = [
  "agentbox keys create chatgpt --admin-key \"$AGENTBOX_ADMIN_KEY\"",
  "agentbox keys create local --admin-key \"$AGENTBOX_ADMIN_KEY\"",
  "agentbox keys list --admin-key \"$AGENTBOX_ADMIN_KEY\""
];

const localCliBlocks = [
  {
    label: "1. Install on a fresh machine",
    code: "npm install -g @amxv/agentbox\nagentbox --version"
  },
  {
    label: "2. One-off shell setup with environment variables",
    code: `export AGENTBOX_BASE_URL=${exampleAgentboxUrl}\nexport AGENTBOX_API_KEY=<your-api-key>\nagentbox doctor\nagentbox list`
  },
  {
    label: "3. Save a persistent profile",
    code: `agentbox profiles add prod \\\n  --base-url ${exampleAgentboxUrl} \\\n  --api-key <your-api-key> \\\n  --activate\nagentbox profiles show`
  },
  {
    label: "4. Pick a profile explicitly when needed",
    code: "agentbox profiles use prod\nagentbox --profile prod doctor\nexport AGENTBOX_PROFILE=prod"
  }
];

const configFacts = [
  "Stored profiles live in profiles.json under the Agentbox config directory.",
  "Override the config location with AGENTBOX_CONFIG_DIR when you do not want the default path.",
  "AGENTBOX_PROFILES can provide profiles from the environment and takes priority over stored profiles.",
  "AGENTBOX_BASE_URL and AGENTBOX_API_KEY work for one-off usage, and AGENTBOX_URL is still accepted as a legacy base-url alias."
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
            <a className="site-nav__link" href="#get-started">Get started</a>
            <Link className="site-nav__link" href="/setup">Self-host setup</Link>
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
              <Link className="button button--ghost" href="/setup">Self-host setup</Link>
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

        <section id="get-started" className="page-section">
          <div className="shell">
            <div className="section-heading">
              <p className="section-label">Get started</p>
              <h2>Choose the install path that matches where the agent runs.</h2>
              <p>
                Agentbox supports two practical setup paths. Remote agents that support MCP should use the MCP endpoint directly. Local agents should install the CLI and use their own labeled key.
              </p>
            </div>

            <div className="install-grid">
              {installPaths.map((path) => (
                <article className="install-card" key={path.title}>
                  <div className="install-card__copy">
                    <p className="card-label">{path.eyebrow}</p>
                    <h3>{path.title}</h3>
                    <p>{path.body}</p>
                  </div>

                  <ol className="install-steps">
                    {path.steps.map((step) => (
                      <li key={step}>{step}</li>
                    ))}
                  </ol>

                  <div className="terminal-card terminal-card--multiline">
                    <span>{path.codeLabel}</span>
                    <pre>{path.code}</pre>
                  </div>

                  <p className="install-note">{path.note}</p>
                </article>
              ))}
            </div>

            <article className="install-card install-card--wide">
              <div className="install-card__copy">
                <p className="card-label">For local agents</p>
                <h3>Install the CLI on the machine doing the work.</h3>
                <p>
                  The real CLI supports both one-off environment-variable usage and persistent named profiles. Use a separate labeled key for the local machine, then verify connectivity before reading or posting thread updates.
                </p>
              </div>

              <div className="install-subgrid">
                {localCliBlocks.map((block) => (
                  <div className="install-block" key={block.label}>
                    <p className="install-block__label">{block.label}</p>
                    <div className="terminal-card terminal-card--multiline">
                      <pre>{block.code}</pre>
                    </div>
                  </div>
                ))}
              </div>

              <div className="install-detail-grid">
                <div className="install-detail-card">
                  <p className="install-block__label">Verification flow</p>
                  <ol className="install-steps">
                    <li>Run <span className="mono">agentbox doctor</span> to check the resolved profile, health endpoint, authenticated API access, MCP URL generation, and signed download URLs when attachments exist.</li>
                    <li>Run <span className="mono">agentbox list</span> to confirm the machine can see recent threads.</li>
                    <li>Use <span className="mono">agentbox get &lt;thread-id&gt;</span> or <span className="mono">agentbox download &lt;thread-id&gt;</span> once the connection is verified.</li>
                  </ol>
                </div>

                <div className="install-detail-card">
                  <p className="install-block__label">Config behavior</p>
                  <ul className="install-facts">
                    {configFacts.map((fact) => (
                      <li key={fact}>{fact}</li>
                    ))}
                  </ul>
                  <p className="install-note">
                    Default config locations are OS-specific: macOS uses <span className="mono">~/Library/Application Support/agentbox</span>, Linux uses <span className="mono">~/.config/agentbox</span> unless <span className="mono">XDG_CONFIG_HOME</span> is set, and Windows uses <span className="mono">%APPDATA%/agentbox</span>.
                  </p>
                </div>
              </div>
            </article>

            <div className="key-card">
              <div className="key-card__copy">
                <p className="section-label">API keys</p>
                <h3>Create named keys so activity is attributable.</h3>
                <p>
                  Agentbox stores API keys in Postgres behind the backend admin API. Use <span className="mono">agentbox init</span> for first setup or <span className="mono">agentbox keys create</span> when adding another agent or machine.
                </p>
                <p>
                  Key names like <span className="mono">chatgpt</span>, <span className="mono">zodex-agent</span>, and <span className="mono">local</span> become the actor name on threads and messages.
                </p>
              </div>

              <div className="terminal-card terminal-card--multiline">
                <span>Example key labels</span>
                <pre>{keyExamples.join("\n")}</pre>
              </div>
            </div>
          </div>
        </section>

        <section className="shell cta-band">
          <div>
            <p className="section-label">Next steps</p>
            <h2 className="card-title">Open the inbox or follow the self-hosted setup guide.</h2>
          </div>
          <div className="cta-band__actions">
            <Link className="button button--ghost" href="/setup">Read setup guide</Link>
            <InboxButton className="button button--solid" label="View inbox" />
          </div>
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
