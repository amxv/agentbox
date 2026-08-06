"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { CopyButton } from "../components/copy-button";
import { AppNav } from "../components/app-nav";
import styles from "./onboarding.module.css";

type Connector = "chatgpt" | "claude" | "local" | "raycast";

type Credential = {
  id: string;
  user_id: string;
  name: string;
  purpose: string;
  key?: string;
  key_masked?: string;
  created_at: string;
  updated_at: string;
};

type OnboardingStep = {
  connector: Connector;
  completed_at?: string;
  updated_at?: string;
  credential?: Credential;
};

type OnboardingState = {
  user_id: string;
  dismissed_at?: string;
  created_at?: string;
  updated_at?: string;
  steps: OnboardingStep[];
};

type ConnectionResult = {
  connector: Connector;
  credential: Credential;
  onboarding: OnboardingState;
  mcp_url?: string;
  profile_command?: string;
  setup_prompt?: string;
  raycast_setup?: RaycastSetup;
  instructions: string[];
};

type RaycastSetup = {
  base_url: string;
  api_key: string;
  repository_url: string;
  extension_path: string;
  install_commands: string[];
  preferences: Array<{
    name: string;
    title: string;
    value: string;
    secret?: boolean;
  }>;
  final_check: string;
};

const connectors: Array<{
  id: Connector;
  eyebrow: string;
  title: string;
  description: string;
  actor: string;
  glyph: string;
}> = [
  {
    id: "chatgpt",
    eyebrow: "Remote MCP",
    title: "Connect ChatGPT",
    description: "Give ChatGPT its own revocable MCP credential while keeping every message attributed to ChatGPT.",
    actor: "You · ChatGPT",
    glyph: "◎"
  },
  {
    id: "claude",
    eyebrow: "Remote MCP",
    title: "Connect Claude",
    description: "Create a separate Claude connector. Its secret and rotation lifecycle never affect ChatGPT.",
    actor: "You · Claude",
    glyph: "✦"
  },
  {
    id: "local",
    eyebrow: "Local machine",
    title: "Connect a coding agent",
    description: "Generate a one-machine setup prompt for Codex, Claude Code, or another local coding agent.",
    actor: "You · Local CLI",
    glyph: "⌁"
  },
  {
    id: "raycast",
    eyebrow: "Local Raycast",
    title: "Connect Raycast",
    description: "Create one dedicated credential for this Mac and load the checked-in extension in Raycast developer mode.",
    actor: "You · Raycast",
    glyph: "↗"
  }
];

async function responseJSON(response: Response) {
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data;
}

function formatDate(value?: string) {
  if (!value) return "Never";
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}

export function OnboardingView() {
  const router = useRouter();
  const [state, setState] = useState<OnboardingState | null>(null);
  const [results, setResults] = useState<Partial<Record<Connector, ConnectionResult>>>({});
  const [busy, setBusy] = useState<Connector | "skip" | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const response = await fetch("/api/onboarding", { cache: "no-store" });
      if (response.status === 401 || response.status === 403) {
        router.replace("/login?next=/onboarding");
        return;
      }
      const data = await responseJSON(response);
      setState(data.onboarding);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [router]);

  useEffect(() => {
    const timer = window.setTimeout(() => { void load(); }, 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  const steps = useMemo(() => new Map((state?.steps ?? []).map((step) => [step.connector, step])), [state]);
  const activeCount = useMemo(() => connectors.filter((connector) => steps.get(connector.id)?.credential).length, [steps]);

  async function connect(connector: Connector) {
    const active = Boolean(steps.get(connector)?.credential);
    if (active && !window.confirm(`Rotate the ${steps.get(connector)?.credential?.name} credential? The current connection will stop working immediately.`)) {
      return;
    }
    setBusy(connector);
    setError(null);
    try {
      const response = await fetch(`/api/onboarding/connectors/${connector}`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ rotate: active })
      });
      const data = await responseJSON(response) as ConnectionResult;
      setState(data.onboarding);
      setResults((current) => ({ ...current, [connector]: data }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      await load();
    } finally {
      setBusy(null);
    }
  }

  async function skip() {
    setBusy("skip");
    setError(null);
    try {
      await responseJSON(await fetch("/api/onboarding/skip", { method: "POST" }));
      router.push("/threads");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(null);
    }
  }

  return (
    <div className={styles.page}>
      <AppNav title="Connect agents" />

      <main className={styles.main}>
        <section className={styles.hero}>
          <div className={styles.heroCopy}>
            <p className={styles.kicker}>{state?.dismissed_at ? "Resume setup" : "One identity, separate actors"}</p>
            <h1>Bring your agents into the same inbox.</h1>
            <p>Each connection gets its own credential and actor label, but all four act for your user. Your private threads stay private to you unless you explicitly share them later.</p>
          </div>
          <div className={styles.progress}>
            <div><span>Connected</span><strong>{activeCount}<small>/{connectors.length}</small></strong></div>
            <div className={styles.track}><i style={{ width: `${(activeCount / connectors.length) * 100}%` }}/></div>
            <p>{activeCount === connectors.length ? "All surfaces are connected." : "Connect only what you use. You can return anytime."}</p>
          </div>
        </section>

        {error && <div className={styles.error}><strong>Setup action failed.</strong><span>{error}</span></div>}

        <section className={styles.cards} aria-busy={loading}>
          {connectors.map((connector, index) => {
            const step = steps.get(connector.id);
            const active = Boolean(step?.credential);
            const completedBefore = Boolean(step?.completed_at);
            const result = results[connector.id];
            const status = active ? "Connected" : completedBefore ? "Needs reconnect" : "Not connected";
            return (
              <article className={styles.card} key={connector.id}>
                <div className={styles.cardIndex}>0{index + 1}</div>
                <div className={styles.cardHeading}>
                  <div className={styles.glyph}>{connector.glyph}</div>
                  <div><p>{connector.eyebrow}</p><h2>{connector.title}</h2></div>
                </div>
                <p className={styles.description}>{connector.description}</p>
                <div className={styles.actor}><span>Messages appear as</span><strong>{connector.actor}</strong></div>
                <div className={styles.statusRow}>
                  <span className={`${styles.status} ${active ? styles.connected : completedBefore ? styles.reconnect : ""}`}>{status}</span>
                  {step?.credential && <code>{step.credential.key_masked || step.credential.id}</code>}
                </div>
                {step?.credential && <dl className={styles.metadata}><div><dt>Credential</dt><dd>{step.credential.name}</dd></div><div><dt>Updated</dt><dd>{formatDate(step.credential.updated_at)}</dd></div></dl>}
                <button className={styles.primary} type="button" disabled={loading || busy !== null} onClick={() => void connect(connector.id)}>
                  {busy === connector.id ? "Generating…" : active ? "Rotate credential" : completedBefore ? "Recreate connection" : connector.title}
                </button>

                {result && <ConnectionOutput connector={connector.id} result={result}/>}
              </article>
            );
          })}
        </section>

        <section className={styles.footerPanel}>
          <div><p className={styles.kicker}>Nothing is mandatory</p><h2>Your inbox already works.</h2><p>Skipping does not create credentials or block your account. Reopen this page from Credentials whenever you are ready.</p></div>
          <div className={styles.footerActions}><button type="button" onClick={() => void skip()} disabled={busy !== null}>{busy === "skip" ? "Skipping…" : "Skip for now"}</button><Link href="/threads">Open inbox</Link></div>
        </section>
      </main>
    </div>
  );
}

function ConnectionOutput({ connector, result }: { connector: Connector; result: ConnectionResult }) {
  if (connector === "raycast") return <RaycastConnectionOutput result={result}/>;
  const value = connector === "local" ? result.setup_prompt ?? "" : result.mcp_url ?? "";
  return (
    <div className={styles.output}>
      <div className={styles.outputHeader}><div><span>Generated once</span><strong>{connector === "local" ? "Local agent setup prompt" : "Authenticated MCP URL"}</strong></div><CopyButton value={value} label={connector === "local" ? "Copy setup prompt" : "Copy MCP URL"}/></div>
      <pre>{value}</pre>
      {connector === "local" && result.profile_command && <div className={styles.command}><span>Profile command inside prompt</span><code>{result.profile_command}</code></div>}
      <ol>{result.instructions.map((instruction) => <li key={instruction}>{instruction}</li>)}</ol>
      <p className={styles.onceNote}>Save this now. Agentbox stores only the credential hash and will not show this URL or prompt again.</p>
    </div>
  );
}

function RaycastConnectionOutput({ result }: { result: ConnectionResult }) {
  const setup = result.raycast_setup;
  if (!setup) return null;
  const commands = setup.install_commands.join("\n");
  return (
    <div className={styles.output}>
      <div className={styles.outputHeader}>
        <div><span>Generated once</span><strong>Raycast developer-mode setup</strong></div>
        <CopyButton value={commands} label="Copy install commands"/>
      </div>
      <div className={styles.setupMeta}>
        <div><span>Repository</span><code>{setup.repository_url}</code></div>
        <div><span>Extension path</span><code>{setup.extension_path}</code></div>
      </div>
      <pre>{commands}</pre>
      <div className={styles.preferenceList}>
        {setup.preferences.map((preference) => (
          <div className={styles.preference} key={preference.name}>
            <div><span>{preference.title}</span><code>{preference.name}</code></div>
            <code>{preference.value}</code>
            <CopyButton value={preference.value} label={`Copy ${preference.title}`}/>
          </div>
        ))}
      </div>
      <div className={styles.finalCheck}><span>Final connection check</span><p>{setup.final_check}</p></div>
      <ol>{result.instructions.map((instruction) => <li key={instruction}>{instruction}</li>)}</ol>
      <p className={styles.onceNote}>Save the API key now. Agentbox stores only its hash. Rotating this credential disconnects only this Raycast installation.</p>
    </div>
  );
}
