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
  "agentbox post task-thread \"I tested it and attached the result\" --asset result.md"
];

const features = [
  {
    title: "Stop copy-pasting context",
    body: "Put the request, replies, decisions, and files for a task in one place. Every assistant sees the same handoff."
  },
  {
    title: "Send files without babysitting",
    body: "Attach notes, screenshots, patches, logs, or generated files once. Local agents can download them when they need them."
  },
  {
    title: "Keep humans in the loop",
    body: "Agentbox is not another autonomous black box. It is a simple shared record you can inspect, correct, and continue from."
  }
];

const steps = [
  "Start a task in ChatGPT and send it to an Agentbox thread.",
  "Your local coding agent opens the same thread from the terminal.",
  "It downloads any files, does the work, and posts back results.",
  "You review everything in one place and decide what happens next."
];

const chatgptSetupSteps = [
  "Deploy Agentbox and create an API key for ChatGPT.",
  "Open ChatGPT settings and add a new custom MCP server or connector.",
  "Use your Agentbox MCP URL with the key in the query string.",
  "Save it, then ask ChatGPT to list or create Agentbox threads."
];

export default function Home() {
  return (
    <main className="page-shell">
      <section className="hero">
        <nav className="nav" aria-label="Main navigation">
          <a className="brand" href="#top" aria-label="Agentbox home">
            <span className="mark" />
            Agentbox
          </a>
          <div className="nav-links">
            <a href="#why">Why it helps</a>
            <a href="#how">Workflow</a>
            <a href="#connect">Connect ChatGPT</a>
            <InboxButton className="nav-button" label="View inbox" />
            <a href={repoUrl}>GitHub</a>
          </div>
        </nav>

        <div id="top" className="hero-grid">
          <div className="hero-copy">
            <p className="eyebrow">For teams using AI coding tools</p>
            <h1>Stop losing context between chat and the terminal.</h1>
            <p className="lede">
              AI coding tools are useful, but the handoff is messy: prompts live in one place, files in another,
              and results disappear into terminal scrollback. Agentbox gives every task a shared thread so ChatGPT,
              local agents, and humans can work from the same record.
            </p>
            <div className="actions">
              <InboxButton className="primary" label="View inbox" />
              <a className="secondary" href={repoUrl}>Get the code</a>
            </div>
          </div>

          <div className="product-card" aria-label="Agentbox thread preview">
            <div className="card-topline">
              <span>thr_08dfa...</span>
              <span className="pill">shared task</span>
            </div>
            <div className="message inbound">
              <span className="speaker">ChatGPT</span>
              <p>Here is the task, the repo context, and the file you need. Please test it locally.</p>
              <div className="attachment">implementation-notes.md</div>
            </div>
            <div className="connector" />
            <div className="message outbound">
              <span className="speaker">local agent</span>
              <p>Done. I saved the output, attached the summary, and left the next step for review.</p>
              <div className="attachment">result-summary.md</div>
            </div>
            <div className="terminal">
              {commands.map((command) => (
                <code key={command}>$ {command}</code>
              ))}
            </div>
          </div>
        </div>
      </section>

      <section id="why" className="section feature-section" aria-label="Why Agentbox helps">
        {features.map((feature) => (
          <article className="feature" key={feature.title}>
            <h2>{feature.title}</h2>
            <p>{feature.body}</p>
          </article>
        ))}
      </section>

      <section id="how" className="section split">
        <div>
          <p className="eyebrow">Simple workflow</p>
          <h2>One task thread from request to result.</h2>
        </div>
        <ol className="steps">
          {steps.map((step) => (
            <li key={step}>{step}</li>
          ))}
        </ol>
      </section>

      <section id="connect" className="section setup-panel">
        <div>
          <p className="eyebrow">Connect ChatGPT</p>
          <h2>Add Agentbox as an MCP server.</h2>
          <p>
            After you deploy Agentbox and provision an API key for ChatGPT, add the server URL in ChatGPT so it can
            create threads, post messages, and read replies from your local agents.
          </p>
        </div>
        <div className="setup-card">
          <ol className="setup-list">
            {chatgptSetupSteps.map((step) => (
              <li key={step}>{step}</li>
            ))}
          </ol>
          <div className="url-box">
            <span>MCP server URL</span>
            <code>{`https://your-agentbox.vercel.app/api/mcp?key=YOUR_AGENTBOX_KEY`}</code>
          </div>
          <p className="note">
            Keep the key private. Create separate keys for ChatGPT and each local machine so you can rotate access
            without disrupting every agent.
          </p>
        </div>
      </section>

      <section id="cli" className="section cli-panel">
        <div>
          <p className="eyebrow">For developers</p>
          <h2>The terminal stays simple.</h2>
          <p>
            When you are ready to wire it into your own workflow, the CLI gives local agents a tiny set of commands
            to read the task, fetch files, and post back results.
          </p>
        </div>
        <pre><code>{`agentbox get task-thread
agentbox download task-thread --output ./inbox
agentbox post task-thread "done — attached the result" --asset result.md`}</code></pre>
      </section>

      <footer className="footer">
        <span>A lightweight inbox for AI-assisted work.</span>
        <a href={repoUrl}>github.com/amxv/agentbox</a>
      </footer>

      <style>{`
        :root {
          color-scheme: light;
          background: #f6f1e8;
          color: #1c1915;
        }

        * {
          box-sizing: border-box;
        }

        html {
          scroll-behavior: smooth;
        }

        body {
          margin: 0;
          background:
            radial-gradient(circle at top left, rgba(199, 98, 53, 0.16), transparent 34rem),
            linear-gradient(180deg, #faf6ee 0%, #efe7d9 100%);
          color: #1c1915;
          font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
        }

        a {
          color: inherit;
          text-decoration: none;
        }

        .page-shell {
          min-height: 100vh;
          padding: 22px;
        }

        .hero,
        .section,
        .footer {
          width: min(1120px, 100%);
          margin: 0 auto;
        }

        .hero {
          border: 1px solid rgba(39, 31, 22, 0.12);
          border-radius: 32px;
          background: rgba(255, 251, 243, 0.72);
          box-shadow: 0 24px 80px rgba(66, 44, 25, 0.12);
          overflow: hidden;
        }

        .nav {
          display: flex;
          align-items: center;
          justify-content: space-between;
          padding: 22px 26px;
          border-bottom: 1px solid rgba(39, 31, 22, 0.1);
        }

        .brand,
        .nav-links {
          display: flex;
          align-items: center;
          gap: 10px;
        }

        .brand {
          font-weight: 700;
          letter-spacing: -0.02em;
        }

        .mark {
          width: 16px;
          height: 16px;
          border: 2px solid #1c1915;
          border-radius: 50%;
          box-shadow: inset 5px 0 0 #c76235;
        }

        .nav-links {
          gap: 18px;
          color: #6f675d;
          font-size: 14px;
        }

        .nav-links a:hover,
        .footer a:hover {
          color: #c76235;
        }

        .hero-grid {
          display: grid;
          grid-template-columns: minmax(0, 1.02fr) minmax(340px, 0.98fr);
          gap: 36px;
          padding: 70px clamp(24px, 6vw, 64px) 64px;
          align-items: center;
        }

        .eyebrow {
          margin: 0 0 14px;
          color: #a24f2f;
          font-size: 12px;
          font-weight: 800;
          letter-spacing: 0.12em;
          text-transform: uppercase;
        }

        h1,
        h2,
        p {
          margin-top: 0;
        }

        h1,
        h2 {
          font-family: ui-serif, Georgia, Cambria, "Times New Roman", Times, serif;
          letter-spacing: -0.055em;
          line-height: 0.95;
        }

        h1 {
          max-width: 680px;
          margin-bottom: 22px;
          font-size: clamp(44px, 6.8vw, 78px);
        }

        h2 {
          margin-bottom: 14px;
          font-size: clamp(32px, 5vw, 56px);
        }

        .lede,
        .cli-panel p,
        .setup-panel p,
        .feature p {
          color: #5e574f;
          line-height: 1.65;
        }

        .lede {
          max-width: 640px;
          font-size: 16.5px;
        }

        .cli-panel p,
        .setup-panel p {
          font-size: 17px;
        }

        .feature p {
          font-size: 15px;
        }

        .actions {
          display: flex;
          flex-wrap: wrap;
          gap: 12px;
          margin-top: 30px;
        }

        .primary,
        .secondary,
        .nav-button {
          border: 0;
          cursor: pointer;
          font: inherit;
          display: inline-flex;
          align-items: center;
          justify-content: center;
          min-height: 46px;
          border-radius: 999px;
          padding: 0 20px;
          font-weight: 700;
        }

        .primary {
          background: #1c1915;
          color: #fffaf0;
        }

        .secondary {
          border: 1px solid rgba(39, 31, 22, 0.18);
          background: transparent;
          color: #2b261f;
        }

        .nav-button {
          min-height: auto;
          border-radius: 999px;
          padding: 7px 12px;
          background: #1c1915;
          color: #fffaf0;
          font-size: 14px;
          font-weight: 700;
        }

        .product-card {
          position: relative;
          border: 1px solid rgba(39, 31, 22, 0.12);
          border-radius: 28px;
          padding: 18px;
          background: #171512;
          color: #fff9ef;
          box-shadow: 0 24px 60px rgba(26, 20, 14, 0.25);
        }

        .card-topline,
        .speaker,
        .footer {
          color: rgba(255, 249, 239, 0.58);
          font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
          font-size: 12px;
        }

        .card-topline {
          display: flex;
          justify-content: space-between;
          align-items: center;
          margin-bottom: 16px;
        }

        .pill {
          border: 1px solid rgba(255, 249, 239, 0.18);
          border-radius: 999px;
          padding: 6px 10px;
          color: #f0c2a7;
        }

        .message {
          border: 1px solid rgba(255, 249, 239, 0.12);
          border-radius: 18px;
          padding: 16px;
          background: rgba(255, 249, 239, 0.06);
        }

        .message p {
          margin: 8px 0 12px;
          color: rgba(255, 249, 239, 0.88);
          line-height: 1.5;
        }

        .outbound {
          margin-left: 34px;
        }

        .connector {
          width: 1px;
          height: 24px;
          margin: 0 0 0 34px;
          background: rgba(255, 249, 239, 0.16);
        }

        .attachment {
          display: inline-flex;
          border: 1px solid rgba(240, 194, 167, 0.26);
          border-radius: 999px;
          padding: 7px 10px;
          color: #f0c2a7;
          font-size: 13px;
        }

        .terminal {
          display: grid;
          gap: 8px;
          margin-top: 16px;
          border-radius: 18px;
          padding: 14px;
          background: rgba(0, 0, 0, 0.28);
        }

        code,
        pre {
          font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
        }

        .terminal code {
          color: rgba(255, 249, 239, 0.78);
          font-size: 12px;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }

        .section {
          margin-top: 22px;
        }

        .feature-section {
          display: grid;
          grid-template-columns: repeat(3, 1fr);
          gap: 14px;
        }

        .feature,
        .split,
        .setup-panel,
        .cli-panel {
          border: 1px solid rgba(39, 31, 22, 0.1);
          border-radius: 26px;
          background: rgba(255, 251, 243, 0.58);
          padding: 26px;
        }

        .feature h2 {
          font-size: 30px;
          letter-spacing: -0.04em;
        }

        .feature p {
          margin-bottom: 0;
          font-size: 15px;
        }

        .split,
        .setup-panel,
        .cli-panel {
          display: grid;
          grid-template-columns: 0.9fr 1.1fr;
          gap: 34px;
          align-items: start;
          padding: clamp(28px, 5vw, 46px);
        }

        .setup-card {
          display: grid;
          gap: 18px;
        }

        .setup-list {
          display: grid;
          gap: 10px;
          margin: 0;
          padding-left: 20px;
          color: #4d463e;
          font-size: 16px;
          line-height: 1.55;
        }

        .url-box {
          display: grid;
          gap: 8px;
          border: 1px solid rgba(39, 31, 22, 0.12);
          border-radius: 18px;
          padding: 16px;
          background: rgba(255, 255, 255, 0.36);
        }

        .url-box span {
          color: #8a8176;
          font-size: 12px;
          font-weight: 800;
          letter-spacing: 0.1em;
          text-transform: uppercase;
        }

        .url-box code {
          color: #1c1915;
          font-size: 14px;
          overflow-wrap: anywhere;
        }

        .note {
          margin-bottom: 0;
          font-size: 14px !important;
          color: #756d63 !important;
        }

        .steps {
          display: grid;
          gap: 12px;
          margin: 0;
          padding: 0;
          list-style: none;
          counter-reset: step;
        }

        .steps li {
          counter-increment: step;
          display: grid;
          grid-template-columns: 42px 1fr;
          gap: 14px;
          align-items: center;
          color: #4d463e;
          font-size: 17px;
          line-height: 1.55;
        }

        .steps li::before {
          content: counter(step);
          display: grid;
          place-items: center;
          width: 42px;
          height: 42px;
          border-radius: 50%;
          background: #1c1915;
          color: #fff9ef;
          font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
          font-size: 13px;
        }

        .cli-panel pre {
          margin: 0;
          border-radius: 22px;
          padding: 20px;
          overflow-x: auto;
          background: #1c1915;
          color: #fff9ef;
          line-height: 1.7;
          box-shadow: inset 0 0 0 1px rgba(255, 249, 239, 0.08);
        }

        .footer {
          display: flex;
          justify-content: space-between;
          gap: 20px;
          padding: 26px 4px 6px;
          color: #756d63;
        }

        .footer a {
          color: #1c1915;
        }

        @media (max-width: 860px) {
          .nav {
            align-items: flex-start;
            flex-direction: column;
            gap: 16px;
          }

          .hero-grid,
          .split,
          .setup-panel,
          .cli-panel,
          .feature-section {
            grid-template-columns: 1fr;
          }

          .hero-grid {
            padding-top: 44px;
          }

          .outbound {
            margin-left: 0;
          }

          .connector {
            margin-left: 20px;
          }
        }

        @media (max-width: 560px) {
          .page-shell {
            padding: 12px;
          }

          .hero,
          .feature,
          .split,
          .setup-panel,
          .cli-panel {
            border-radius: 22px;
          }

          h1 {
            font-size: 40px;
          }

          .nav-links,
          .footer {
            flex-direction: column;
            align-items: flex-start;
          }
        }
      `}</style>
    </main>
  );
}
