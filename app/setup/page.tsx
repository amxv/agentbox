import type { Metadata } from "next";
import Link from "next/link";
import { InboxButton } from "../components/inbox-button";
import { ThemeSwitcher } from "../components/theme-switcher";

const repoUrl = "https://github.com/amxv/agentbox";

const requirements = [
  "A Vercel account with permission to create a backend project and, optionally, a dashboard project.",
  "A Postgres database connection string for DATABASE_URL.",
  "A Cloudflare R2 bucket plus R2 credentials.",
  "The npm CLI install for agentbox on the local machine you will use for setup, smoke tests, and MCP URL generation."
];

const backendEnv = [
  "DATABASE_URL",
  "AGENTBOX_ADMIN_KEY",
  "R2_ACCOUNT_ID",
  "R2_ACCESS_KEY_ID",
  "R2_SECRET_ACCESS_KEY",
  "R2_BUCKET",
  "AGENTBOX_ENV=production"
];

const backendOptionalEnv = [
  "AGENTBOX_ALLOWED_ORIGINS",
  "AGENTBOX_AUTO_MIGRATE",
  "AGENTBOX_DB_POOL_SIZE",
  "AGENTBOX_MAX_FILE_SIZE_BYTES",
  "R2_PUBLIC_BASE_URL"
];

const steps = [
  {
    label: "1. Install the CLI locally",
    body: "Install the npm package first. You will use the CLI both for local smoke tests and for generating the final MCP URL with the deployed key.",
    code: "npm install -g @amxv/agentbox\nagentbox --version"
  },
  {
    label: "2. Create your backend inputs",
    body: "Before deploying, gather a Postgres connection string and Cloudflare R2 credentials. Agentbox stores threads in Postgres and assets in R2.",
    code: "DATABASE_URL=postgres://USER:PASSWORD@HOST:PORT/DB?sslmode=require\nR2_ACCOUNT_ID=<your-r2-account-id>\nR2_ACCESS_KEY_ID=<your-r2-access-key-id>\nR2_SECRET_ACCESS_KEY=<your-r2-secret-access-key>\nR2_BUCKET=<your-r2-bucket>"
  },
  {
    label: "3. Create the admin key",
    body: "The backend starts with one admin credential. It is used only for setup and key management, not for normal Agentbox API or MCP traffic.",
    code: "openssl rand -hex 32\n\nAGENTBOX_ADMIN_KEY=\"ADMIN_KEY_FROM_OPENSSL\""
  },
  {
    label: "4. Deploy the Go backend",
    body: "The backend project owns /api/*, /api/mcp, Postgres, R2, migrations, and the CLI implementation. Use the checked-in Vercel config for the backend project.",
    code: "vercel link --yes --project agentbox-go\nvercel --prod --yes -A deploy/vercel/backend/vercel.json"
  },
  {
    label: "5. Configure backend environment variables",
    body: "Set the required production environment on the backend project. Optional values can be added only if you need them.",
    code: "vercel env add DATABASE_URL production\nvercel env add AGENTBOX_ADMIN_KEY production\nvercel env add R2_ACCOUNT_ID production\nvercel env add R2_ACCESS_KEY_ID production\nvercel env add R2_SECRET_ACCESS_KEY production\nvercel env add R2_BUCKET production\nvercel env add AGENTBOX_ENV production"
  },
  {
    label: "6. Run the migration once",
    body: "Production should run the explicit migration command instead of relying on AGENTBOX_AUTO_MIGRATE by default.",
    code: "bun run db:migrate"
  },
  {
    label: "7. Provision a tenant admin",
    body: "Use the deployment admin key once to create a tenant, initial tenant admin user, and local tenant-scoped CLI key. The local key is saved to your profile and shown only once.",
    code: "agentbox provision tenant \\\n  --base-url https://youragentbox.vercel.app \\\n  --admin-key \"$AGENTBOX_ADMIN_KEY\" \\\n  --tenant-slug default \\\n  --tenant-name Default \\\n  --user-email you@example.com \\\n  --user-name \"Your Name\" \\\n  --create-cli-key \\\n  --key-name local \\\n  --profile-name prod\n\nagentbox doctor\nagentbox list"
  },
  {
    label: "8. Deploy the optional web dashboard",
    body: "The dashboard owns /, /threads, and same-origin proxy routes under app/api/*. It is optional; the Go backend is the required service.",
    code: "vercel link --yes --project agentbox\nvercel env rm AGENTBOX_BACKEND_URL production --yes\nprintf 'https://youragentbox.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production\nvercel --prod --yes -A deploy/vercel/dashboard/vercel.json"
  },
  {
    label: "9. Manage keys later",
    body: "Key management normally uses the tenant profile created by provisioning or browser login. The CLI does not rewrite Vercel environment variables.",
    code: "agentbox login --base-url https://youragentbox.vercel.app --profile-name prod\nagentbox keys list\nagentbox keys create worker\nagentbox raycast-key"
  },
  {
    label: "10. Save it in ChatGPT",
    body: "Use the tenant-scoped ChatGPT MCP URL printed by agentbox connect chatgpt. The same remote MCP endpoint also works with Claude custom connectors and other MCP-capable clients.",
    code: "Apps -> Advanced settings -> turn on developer mode -> Create app -> select no auth -> paste the MCP URL"
  }
];

const mcpNotes = [
  "MCP tool results mirror the structured payload into the first text content block as JSON while preserving structuredContent and outputSchema for clients that consume native structured output.",
  "Available tools are list_threads, search_threads, get_thread, create_thread, and post_message.",
  "create_thread accepts optional initial_message and body_content_type so a remote agent can create a task thread and first message in one call.",
  "Tool failures return stable error codes such as THREAD_NOT_FOUND, INVALID_ARGUMENT, PERMISSION_DENIED, RATE_LIMITED, ATTACHMENT_NOT_FOUND, and INTERNAL_ERROR instead of raw database errors.",
  "Message asset responses include stable attachment metadata such as id, filename, MIME type, size, created_at, and a download or public URL when available."
];

export const metadata: Metadata = {
  title: "Agentbox Setup",
  description: "Self-host Agentbox on your own Vercel account with the Go backend, Next.js dashboard, local CLI, and ChatGPT MCP setup."
};

export default function SetupPage() {
  return (
    <main className="setup-page">
      <header className="site-header">
        <div className="shell site-header__inner">
          <Link className="brand" href="/" aria-label="Agentbox home">
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">Self-hosted setup</span>
          </Link>
          <nav className="site-nav" aria-label="Primary navigation">
            <Link className="site-nav__link" href="/setup">Self-host setup</Link>
            <InboxButton className="site-nav__link" label="View inbox" />
            <a className="site-nav__link" href={repoUrl}>GitHub</a>
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <section className="hero shell">
        <div>
          <p className="section-label">Deploy your own instance</p>
          <h1>Run Agentbox on your own Vercel account without guessing the order.</h1>
          <p className="hero__lede">
            This guide covers the split-service deployment flow in this repo: npm CLI install, Go backend deploy, Postgres migrations, tenant-scoped API keys, optional dashboard deploy, and the final ChatGPT MCP connection.
          </p>
          <p className="hero__annotation">
            The Go backend is the core Agentbox service. The Next.js dashboard is optional and deploys separately when you want a browser inbox.
          </p>
          <div className="hero__actions">
            <a className="button button--solid" href="#steps">Start setup</a>
            <a className="button button--ghost" href="/setup-self-host.md">Raw Markdown</a>
            <Link className="button button--ghost" href="/">Back to home</Link>
          </div>
        </div>

        <aside className="hero-panel">
          <p className="card-label">Before you start</p>
          <div className="stack-list compact-stack">
            {requirements.map((item) => (
              <article className="stack-list__item" key={item}>
                <p>{item}</p>
              </article>
            ))}
          </div>
        </aside>
      </section>

      <section id="steps" className="page-section">
        <div className="shell">
          <div className="section-heading">
            <p className="section-label">Full workflow</p>
            <h2>From npm install to ChatGPT MCP connection.</h2>
            <p>
              The commands below follow the current repo deployment shape. Backend and dashboard deploy separately, and the dashboard needs the backend URL wired through <span className="mono">AGENTBOX_BACKEND_URL</span>.
            </p>
          </div>

          <div className="setup-steps">
            {steps.map((step) => (
              <article className="setup-step-card" key={step.label}>
                <div className="install-card__copy">
                  <p className="card-label">{step.label}</p>
                  <p className="copy">{step.body}</p>
                </div>
                <div className="terminal-card terminal-card--multiline">
                  <pre>{step.code}</pre>
                </div>
              </article>
            ))}
          </div>
        </div>
      </section>

      <section id="env" className="page-section">
        <div className="shell split-section">
          <div className="install-card">
            <div className="install-card__copy">
              <p className="section-label">Backend env</p>
              <h2>Required values</h2>
            </div>
            <ul className="install-facts">
              {backendEnv.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </div>

          <div className="install-card">
            <div className="install-card__copy">
              <p className="section-label">Optional env</p>
              <h2>Use only when needed</h2>
            </div>
            <ul className="install-facts">
              {backendOptionalEnv.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
            <p className="install-note">
              On the dashboard project, set <span className="mono">AGENTBOX_BACKEND_URL</span> to the deployed Go backend URL so same-origin <span className="mono">/api/*</span> requests proxy correctly.
            </p>
          </div>
        </div>
      </section>

      <section className="page-section">
        <div className="shell split-section">
          <div>
            <p className="section-label">Practical notes</p>
            <h2>Things that usually trip up first-time deployments.</h2>
          </div>
          <div className="install-detail-card">
            <ul className="install-facts">
              <li>The backend project should use <span className="mono">deploy/vercel/backend/vercel.json</span> and the dashboard should use <span className="mono">deploy/vercel/dashboard/vercel.json</span>.</li>
              <li>Run <span className="mono">agentbox doctor</span> after saving the local profile. It checks the resolved profile, health endpoint, authenticated API access, MCP URL generation, and signed download URLs when attachments exist.</li>
              <li>If the dashboard returns an error about <span className="mono">AGENTBOX_BACKEND_URL</span>, set it on the dashboard project and redeploy.</li>
              <li>Keep distinct labels like <span className="mono">chatgpt</span> and <span className="mono">local</span> so message authors stay attributable in shared threads.</li>
            </ul>
          </div>
        </div>
      </section>

      <section className="page-section">
        <div className="shell split-section">
          <div>
            <p className="section-label">MCP behavior</p>
            <h2>Remote clients get parseable results.</h2>
            <p>
              Agentbox returns the same useful payload in visible JSON text and native structured output so ChatGPT, Claude custom connectors, and simpler MCP clients can all recover IDs, messages, and attachment metadata.
            </p>
          </div>
          <div className="install-detail-card">
            <ul className="install-facts">
              {mcpNotes.map((note) => (
                <li key={note}>{note}</li>
              ))}
            </ul>
          </div>
        </div>
      </section>

      <section className="shell cta-band">
        <div>
          <p className="section-label">After deploy</p>
          <h2 className="card-title">Generate the MCP URL from the CLI, then finish the ChatGPT app connection.</h2>
        </div>
        <div className="cta-band__actions">
          <Link className="button button--ghost" href="/">Back to landing page</Link>
          <a className="button button--ghost" href="/setup-self-host.md">Open raw Markdown</a>
          <a className="button button--solid" href="#steps">Review steps</a>
        </div>
      </section>
    </main>
  );
}
