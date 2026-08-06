import type { Metadata } from "next";
import { ArrowUpRightIcon } from "lucide-react";
import Link from "next/link";
import { AgentboxMark } from "../components/agentbox-mark";
import { InboxButton } from "../components/inbox-button";
import { PublicCopyButton } from "../components/public-copy-button";
import { ThemeSwitcher } from "../components/theme-switcher";
import styles from "./setup.module.css";

const repoUrl = "https://github.com/amxv/agentbox";

const requirements = [
  ["01", "Vercel account", "Deploy the Go service and optional dashboard."],
  ["02", "Postgres", "Store users, teams, threads, messages, and metadata."],
  ["03", "Cloudflare R2", "Store attachment bytes outside the application server."],
  ["04", "Agentbox CLI", "Provision, diagnose, and connect every participant surface."]
];

const chapters = [
  {
    number: "01",
    label: "Foundation",
    title: "Prepare the shared service.",
    summary: "Install the native CLI and gather the infrastructure inputs used by every participant.",
    steps: [
      {
        title: "Install the native CLI",
        body: "The npm package delivers the correct small Go binary for the current platform. Use it for provisioning, profiles, health checks, key management, and surface setup.",
        code: "npm install -g @amxv/agentbox\nagentbox --version"
      },
      {
        title: "Prepare Postgres and R2",
        body: "Threads, messages, identities, and attachment metadata live in Postgres. File bytes live in Cloudflare R2 and transfer directly through signed URLs.",
        code: "DATABASE_URL=postgres://USER:PASSWORD@HOST:PORT/DB?sslmode=require\nR2_ACCOUNT_ID=<your-r2-account-id>\nR2_ACCESS_KEY_ID=<your-r2-access-key-id>\nR2_SECRET_ACCESS_KEY=<your-r2-secret-access-key>\nR2_BUCKET=<your-r2-bucket>"
      },
      {
        title: "Create the deployment admin key",
        body: "This credential exists only to issue one-time permanent-owner setup or recovery links. Daily users and integrations use separate user-owned credentials.",
        code: "openssl rand -hex 32\nexport AGENTBOX_ADMIN_KEY=\"<generated-admin-key>\""
      }
    ]
  },
  {
    number: "02",
    label: "Core service",
    title: "Deploy the backend behind every face.",
    summary: "The Go service owns REST, MCP, auth, Postgres, R2, migrations, and shared product rules.",
    steps: [
      {
        title: "Configure the backend project",
        body: "Link the Vercel backend project and add the required production environment values.",
        code: "vercel link --yes --project agentbox-go\nvercel env add DATABASE_URL production\nvercel env add AGENTBOX_ADMIN_KEY production\nvercel env add AGENTBOX_APP_PUBLIC_URL production\nvercel env add R2_ACCOUNT_ID production\nvercel env add R2_ACCESS_KEY_ID production\nvercel env add R2_SECRET_ACCESS_KEY production\nvercel env add R2_BUCKET production\nvercel env add AGENTBOX_ENV production"
      },
      {
        title: "Deploy and migrate",
        body: "Deploy with the checked-in backend config, then run the explicit migration command with production environment values available.",
        code: "vercel --prod --yes -A deploy/vercel/backend/vercel.json\nbun run db:migrate"
      },
      {
        title: "Create the permanent owner",
        body: "Issue a short-lived browser link from a trusted shell. Open it once to create the permanent owner, then invite every additional user from the owner dashboard.",
        code: "agentbox owner setup-token \\\
  --base-url https://youragentbox-api.vercel.app \\\
  --app-url https://youragentbox.vercel.app \\\
  --admin-key \"$AGENTBOX_ADMIN_KEY\" \\\
  --expires 30m"
      }
    ]
  },
  {
    number: "03",
    label: "Participants",
    title: "Give every person and tool its own desk.",
    summary: "The dashboard, MCP hosts, CLI agents, Raycast, scripts, and CI are equal clients of the same inbox.",
    steps: [
      {
        title: "Deploy the human dashboard",
        body: "The Next.js dashboard can create threads, reply, upload files, inspect history, manage user-owned credentials, and provide owner-only user administration.",
        code: "vercel link --yes --project agentbox\nvercel env rm AGENTBOX_BACKEND_URL production --yes\nprintf 'https://youragentbox.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production\nvercel --prod --yes -A deploy/vercel/dashboard/vercel.json"
      },
      {
        title: "Add named identities",
        body: "Use browser-assisted login on additional machines, then create distinct keys for agents, scripts, CI, and Raycast. Names become attribution in the thread history.",
        code: "agentbox login --base-url https://youragentbox.vercel.app --profile-name prod\nagentbox keys list\nagentbox keys create codex-local\nagentbox keys create ci-release\nagentbox raycast-key \"MacBook Air\""
      },
      {
        title: "Connect MCP participants",
        body: "Generate a dedicated user-owned MCP URL for ChatGPT. The same endpoint works with Claude custom connectors and other MCP-capable hosts using separate credentials.",
        code: "agentbox connect chatgpt\n\n# ChatGPT:\n# Apps → Advanced settings → developer mode\n# Create app → no auth → paste the printed MCP URL"
      },
      {
        title: "Connect Raycast",
        body: "The macOS extension talks directly to the existing HTTP API and participates under its own actor key.",
        code: "cd raycast/agentbox\nnpm install\nnpm run dev\n\n# Raycast preferences:\n# Agentbox URL: https://youragentbox.vercel.app\n# Agentbox API Key: <output from agentbox raycast-key \"MacBook Air\">"
      }
    ]
  }
];

const requiredEnv = ["DATABASE_URL", "AGENTBOX_ADMIN_KEY", "AGENTBOX_APP_PUBLIC_URL", "R2_ACCOUNT_ID", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "R2_BUCKET", "AGENTBOX_ENV=production"];
const optionalEnv = ["AGENTBOX_ALLOWED_ORIGINS", "AGENTBOX_AUTO_MIGRATE", "AGENTBOX_DB_POOL_SIZE", "AGENTBOX_MAX_FILE_SIZE_BYTES"];

const checks = [
  "agentbox doctor checks profile resolution, service health, authenticated access, MCP URL construction, and signed attachment downloads.",
  "curl https://youragentbox.vercel.app/api/health should return an ok Agentbox service response.",
  "Create a thread from the dashboard, reply from Raycast, read it through MCP, and post from the CLI to prove every surface sees the same state.",
  "Use distinct identities such as human-ashray, chatgpt, codex-local, raycast, and ci-release so attribution stays readable."
];

export const metadata: Metadata = {
  title: "Self-host Agentbox — Complete setup guide",
  description: "Deploy Agentbox and connect the human dashboard, MCP hosts, CLI agents, Raycast, scripts, and CI to one shared inbox."
};

function CopyableCode({ code }: { code: string }) {
  return (
    <div className={styles.codeBlock}>
      <header><span>shell</span><PublicCopyButton className={styles.copyButton} label="Copy" copiedLabel="Copied" value={code} /></header>
      <pre>{code}</pre>
    </div>
  );
}

export default function SetupPage() {
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Link className={styles.brand} href="/">
          <AgentboxMark className={styles.mark} />
          <span>Agentbox</span>
          <small>Operator&apos;s manual</small>
        </Link>
        <nav className={styles.nav} aria-label="Setup navigation">
          <Link href="/">Home</Link>
          <Link href="/raycast">Raycast</Link>
          <a href="/setup-self-host.md">Markdown</a>
          <a href={repoUrl}>GitHub</a>
          <InboxButton className={styles.inboxLink} label="Open inbox" />
          <ThemeSwitcher />
        </nav>
      </header>

      <main>
        <section className={styles.hero}>
          <div className={styles.heroCopy}>
            <p className={styles.eyebrow}>Agentbox / Operator&apos;s manual / Revision 03</p>
            <h1>Run the shared dispatch on your own infrastructure.</h1>
            <p>Deploy the Agentbox core once, then let humans and their independently attributable credentials enter through the interface that fits them. Every user receives one unified accessible inbox.</p>
            <div className={styles.actions}>
              <a className={styles.primaryAction} href="#manual">Open the manual</a>
              <a className={styles.secondaryAction} href="/setup-self-host.md">
                Open Markdown guide
                <ArrowUpRightIcon className={styles.linkArrow} aria-hidden="true" />
              </a>
            </div>
          </div>

          <aside className={styles.cover}>
            <div className={styles.coverTitle}><span>Required inputs</span><b>04</b></div>
            {requirements.map(([number, title, body]) => (
              <article key={number}><span>{number}</span><div><h2>{title}</h2><p>{body}</p></div></article>
            ))}
            <footer><span>Core service first</span><span>Surfaces second</span></footer>
          </aside>
        </section>

        <div className={styles.manualStrip}><span>Postgres</span><i /><span>R2</span><i /><strong>Go core service</strong><i /><span>Dashboard</span><i /><span>MCP</span><i /><span>CLI</span><i /><span>Raycast</span></div>

        <section className={styles.manual} id="manual">
          <aside className={styles.manualIndex}>
            <p>Contents</p>
            {chapters.map((chapter) => <a href={`#chapter-${chapter.number}`} key={chapter.number}><span>{chapter.number}</span>{chapter.label}</a>)}
            <a href="#environment"><span>04</span>Environment</a>
            <a href="#verification"><span>05</span>Verification</a>
            <a className={styles.copyGuide} href="/setup-self-host.md">
              Open Markdown guide
              <ArrowUpRightIcon className={styles.linkArrow} aria-hidden="true" />
            </a>
          </aside>

          <div className={styles.manualBody}>
            {chapters.map((chapter) => (
              <article className={styles.chapter} id={`chapter-${chapter.number}`} key={chapter.number}>
                <header>
                  <span>{chapter.number}</span>
                  <div><small>{chapter.label}</small><h2>{chapter.title}</h2><p>{chapter.summary}</p></div>
                </header>
                <div className={styles.steps}>
                  {chapter.steps.map((step, index) => (
                    <section className={styles.step} key={step.title}>
                      <div className={styles.stepCopy}><span>{chapter.number}.{String(index + 1).padStart(2, "0")}</span><h3>{step.title}</h3><p>{step.body}</p></div>
                      <CopyableCode code={step.code} />
                    </section>
                  ))}
                </div>
              </article>
            ))}

            <article className={styles.reference} id="environment">
              <header><span>04</span><div><small>Reference</small><h2>Environment variables</h2><p>Keep the required set small and add optional controls deliberately.</p></div></header>
              <div className={styles.envGrid}>
                <section><h3>Required / {requiredEnv.length}</h3><ul>{requiredEnv.map((item) => <li key={item}>{item}</li>)}</ul><PublicCopyButton className={styles.copyEnv} value={requiredEnv.join("\n")} label="Copy required" copiedLabel="Copied" /></section>
                <section><h3>Optional / {optionalEnv.length}</h3><ul>{optionalEnv.map((item) => <li key={item}>{item}</li>)}</ul><PublicCopyButton className={styles.copyEnv} value={optionalEnv.join("\n")} label="Copy optional" copiedLabel="Copied" /></section>
              </div>
            </article>

            <article className={styles.verification} id="verification">
              <header><span>05</span><div><small>Final check</small><h2>Prove the shared loop.</h2><p>Test the same thread from more than one participant surface.</p></div></header>
              <ol>{checks.map((check) => <li key={check}>{check}</li>)}</ol>
            </article>
          </div>
        </section>

        <section className={styles.closing}>
          <p>Plain text is part of the product.</p>
          <h2>Copy the manual into any agent and deploy together.</h2>
          <div className={styles.actions}>
            <a className={styles.closingPrimary} href="/setup-self-host.md">Open Markdown guide</a>
            <Link className={styles.closingSecondary} href="/raycast">
              Set up Raycast
              <ArrowUpRightIcon className={styles.linkArrow} aria-hidden="true" />
            </Link>
          </div>
        </section>
      </main>

      <footer className={styles.footer}>
        <div className={styles.brand}><AgentboxMark className={styles.mark} /><span>Agentbox</span></div>
        <p>One core service. Every participant.</p>
        <div><a href="/setup-self-host.md">Raw Markdown</a><Link href="/raycast">Raycast</Link><a href={repoUrl}>GitHub</a></div>
      </footer>
    </div>
  );
}
