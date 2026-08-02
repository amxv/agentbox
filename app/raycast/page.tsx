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
  ["01", "Latest Messages", "Browse recent messages across every thread, copy content, inspect context, open the source thread, and work with attachments."],
  ["02", "Search Threads", "Find threads by title or message content, inspect messages, open dashboard links, post replies, and copy thread details."],
  ["03", "List Threads", "Browse the shared inbox chronologically and jump into the same durable threads used by every other participant."],
  ["04", "Post Message", "Reply to an existing thread or create a new one with an optional first message and local attachments from macOS."],
  ["05", "Check Connection", "Verify preferences, health, authenticated thread access, attachment behavior, and MCP URL construction."]
];

const installSteps = [
  {
    title: "Create a Raycast identity",
    body: "Run this from an authenticated user profile. The printed actor key belongs to that user and appears as its own participant in thread attribution.",
    code: "agentbox login --base-url https://youragentbox.vercel.app --profile-name prod\nagentbox raycast-key"
  },
  {
    title: "Load the extension locally",
    body: "The extension lives inside the Agentbox repository and uses the standard Raycast development workflow.",
    code: "git clone https://github.com/amxv/agentbox.git\ncd agentbox/raycast/agentbox\nnpm install\nnpm run dev"
  },
  {
    title: "Validate before publishing",
    body: "Run the Raycast checks from the extension directory. Private team publishing is available to the configured owner when appropriate.",
    code: "npm run lint\nnpm run build\n\n# Private team store, maintainer only:\nnpm run publish"
  }
];

const preferences = [
  ["01", "Agentbox URL", "https://youragentbox.vercel.app"],
  ["02", "Agentbox API Key", "The output from agentbox raycast-key"],
  ["03", "Attachment folder", "Optional; defaults to ~/Downloads/Agentbox"]
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
            <p className={styles.eyebrow}>Agentbox / macOS desk / Five commands</p>
            <h1>The shared inbox, one keystroke away.</h1>
            <p>Agentbox for Raycast is not a companion viewer or a one-way shortcut. It is another full participant surface for the same threads, messages, files, identities, and history used by the dashboard, MCP hosts, CLI agents, scripts, and CI.</p>
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
            <p>The extension talks to the existing HTTP API and stores its actor key only in Raycast preferences. It does not require the Go CLI at runtime.</p>
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
            <p>Create a user-owned Raycast credential, load the extension, and paste the URL and key into Raycast preferences.</p>
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
            <p>Each person can use their own URL and actor key. Credentials stay inside Raycast preferences and never touch the CLI profile.</p>
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
