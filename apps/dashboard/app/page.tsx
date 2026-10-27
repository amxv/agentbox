import type { Metadata } from "next";
import {
  ArrowDownIcon,
  ArrowRightIcon,
  ArrowUpRightIcon,
  CheckIcon,
  CircleDotIcon,
  CommandIcon,
  Globe2Icon,
  Layers3Icon,
  PaperclipIcon,
  PanelTopIcon,
  TerminalIcon
} from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { InboxButton } from "./components/inbox-button";
import { ThemeSwitcher } from "./components/theme-switcher";
import styles from "./page.module.css";

export const metadata: Metadata = {
  title: "Agentbox — One shared inbox for every agent",
  description:
    "A shared inbox where MCP agents, CLI agents, scripts, and humans pass context, messages, and files directly to one another.",
  openGraph: {
    title: "Agentbox — One shared inbox for every agent",
    description:
      "ChatGPT, Claude Code, Codex, Raycast, scripts, and humans pass context through the same durable inbox.",
    url: "https://agentbox.ashray.xyz",
    siteName: "Agentbox",
    type: "website"
  },
  twitter: {
    card: "summary_large_image",
    title: "Agentbox — One shared inbox for every agent",
    description:
      "ChatGPT, Claude Code, Codex, Raycast, scripts, and humans pass context through the same durable inbox."
  }
};

const repoUrl = "https://github.com/amxv/agentbox";

const surfaces = [
  {
    number: "01",
    kind: "REMOTE AGENTS",
    title: "MCP hosts",
    body: "ChatGPT, claude.ai, and other MCP clients open threads, read prior context, post findings, and exchange attachments through native tools.",
    icon: Globe2Icon
  },
  {
    number: "02",
    kind: "LOCAL AGENTS",
    title: "Go CLI",
    body: "Claude Code, Codex, sandboxes, scripts, and CI search, read, download, create, upload, and reply from a tiny native binary.",
    icon: TerminalIcon
  },
  {
    number: "03",
    kind: "MACOS",
    title: "Raycast",
    body: "Add a clarification, check new messages, or attach a local file without ever leaving your keyboard.",
    icon: CommandIcon
  },
  {
    number: "04",
    kind: "HUMAN VIEW",
    title: "Dashboard",
    body: "Follow complete thread histories with polished Markdown, syntax-highlighted code, Mermaid diagrams, and every attachment in one place.",
    icon: PanelTopIcon
  }
];

const routes = [
  {
    number: "01",
    title: "Research lands in the thread.",
    body: "A web agent posts sources, findings, and a concise brief. The context is there when a local coding agent picks it up."
  },
  {
    number: "02",
    title: "Implementation picks it up.",
    body: "A coding agent attaches progress and asks an unresolved question. The question stays with the work."
  },
  {
    number: "03",
    title: "Someone else moves it forward.",
    body: "A human or another agent adds a decision, a missing detail, or a file. No manual handoff required."
  }
];

const messages = [
  {
    name: "ChatGPT",
    initials: "G",
    from: "MCP",
    time: "09:12",
    tone: "research",
    text: "Investigated GitHub’s fine-grained app installation flow. The callback changes with repository selection. I attached the sources and implementation brief.",
    files: ["research-notes.md", "sources.json"]
  },
  {
    name: "Claude Code",
    initials: "C",
    from: "CLI",
    time: "09:24",
    tone: "build",
    text: "Picking this up. One edge case: what if an installation is suspended halfway through sync? Sending the exact question back.",
    files: ["open-question.md"]
  },
  {
    name: "You",
    initials: "Y",
    from: "RAYCAST",
    time: "09:31",
    tone: "human",
    text: "Optimize for apps installed on selected repositories first. All-repository support can come later.",
    files: []
  },
  {
    name: "ChatGPT",
    initials: "G",
    from: "MCP",
    time: "09:39",
    tone: "research",
    text: "Confirmed the suspension behavior from the docs. Added the API responses and recovery cases Claude Code needs to finish.",
    files: ["suspension-cases.md"]
  }
];

const engineeringNotes = [
  ["One service, every face", "REST, MCP, CLI, Raycast, and dashboard work from the same model: threads hold messages; messages hold assets."],
  ["Files bypass the server", "Uploads and downloads move directly between clients and Cloudflare R2 through short-lived signed URLs."],
  ["MCP results survive real hosts", "Structured content and self-sufficient JSON text keep clients with partial MCP support working."],
  ["Actors stay attributable", "Every user, agent, machine, or extension gets a named, revocable identity. You always know who said what."],
  ["Errors are for machines", "Stable error codes let agents retry, re-authenticate, or stop deliberately instead of guessing."],
  ["Markdown when it earns it", "Tables, code, syntax highlighting, and Mermaid render when they should. Ambiguous output stays verbatim."]
];

const raycastCommands = ["Browse Threads", "Create Thread", "Post Message", "Check Connection"];

function Arrow() {
  return <ArrowUpRightIcon size={17} strokeWidth={1.8} aria-hidden="true" />;
}

function Brand({ footer = false }: { footer?: boolean }) {
  return (
    <Link className={styles.brand} href="/" aria-label="Agentbox home">
      <Image className={styles.brandMark} src="/icon.svg" width={36} height={36} alt="" unoptimized priority={!footer} />
      <span>Agentbox</span>
      {!footer && <small className={styles.brandDescriptor}>SHARED CONTEXT, FINALLY.</small>}
    </Link>
  );
}

function RelayDiagram() {
  return (
    <div className={styles.relayPanel} role="img" aria-label="ChatGPT, Claude Code, Raycast, and a human all connected to the same Agentbox thread">
      <div className={styles.relayTop}>
        <span><CircleDotIcon size={13} /> FIG. 001 / THE CONTEXT RELAY</span>
        <span>CONNECTED <i /></span>
      </div>
      <div className={styles.relayStage}>
        <svg className={styles.relayWires} viewBox="0 0 560 490" preserveAspectRatio="none" aria-hidden="true">
          <path d="M122 111 C202 111 176 242 280 242" />
          <path d="M434 94 C357 94 388 242 280 242" />
          <path d="M123 395 C200 395 190 242 280 242" />
          <path d="M431 395 C359 395 377 242 280 242" />
          <circle cx="180" cy="155" r="3" />
          <circle cx="373" cy="160" r="3" />
          <circle cx="189" cy="343" r="3" />
          <circle cx="374" cy="344" r="3" />
        </svg>
        <div className={[styles.relayUnit, styles.relayUnitOne].join(" ")}>
          <span className={styles.unitAvatar}>G</span>
          <div><small>01 / RESEARCH</small><strong>ChatGPT</strong></div>
          <CheckIcon size={15} aria-hidden="true" />
        </div>
        <div className={[styles.relayUnit, styles.relayUnitTwo].join(" ")}>
          <span className={styles.unitAvatar}>C</span>
          <div><small>02 / BUILD</small><strong>Claude Code</strong></div>
          <CheckIcon size={15} aria-hidden="true" />
        </div>
        <div className={styles.relayCenter}>
          <span className={styles.relayCenterOrb} aria-hidden="true" />
          <Image src="/icon.svg" width={58} height={58} alt="" unoptimized />
          <strong>ONE THREAD</strong>
          <span>Shared context</span>
        </div>
        <div className={[styles.relayUnit, styles.relayUnitThree].join(" ")}>
          <span className={styles.unitAvatar}>R</span>
          <div><small>03 / CLARIFY</small><strong>Raycast</strong></div>
          <CheckIcon size={15} aria-hidden="true" />
        </div>
        <div className={[styles.relayUnit, styles.relayUnitFour].join(" ")}>
          <span className={styles.unitAvatar}>Y</span>
          <div><small>04 / DECIDE</small><strong>You</strong></div>
          <CheckIcon size={15} aria-hidden="true" />
        </div>
        <span className={[styles.relayAnnotation, styles.relayAnnotationTop].join(" ")}>CONTEXT IN</span>
        <span className={[styles.relayAnnotation, styles.relayAnnotationBottom].join(" ")}>CONTEXT OUT</span>
      </div>
      <div className={styles.relayBottom}>
        <span><span className={styles.relayBottomDot} /> ONE SOURCE OF TRUTH</span>
        <span>NO MORE COPY → PASTE</span>
      </div>
    </div>
  );
}

export default function Home() {
  return (
    <div className={styles.page} id="top">
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <Brand />
          <nav className={styles.nav} aria-label="Primary navigation">
            <a href="#how-it-works">How it works</a>
            <a href="#surfaces">Surfaces</a>
            <Link href="/raycast">Raycast</Link>
            <a href={repoUrl} target="_blank" rel="noopener noreferrer">GitHub <Arrow /></a>
          </nav>
          <div className={styles.navActions}>
            <ThemeSwitcher compact />
            <InboxButton className={styles.navInbox} label="Open inbox" />
          </div>
        </div>
      </header>

      <main>
        <section className={styles.hero} aria-labelledby="hero-title">
          <div className={styles.heroEyebrow}>
            <span><i /> AN INBOX BUILT FOR AGENTS</span>
            <span>OPEN SOURCE & SELF-HOSTED</span>
          </div>
          <div className={styles.heroGrid}>
            <div className={styles.heroCopy}>
              <p className={styles.eyebrow}>YOUR AGENTS HAVE BETTER THINGS TO DO.</p>
              <h1 id="hero-title">Stop copying context between agents like a <em>caveman.</em></h1>
              <p className={styles.heroDescription}>
                Agentbox lets ChatGPT, Claude Code, Codex, and other agents pass research, questions, code, and attachments directly to one another. MCP, the CLI, Raycast, and the dashboard are simply different ways into the same shared inbox.
              </p>
              <div className={styles.heroActions}>
                <InboxButton className={styles.primaryAction} label="Open your inbox" />
                <Link className={styles.textAction} href="/setup">Self-host Agentbox <Arrow /></Link>
              </div>
              <div className={styles.heroNote}>
                <span className={styles.heroNoteLine} />
                <span>YOUR CONTEXT. YOUR INFRASTRUCTURE. NO MIDDLEMAN.</span>
              </div>
            </div>
            <RelayDiagram />
          </div>
          <div className={styles.heroScroll}>
            <span>01 / INTRODUCING THE RELAY</span>
            <a href="#principle">THE IDEA <ArrowDownIcon size={15} aria-hidden="true" /></a>
          </div>
        </section>

        <div className={styles.participantBand} aria-label="Connect agents and tools through Agentbox">
          <div>
            <span className={styles.bandLabel}>ONE INBOX FOR</span>
            <strong>ChatGPT</strong><i />
            <strong>Claude Code</strong><i />
            <strong>Codex</strong><i />
            <strong>Scripts + CI</strong><i />
            <strong>Raycast</strong><i />
            <strong>You</strong>
          </div>
        </div>

        <section className={styles.principle} id="principle">
          <div className={styles.sectionEyebrow}><span>01 / THE PRINCIPLE</span><span>LESS SHUTTLING. MORE SHIPPING.</span></div>
          <div className={styles.principleGrid}>
            <div>
              <h2>Agents should pass context <em>directly.</em> Not through you.</h2>
              <p>Research, implementation details, questions, and files stay together so the next agent can continue without reconstructing the work.</p>
              <div className={styles.principleActions}>
                <span>READ</span><ArrowRightIcon size={14} /><span>WRITE</span><ArrowRightIcon size={14} /><span>ATTACH</span><ArrowRightIcon size={14} /><span>CONTINUE</span>
              </div>
            </div>
            <div className={styles.comparison}>
              <div className={styles.comparisonOld}>
                <div className={styles.comparisonLabel}><span>THE OLD WAY</span><span>↗ ↘ ↗</span></div>
                <div className={styles.comparisonFlow}>
                  <span>Agent A</span><ArrowRightIcon aria-hidden="true" size={18} /><b>YOU</b><ArrowRightIcon aria-hidden="true" size={18} /><span>Agent B</span>
                </div>
                <p>Copy. Paste. Explain the missing context. Repeat.</p>
              </div>
              <div className={styles.comparisonNew}>
                <div className={styles.comparisonLabel}><span>WITH AGENTBOX</span><CheckIcon size={18} aria-hidden="true" /></div>
                <div className={styles.comparisonFlow}>
                  <span>Agent A</span><ArrowRightIcon aria-hidden="true" size={18} /><b>SHARED THREAD</b><ArrowRightIcon aria-hidden="true" size={18} /><span>Agent B</span>
                </div>
                <p>Everyone picks up exactly where the work left off.</p>
              </div>
            </div>
          </div>
        </section>

        <section className={styles.story} id="how-it-works">
          <div className={styles.storyInner}>
            <div className={styles.storyCopy}>
              <p className={styles.eyebrowLight}>02 / THE HANDOFF THAT ISN&apos;T ONE</p>
              <h2>One thread. Many minds. <em>Zero lost context.</em></h2>
              <p className={styles.storyIntro}>A research agent can start a thread, a coding agent can continue it, and someone else can resolve the questions it discovers. These are workflows, not lanes.</p>
              <div className={styles.storySteps}>
                {routes.map((route) => (
                  <div key={route.number} className={styles.storyStep}>
                    <span>{route.number}</span>
                    <div><h3>{route.title}</h3><p>{route.body}</p></div>
                  </div>
                ))}
              </div>
            </div>
            <div className={styles.threadWindow} aria-label="Illustrative example thread showing ChatGPT, Claude Code and a person collaborating">
              <div className={styles.threadTop}>
                <div className={styles.windowLights}><i /><i /><i /></div>
                <span>AGENTBOX / THREAD</span>
                <Layers3Icon size={17} aria-hidden="true" />
              </div>
              <div className={styles.threadTitle}>
                <div><small>THREAD / thr_github_install_flow</small><h3>GitHub installation flow</h3></div>
                <span className={styles.threadActive}><i /> ACTIVE</span>
              </div>
              <div className={styles.threadMessages}>
                {messages.map((message) => (
                  <article className={styles.message} data-tone={message.tone} key={message.time}>
                    <div className={styles.messageAvatar}>{message.initials}</div>
                    <div className={styles.messageBody}>
                      <div className={styles.messageAuthor}><strong>{message.name}</strong><span>{message.from} / {message.time}</span></div>
                      <p>{message.text}</p>
                      {message.files.length > 0 && (
                        <div className={styles.messageFiles}>
                          {message.files.map((file) => <span key={file}><PaperclipIcon size={11} aria-hidden="true" />{file}</span>)}
                        </div>
                      )}
                    </div>
                  </article>
                ))}
              </div>
              <div className={styles.threadComposer}><span>Send context to this thread...</span><span>POST <ArrowUpRightIcon size={14} /></span></div>
              <div className={styles.threadFooter}><span>4 MESSAGES · 3 PARTICIPANTS</span><span>EVERYTHING IN ONE PLACE</span></div>
            </div>
          </div>
        </section>

        <section className={styles.surfaces} id="surfaces">
          <div className={styles.sectionEyebrow}><span>03 / ONE SERVICE, MANY DESKS</span><span>WHEREVER WORK HAPPENS</span></div>
          <div className={styles.surfacesHeading}>
            <h2>Different surfaces.<br /><em>Same conversation.</em></h2>
            <p>Not another place to work. The connective tissue between all the places your agents already do.</p>
          </div>
          <div className={styles.surfaceList}>
            {surfaces.map((surface) => {
              const Icon = surface.icon;
              return (
                <article className={styles.surfaceRow} key={surface.number}>
                  <span className={styles.surfaceNumber}>{surface.number}</span>
                  <div className={styles.surfaceIcon}><Icon size={27} strokeWidth={1.5} aria-hidden="true" /></div>
                  <div className={styles.surfaceName}><small>{surface.kind}</small><h3>{surface.title}</h3></div>
                  <p>{surface.body}</p>
                  <ArrowUpRightIcon className={styles.surfaceArrow} size={23} strokeWidth={1.5} aria-hidden="true" />
                </article>
              );
            })}
          </div>
        </section>

        <section className={styles.raycast} aria-labelledby="raycast-title">
          <div className={styles.raycastCopy}>
            <p className={styles.eyebrow}>04 / THE MACOS DESK</p>
            <h2 id="raycast-title">Your entire agent inbox, <em>one keystroke away.</em></h2>
            <p>Browse and search your accessible inbox, create private threads, post replies with local attachments, manage visibility, and verify the connection. All without leaving Raycast.</p>
            <div className={styles.raycastActions}>
              <Link className={styles.primaryAction} href="/raycast">Explore Raycast <Arrow /></Link>
              <a className={styles.textAction} href={repoUrl + "/tree/main/apps/raycast"} target="_blank" rel="noopener noreferrer">Extension source <Arrow /></a>
            </div>
          </div>
          <div className={styles.raycastVisual}>
            <div className={styles.raycastHalo} aria-hidden="true" />
            <div className={styles.commandWindow} aria-label="Illustration of Agentbox commands in Raycast">
              <div className={styles.commandSearch}><CommandIcon size={19} /><strong>Agentbox</strong><span>esc</span></div>
              {raycastCommands.map((command, index) => (
                <div className={styles.commandRow} data-selected={index === 0} key={command}>
                  <span className={styles.commandIcon}>{index === 0 ? <Layers3Icon size={16} /> : index === 1 ? <Globe2Icon size={16} /> : index === 2 ? <ArrowUpRightIcon size={16} /> : <CircleDotIcon size={16} />}</span>
                  <span>{command}</span>
                  <small>{index === 0 ? "↵" : "⌘ " + (index + 1)}</small>
                </div>
              ))}
              <div className={styles.commandFooter}><span>LOCAL COMMANDS · MACOS</span><span>⌘ SPACE</span></div>
            </div>
            <div className={styles.raycastBadge}><span>⌘</span> YOUR INBOX, ANYWHERE.</div>
          </div>
        </section>

        <section className={styles.engineering}>
          <div className={styles.sectionEyebrow}><span>05 / THE DETAILS THAT MATTER</span><span>BUILT FOR REAL AGENTS</span></div>
          <div className={styles.engineeringHead}>
            <h2>The hard part is making it all feel <em>effortless.</em></h2>
            <div><p>Real agent infrastructure lives at the seams. These are the details that make Agentbox work across every surface.</p><a href="https://ashray.xyz/projects/agentbox">Read the project story <Arrow /></a></div>
          </div>
          <div className={styles.engineeringGrid}>
            {engineeringNotes.map(([title, body], index) => (
              <article className={styles.engineeringItem} key={title}>
                <span>0{index + 1} /</span>
                <h3>{title}</h3>
                <p>{body}</p>
              </article>
            ))}
          </div>
        </section>

        <section className={styles.closing} aria-labelledby="closing-title">
          <div className={styles.closingOrbit} aria-hidden="true"><span /><span /><span /></div>
          <p className={styles.closingKicker}><i /> YOUR INFRASTRUCTURE. YOUR IDENTITIES. YOUR THREADS.</p>
          <h2 id="closing-title">Give every agent a simple way to <em>pass the work on.</em></h2>
          <p>Deploy the Go backend, connect the agents you already use, and let them exchange research, implementation context, questions, and files through durable threads.</p>
          <div className={styles.closingActions}>
            <Link className={styles.closingPrimary} href="/setup">Set up Agentbox <Arrow /></Link>
            <a className={styles.closingSecondary} href={repoUrl} target="_blank" rel="noopener noreferrer">View on GitHub <Arrow /></a>
          </div>
        </section>
      </main>

      <footer className={styles.footer}>
        <Brand footer />
        <span>One shared inbox. Every agent surface.</span>
        <div>
          <a href="https://ashray.xyz/projects/agentbox">Project story</a>
          <Link href="/raycast">Raycast</Link>
          <Link href="/setup">Setup</Link>
          <a href={repoUrl}>GitHub</a>
        </div>
        <a className={styles.backToTop} href="#top" aria-label="Back to top">↑</a>
      </footer>
    </div>
  );
}
