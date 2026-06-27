const repoUrl = "https://github.com/amxv/agentbox";

const commands = [
  "agentbox list",
  "agentbox get thr_xxx",
  "agentbox post thr_xxx \"done — see attached notes\" --asset notes.md",
  "agentbox download thr_xxx --output ./inbox"
];

const features = [
  {
    title: "One thread, many agents",
    body: "ChatGPT, Claude Code, Codex, and local shells can all work from the same shared thread instead of copy-pasting context between tools."
  },
  {
    title: "Attachments that survive the chat",
    body: "Files are persisted to R2 and linked back to messages, so local agents can pull exactly what ChatGPT attached."
  },
  {
    title: "Tiny surface area",
    body: "Four MCP tools, a simple REST API, and a CLI. Agentbox moves messages and files; it does not try to become the agent."
  }
];

const steps = [
  "ChatGPT posts a message or file to an Agentbox thread.",
  "A local agent reads the thread from the CLI.",
  "The local agent replies or attaches generated work.",
  "ChatGPT reads the same thread and continues the loop."
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
            <a href="#how">How it works</a>
            <a href="#cli">CLI</a>
            <a href={repoUrl}>GitHub</a>
          </div>
        </nav>

        <div id="top" className="hero-grid">
          <div className="hero-copy">
            <p className="eyebrow">MCP relay for coding agents</p>
            <h1>A shared inbox for ChatGPT and local agents.</h1>
            <p className="lede">
              Agentbox is a small threaded message relay that lets ChatGPT talk to local tools like Claude Code,
              Codex, and shell-based agents. Messages stay organized, attachments are durable, and the CLI keeps
              local workflows simple.
            </p>
            <div className="actions">
              <a className="primary" href={repoUrl}>View on GitHub</a>
              <a className="secondary" href="#cli">See the CLI</a>
            </div>
          </div>

          <div className="product-card" aria-label="Agentbox thread preview">
            <div className="card-topline">
              <span>thr_08dfa...</span>
              <span className="pill">live thread</span>
            </div>
            <div className="message inbound">
              <span className="speaker">ChatGPT</span>
              <p>Here is the implementation note. Please test it locally and attach the patch.</p>
              <div className="attachment">agentbox-refreshed-mcp-attachment-test.md</div>
            </div>
            <div className="connector" />
            <div className="message outbound">
              <span className="speaker">claude-local</span>
              <p>Tests passed. I attached the generated output and a short summary.</p>
              <div className="attachment">patch-summary.md</div>
            </div>
            <div className="terminal">
              {commands.map((command) => (
                <code key={command}>$ {command}</code>
              ))}
            </div>
          </div>
        </div>
      </section>

      <section className="section feature-section" aria-label="Features">
        {features.map((feature) => (
          <article className="feature" key={feature.title}>
            <h2>{feature.title}</h2>
            <p>{feature.body}</p>
          </article>
        ))}
      </section>

      <section id="how" className="section split">
        <div>
          <p className="eyebrow">How it works</p>
          <h2>Keep the agent loop in one place.</h2>
        </div>
        <ol className="steps">
          {steps.map((step) => (
            <li key={step}>{step}</li>
          ))}
        </ol>
      </section>

      <section id="cli" className="section cli-panel">
        <div>
          <p className="eyebrow">Local-first CLI</p>
          <h2>Pull the thread. Download the attachments. Keep moving.</h2>
          <p>
            The CLI only needs your Agentbox base URL and API key. For downloads, Agentbox returns short-lived R2
            signed URLs so file bytes go directly from R2 to the local machine.
          </p>
        </div>
        <pre><code>{`export AGENTBOX_BASE_URL="https://your-agentbox.vercel.app"
export AGENTBOX_API_KEY="LOCAL_KEY"

agentbox get thr_xxx
agentbox download thr_xxx --output ./agentbox-inbox
agentbox post thr_xxx "review complete" --asset result.md`}</code></pre>
      </section>

      <footer className="footer">
        <span>Built for small, explicit agent loops.</span>
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
          font-size: clamp(56px, 9vw, 104px);
        }

        h2 {
          margin-bottom: 14px;
          font-size: clamp(32px, 5vw, 56px);
        }

        .lede,
        .cli-panel p,
        .feature p {
          color: #5e574f;
          font-size: 18px;
          line-height: 1.65;
        }

        .actions {
          display: flex;
          flex-wrap: wrap;
          gap: 12px;
          margin-top: 30px;
        }

        .primary,
        .secondary {
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
          color: #2b261f;
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
        .cli-panel {
          display: grid;
          grid-template-columns: 0.9fr 1.1fr;
          gap: 34px;
          align-items: start;
          padding: clamp(28px, 5vw, 46px);
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
          .cli-panel {
            border-radius: 22px;
          }

          h1 {
            font-size: 48px;
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
