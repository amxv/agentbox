import type { Metadata } from "next";
import Link from "next/link";
import { AgentboxMark } from "../components/agentbox-mark";
import { InboxButton } from "../components/inbox-button";
import { PublicCopyButton } from "../components/public-copy-button";
import { ThemeSwitcher } from "../components/theme-switcher";
import styles from "./setup.module.css";

const repoUrl = "https://github.com/amxv/agentbox";

const requirements = [
  { label: "DEPLOY", value: "Vercel account", tone: "pink" },
  { label: "STATE", value: "Postgres database", tone: "cyan" },
  { label: "FILES", value: "Cloudflare R2 bucket", tone: "orange" },
  { label: "CONTROL", value: "Agentbox CLI", tone: "lime" }
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

const phases = [
  {
    number: "01",
    eyebrow: "FOUNDATION",
    title: "Prepare the service that every surface shares.",
    tone: "pink",
    steps: [
      {
        label: "Install the native CLI",
        body: "The npm package delivers the correct small Go binary for the current platform. Use it for provisioning, profiles, health checks, key management, and surface setup.",
        code: "npm install -g @amxv/agentbox\nagentbox --version"
      },
      {
        label: "Prepare Postgres and R2",
        body: "Threads, messages, identities, and attachment metadata live in Postgres. File bytes live in Cloudflare R2 and transfer directly through signed URLs.",
        code: "DATABASE_URL=postgres://USER:PASSWORD@HOST:PORT/DB?sslmode=require\nR2_ACCOUNT_ID=<your-r2-account-id>\nR2_ACCESS_KEY_ID=<your-r2-access-key-id>\nR2_SECRET_ACCESS_KEY=<your-r2-secret-access-key>\nR2_BUCKET=<your-r2-bucket>"
      },
      {
        label: "Create the deployment admin key",
        body: "This credential exists for provisioning and deployment-level administration. Daily users, agents, Raycast, and scripts should receive separate tenant-scoped identities.",
        code: "openssl rand -hex 32\nexport AGENTBOX_ADMIN_KEY=\"<generated-admin-key>\""
      }
    ]
  },
  {
    number: "02",
    eyebrow: "CORE SERVICE",
    title: "Deploy the one backend behind every face.",
    tone: "cyan",
    steps: [
      {
        label: "Configure the backend project",
        body: "Link the Vercel backend project and add the required production environment values. The Go backend owns REST, MCP, auth, Postgres, R2, migrations, and business rules.",
        code: "vercel link --yes --project agentbox-go\nvercel env add DATABASE_URL production\nvercel env add AGENTBOX_ADMIN_KEY production\nvercel env add R2_ACCOUNT_ID production\nvercel env add R2_ACCESS_KEY_ID production\nvercel env add R2_SECRET_ACCESS_KEY production\nvercel env add R2_BUCKET production\nvercel env add AGENTBOX_ENV production"
      },
      {
        label: "Deploy and migrate",
        body: "Deploy with the checked-in backend config, then run the explicit migration command with production environment values available.",
        code: "vercel --prod --yes -A deploy/vercel/backend/vercel.json\nbun run db:migrate"
      },
      {
        label: "Provision the first tenant and human",
        body: "Create the tenant, initial tenant admin, and a tenant-scoped CLI identity. The resulting profile becomes your first authenticated seat at the shared inbox.",
        code: "agentbox provision tenant \\\n  --base-url https://youragentbox.vercel.app \\\n  --admin-key \"$AGENTBOX_ADMIN_KEY\" \\\n  --tenant-slug default \\\n  --tenant-name Default \\\n  --user-email you@example.com \\\n  --user-name \"Your Name\" \\\n  --create-cli-key \\\n  --key-name local \\\n  --profile-name prod\n\nagentbox doctor\nagentbox list"
      }
    ]
  },
  {
    number: "03",
    eyebrow: "PARTICIPANTS",
    title: "Give every person and tool its own way in.",
    tone: "orange",
    steps: [
      {
        label: "Deploy the human dashboard",
        body: "The Next.js dashboard is the human participant surface. It can create threads, reply, upload files, inspect history, and manage tenant-scoped keys through the same backend.",
        code: "vercel link --yes --project agentbox\nvercel env rm AGENTBOX_BACKEND_URL production --yes\nprintf 'https://youragentbox.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production\nvercel --prod --yes -A deploy/vercel/dashboard/vercel.json"
      },
      {
        label: "Add more named identities",
        body: "Use browser-assisted login on additional machines, then create distinct keys for agents, scripts, CI, and Raycast. Names become attribution in the shared thread history.",
        code: "agentbox login --base-url https://youragentbox.vercel.app --profile-name prod\nagentbox keys list\nagentbox keys create codex-local\nagentbox keys create ci-release\nagentbox raycast-key"
      },
      {
        label: "Connect MCP participants",
        body: "Generate a tenant-scoped MCP URL for ChatGPT. The same endpoint works with Claude custom connectors and other MCP-capable hosts. Every MCP tool reads and writes the same inbox as all other surfaces.",
        code: "agentbox connect chatgpt\n\n# In ChatGPT:\n# Apps → Advanced settings → developer mode\n# Create app → no auth → paste the printed MCP URL"
      },
      {
        label: "Connect the Raycast participant",
        body: "The macOS extension talks directly to the existing HTTP API. Configure the Agentbox URL and the actor key printed by agentbox raycast-key, then browse and post to the same shared threads.",
        code: "cd raycast/agentbox\nnpm install\nnpm run dev\n\n# Raycast preferences:\n# Agentbox URL: https://youragentbox.vercel.app\n# Agentbox API Key: <output from agentbox raycast-key>"
      }
    ]
  }
];

const checks = [
  "agentbox doctor checks profile resolution, service health, authenticated access, MCP URL construction, and signed attachment downloads.",
  "curl https://youragentbox.vercel.app/api/health should return an ok Agentbox service response.",
  "Create a thread from the dashboard, reply from Raycast, read it through MCP, and post from the CLI to prove every surface sees the same state.",
  "Use distinct identities such as human-ashray, chatgpt, codex-local, raycast, and ci-release so attribution stays readable."
];

export const metadata: Metadata = {
  title: "Self-host Agentbox — Complete setup guide",
  description:
    "Deploy Agentbox, provision a tenant, and connect the human dashboard, MCP hosts, CLI agents, Raycast, scripts, and CI to one shared inbox."
};

function CodeBlock({ code }: { code: string }) {
  return (
    <div className={styles.codeBlock}>
      <div className={styles.codeBar}>
        <span>copyable command</span>
        <PublicCopyButton className={styles.copyButton} label="COPY" copiedLabel="COPIED" value={code} />
      </div>
      <pre>{code}</pre>
    </div>
  );
}

export default function SetupPage() {
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <Link className={styles.brand} href="/" aria-label="Agentbox home">
            <AgentboxMark className={styles.mark} />
            <span>AGENTBOX</span>
            <small>SETUP</small>
          </Link>
          <nav className={styles.nav} aria-label="Setup navigation">
            <Link href="/">Home</Link>
            <Link href="/raycast">Raycast</Link>
            <a href="/setup-self-host.md">Raw Markdown</a>
            <a href={repoUrl}>GitHub</a>
            <InboxButton className={styles.inboxLink} label="Open inbox" />
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <main>
        <section className={styles.hero}>
          <div className={styles.heroCopy}>
            <div className={styles.kicker}><span /> Self-host the shared state</div>
            <h1>One backend.<br /><em>Every participant.</em></h1>
            <p>
              Deploy the Agentbox core once, then let humans, MCP hosts, CLI agents, Raycast, scripts, and CI enter through the interface that fits them. They all read and write the same tenant-scoped inbox.
            </p>
            <div className={styles.heroActions}>
              <a className={styles.primaryButton} href="#phases">Start setup</a>
              <PublicCopyButton className={styles.secondaryButton} label="Copy full Markdown" copiedLabel="Markdown copied" sourceUrl="/setup-self-host.md" />
              <a className={styles.textButton} href="/setup-self-host.md">Open raw guide ↗</a>
            </div>
          </div>

          <div className={styles.requirementBoard}>
            <div className={styles.requirementTitle}>
              <span>BEFORE YOU START</span>
              <b>4 inputs</b>
            </div>
            <div className={styles.requirementGrid}>
              {requirements.map((requirement) => (
                <article className={styles[requirement.tone]} key={requirement.value}>
                  <small>{requirement.label}</small>
                  <strong>{requirement.value}</strong>
                </article>
              ))}
            </div>
            <div className={styles.requirementFooter}>
              <span>POSTGRES</span><i>+</i><span>R2</span><i>+</i><span>GO BACKEND</span><i>+</i><span>YOUR SURFACES</span>
            </div>
          </div>
        </section>

        <div className={styles.marquee} aria-hidden="true">
          <span>HUMAN DASHBOARD</span><i>·</i><span>MCP HOSTS</span><i>·</i><span>CLI AGENTS</span><i>·</i><b>ONE TENANT-SCOPED INBOX</b><i>·</i><span>RAYCAST</span><i>·</i><span>SCRIPTS</span><i>·</i><span>CI</span>
        </div>

        <section className={styles.overview}>
          <div className={styles.sectionNumber}>THE DEPLOYMENT MODEL</div>
          <div>
            <h2>The core is required.<br />Every interface is a projection.</h2>
            <p>
              The Go service owns product rules and data. The Next.js dashboard is the human seat. MCP, CLI, Raycast, REST scripts, and CI are equal client surfaces. Add only the surfaces your participants need.
            </p>
          </div>
        </section>

        <section className={styles.phases} id="phases">
          {phases.map((phase) => (
            <article className={`${styles.phase} ${styles[phase.tone]}`} key={phase.number}>
              <div className={styles.phaseIntro}>
                <span>{phase.number}</span>
                <small>{phase.eyebrow}</small>
                <h2>{phase.title}</h2>
              </div>
              <div className={styles.stepList}>
                {phase.steps.map((step, index) => (
                  <section className={styles.step} key={step.label}>
                    <div className={styles.stepCopy}>
                      <span>{phase.number}.{String(index + 1).padStart(2, "0")}</span>
                      <h3>{step.label}</h3>
                      <p>{step.body}</p>
                    </div>
                    <CodeBlock code={step.code} />
                  </section>
                ))}
              </div>
            </article>
          ))}
        </section>

        <section className={styles.envSection}>
          <div className={styles.envIntro}>
            <div className={styles.sectionNumber}>ENVIRONMENT REFERENCE</div>
            <h2>Keep the required set small. Add optional controls deliberately.</h2>
            <p>The dashboard needs only <code>AGENTBOX_BACKEND_URL</code> to proxy same-origin API requests to the deployed Go service.</p>
          </div>
          <div className={styles.envCards}>
            <article>
              <div><span>REQUIRED</span><b>{backendEnv.length}</b></div>
              <ul>{backendEnv.map((item) => <li key={item}>{item}</li>)}</ul>
              <PublicCopyButton className={styles.copyListButton} label="Copy required env" copiedLabel="Required env copied" value={backendEnv.join("\n")} />
            </article>
            <article>
              <div><span>OPTIONAL</span><b>{backendOptionalEnv.length}</b></div>
              <ul>{backendOptionalEnv.map((item) => <li key={item}>{item}</li>)}</ul>
              <PublicCopyButton className={styles.copyListButton} label="Copy optional env" copiedLabel="Optional env copied" value={backendOptionalEnv.join("\n")} />
            </article>
          </div>
        </section>

        <section className={styles.checkSection}>
          <div className={styles.checkStamp}>PROVE<br />THE LOOP</div>
          <div>
            <div className={styles.sectionNumber}>FINAL VERIFICATION</div>
            <h2>Test the shared inbox from more than one surface.</h2>
            <div className={styles.checkList}>
              {checks.map((check, index) => (
                <article key={check}>
                  <span>{String(index + 1).padStart(2, "0")}</span>
                  <p>{check}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className={styles.cta}>
          <div>
            <span>THE GUIDE IS ALSO PLAIN MARKDOWN</span>
            <h2>Copy it into any agent and deploy together.</h2>
          </div>
          <div className={styles.heroActions}>
            <PublicCopyButton className={styles.primaryButton} label="Copy full guide" copiedLabel="Guide copied" sourceUrl="/setup-self-host.md" />
            <Link className={styles.textButton} href="/raycast">Set up Raycast ↗</Link>
            <Link className={styles.textButton} href="/">Back home ↗</Link>
          </div>
        </section>
      </main>

      <footer className={styles.footer}>
        <div className={styles.brand}><AgentboxMark className={styles.mark} /><span>AGENTBOX</span></div>
        <p>One shared inbox. Every participant.</p>
        <div><a href="/setup-self-host.md">Raw Markdown</a><Link href="/raycast">Raycast</Link><a href={repoUrl}>GitHub</a></div>
      </footer>
    </div>
  );
}
