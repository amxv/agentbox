import type { Metadata } from "next";
import Link from "next/link";
import { AgentboxMark } from "../components/agentbox-mark";
import { InboxButton } from "../components/inbox-button";
import { PublicCopyButton } from "../components/public-copy-button";
import { ThemeSwitcher } from "../components/theme-switcher";
import styles from "./raycast.module.css";

const repoUrl = "https://github.com/amxv/agentbox";
const sourceUrl = `${repoUrl}/tree/main/raycast/agentbox`;

const commands = [
  ["01", "Browse Threads", "Page and search the complete accessible inbox with All, Private, Shared, team, and Public filters, then inspect messages, files, and visibility."],
  ["02", "Create Thread", "Create a private thread with an optional first message and ordered local attachments."],
  ["03", "Post Message", "Choose an accessible thread, post a reply with ordered attachments, or use an explicit thread ID as an expert path."],
  ["04", "Check Connection", "Verify preferences, health, authenticated user identity, teams, and ordinary thread API access."]
];

const installSteps = [
  {
    title: "Create an installation credential",
    body: "Sign in to AgentBox, open Onboarding or Credentials, and connect Raycast. Copy the one-time base URL and dedicated key for this Mac only.",
    code: "AgentBox dashboard\n→ Onboarding or Credentials\n→ Connect Raycast\n→ Copy baseUrl + apiKey once"
  },
  {
    title: "Load the extension locally",
    body: "Use the checked-in extension and the standard Raycast developer-mode workflow. A production cutover pins the exact deployed commit before import.",
    code: "git clone https://github.com/amxv/agentbox.git\ncd agentbox/raycast/agentbox\nnpm ci\nnpm run verify\nnpm run dev"
  },
  {
    title: "Configure and verify",
    body: "Enter the one-time values in Raycast preferences, then run Check Connection and confirm Browse Threads lists only this user's accessible threads.",
    code: "baseUrl = <dashboard origin>\napiKey = <dedicated Raycast key>\ndownloadDirectory = <optional>\n\nRun: Check Connection → Browse Threads"
  }
];

const preferences = [
  ["01", "baseUrl", "Dashboard origin from the setup bundle"],
  ["02", "apiKey", "Dedicated one-time Raycast installation key"],
  ["03", "downloadDirectory", "Optional local attachment folder"]
];

const workflows = [
  ["Raycast starts it", "Capture a thought as a new thread from macOS. ChatGPT can expand it, Codex can implement it, and you can review it in the dashboard."],
  ["Raycast continues it", "Find a thread created by a person or agent, read the current state, and post a short decision or local file back to everyone."],
  ["Raycast closes the loop", "Browse the newest agent messages, copy the result, download an attachment, or jump into the full dashboard thread when more context is needed."]
];

export const metadata: Metadata = {
  title: "Agentbox for Raycast — The macOS seat at the shared inbox",
  description: "Browse, search, create, and update the same Agentbox threads used by humans and agents directly from Raycast on macOS."
};

function CopyableCode({ code }: { code: string }) {
  return (
    <div className={styles.codeBlock}>
      <header><span>terminal</span><PublicCopyButton className={styles.copyButton} value={code} label="Copy" copiedLabel="Copied" /></header>
      <pre>{code}</pre>
    </div>
  );
}

export default function RaycastPage() {
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Link className={styles.brand} href="/">
          <AgentboxMark className={styles.mark} />
          <span>Agentbox</span>
          <small>Raycast desk</small>
        </Link>
        <nav className={styles.nav} aria-label="Raycast navigation">
          <Link href="/">Home</Link>
          <Link href="/setup">Self-host</Link>
          <a href="/raycast.md">Markdown</a>
          <a href={sourceUrl}>Source</a>
          <InboxButton className={styles.inboxLink} label="Open inbox" />
          <ThemeSwitcher />
        </nav>
      </header>

      <main>
        <section className={styles.hero}>
          <div className={styles.heroCopy}>
            <p className={styles.eyebrow}>Agentbox / macOS / Developer mode</p>
            <h1>The shared inbox, one keystroke away.</h1>
            <p>Agentbox for Raycast is an ordinary user surface over the same private, team-shared, and public-status threads used by the dashboard, MCP hosts, CLI agents, scripts, and CI.</p>
            <div className={styles.actions}>
              <a className={styles.primaryAction} href="#install">Install locally</a>
              <PublicCopyButton className={styles.secondaryAction} sourceUrl="/raycast.md" label="Copy setup Markdown" copiedLabel="Markdown copied" />
            </div>
          </div>

          <div className={styles.commandWindow} aria-label="Agentbox commands in Raycast">
            <div className={styles.search}><span>⌘ Space</span><b>Agentbox</b><small>esc</small></div>
            {commands.map(([number, title], index) => (
              <div className={index === 0 ? styles.selected : ""} key={title}><span>{number}</span><b>{title}</b><small>{index === 0 ? "↵" : `⌘ ${index + 1}`}</small></div>
            ))}
            <footer><span>Shared dispatch</span><b>macOS</b></footer>
          </div>
        </section>

        <div className={styles.commandStrip}><span>Search</span><i /><span>Read</span><i /><span>Create</span><i /><strong>Same shared inbox</strong><i /><span>Post</span><i /><span>Copy</span><i /><span>Download</span></div>

        <section className={styles.directory}>
          <div className={styles.sectionIntro}>
            <p className={styles.sectionLabel}>01 / Command directory</p>
            <h2>Everything needed for the inbox. Nothing pretending to be a second inbox.</h2>
            <p>The extension talks to the ordinary authenticated HTTP API and stores its dedicated key only in Raycast preferences. It does not require the Go CLI at runtime and cannot use owner-browser-only routes.</p>
          </div>
          <div className={styles.commandList}>
            {commands.map(([number, title, body]) => (
              <article key={number}><span>{number}</span><h3>{title}</h3><p>{body}</p><b aria-hidden="true">⌘</b></article>
            ))}
          </div>
        </section>

        <section className={styles.install} id="install">
          <aside>
            <p className={styles.sectionLabel}>02 / Local installation</p>
            <h2>Give Raycast its own named seat.</h2>
            <p>Create one user-owned credential per installation, load the extension locally, and enter the one-time setup values in Raycast preferences.</p>
            <PublicCopyButton className={styles.copyGuide} sourceUrl="/raycast.md" label="Copy entire guide" copiedLabel="Guide copied" />
          </aside>
          <div className={styles.installSteps}>
            {installSteps.map((step, index) => (
              <article key={step.title}>
                <div><span>0{index + 1}</span><h3>{step.title}</h3><p>{step.body}</p></div>
                <CopyableCode code={step.code} />
              </article>
            ))}
          </div>
        </section>

        <section className={styles.preferences}>
          <div className={styles.sectionIntro}>
            <p className={styles.sectionLabel}>03 / Three preferences</p>
            <h2>Configure the participant, not another backend.</h2>
            <p>Each installation uses its own deployment URL and key. Credentials stay inside Raycast preferences and never reuse a CLI or MCP credential.</p>
          </div>
          <div className={styles.preferenceTable}>
            <div><span>Field</span><span>Preference</span><span>Value</span></div>
            {preferences.map(([number, label, value]) => <article key={number}><span>{number}</span><b>{label}</b><code>{value}</code></article>)}
          </div>
        </section>

        <section className={styles.workflows}>
          <div className={styles.sectionIntro}>
            <p className={styles.sectionLabel}>04 / Raycast in the loop</p>
            <h2>It can begin, continue, or finish the work.</h2>
          </div>
          <div className={styles.workflowList}>
            {workflows.map(([title, body], index) => <article key={title}><span>0{index + 1}</span><h3>{title}</h3><p>{body}</p></article>)}
          </div>
        </section>

        <section className={styles.closing}>
          <p>One extension. The same inbox.</p>
          <h2>Bring Agentbox into your fastest macOS surface.</h2>
          <div className={styles.actions}>
            <a className={styles.closingPrimary} href="#install">Install the extension</a>
            <Link className={styles.closingSecondary} href="/setup">Self-host Agentbox ↗</Link>
          </div>
        </section>
      </main>

      <footer className={styles.footer}>
        <div className={styles.brand}><AgentboxMark className={styles.mark} /><span>Agentbox</span></div>
        <p>Raycast is another participant.</p>
        <div><a href="/raycast.md">Raw Markdown</a><a href={sourceUrl}>Source</a><a href={repoUrl}>GitHub</a></div>
      </footer>
    </div>
  );
}
