"use client";

import type { LucideIcon } from "lucide-react";
import {
  BotMessageSquareIcon,
  CircleCheckIcon,
  InboxIcon,
  PlugZapIcon,
  RocketIcon,
  RotateCwIcon,
  ShieldAlertIcon,
  SkipForwardIcon,
  SparklesIcon,
  TerminalIcon
} from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle
} from "@/components/ui/card";
import { Progress, ProgressLabel, ProgressValue } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";
import { CopyButton } from "../components/copy-button";
import { AppNav } from "../components/app-nav";
import {
  DetailRow,
  MonoValue,
  PanelHeader,
  PanelMain,
  PanelPage
} from "../components/panel-shell";

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
  icon: LucideIcon;
}> = [
  {
    id: "chatgpt",
    eyebrow: "Remote MCP",
    title: "Connect ChatGPT",
    description: "Give ChatGPT its own revocable MCP credential while keeping every message attributed to ChatGPT.",
    actor: "You · ChatGPT",
    icon: BotMessageSquareIcon
  },
  {
    id: "claude",
    eyebrow: "Remote MCP",
    title: "Connect Claude",
    description: "Create a separate Claude connector. Its secret and rotation lifecycle never affect ChatGPT.",
    actor: "You · Claude",
    icon: SparklesIcon
  },
  {
    id: "local",
    eyebrow: "Local machine",
    title: "Connect a coding agent",
    description: "Generate a one-machine setup prompt for Codex, Claude Code, or another local coding agent.",
    actor: "You · Local CLI",
    icon: TerminalIcon
  },
  {
    id: "raycast",
    eyebrow: "Local Raycast",
    title: "Connect Raycast",
    description: "Create one dedicated credential for this Mac and load the checked-in extension in Raycast developer mode.",
    actor: "You · Raycast",
    icon: RocketIcon
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
  const [rotateConnector, setRotateConnector] = useState<Connector | null>(null);

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
  const progressValue = (activeCount / connectors.length) * 100;

  async function connect(connector: Connector) {
    const active = Boolean(steps.get(connector)?.credential);
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
      setRotateConnector(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      await load();
    } finally {
      setBusy(null);
    }
  }

  function requestConnection(connector: Connector) {
    if (steps.get(connector)?.credential) {
      setRotateConnector(connector);
      return;
    }
    void connect(connector);
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

  const rotatingStep = rotateConnector ? steps.get(rotateConnector) : undefined;

  return (
    <PanelPage>
      <AppNav title="Connect agents" />
      <PanelMain>
        <PanelHeader
          eyebrow={state?.dismissed_at ? "Resume setup" : "One identity, separate actors"}
          title="Bring your agents into the same inbox."
          description="Each connection gets its own credential and actor label, but all four act for your user. Private threads stay private until you explicitly share them."
          aside={
            <Card>
              <CardHeader>
                <CardTitle>{activeCount} of {connectors.length} connected</CardTitle>
                <CardDescription>
                  {activeCount === connectors.length ? "Every supported surface is ready." : "Connect only the surfaces you actually use."}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Progress value={progressValue}>
                  <ProgressLabel>Connection progress</ProgressLabel>
                  <ProgressValue>{(_, value) => `${Math.round(value ?? 0)}%`}</ProgressValue>
                </Progress>
              </CardContent>
            </Card>
          }
        />

        {error ? (
          <Alert variant="destructive">
            <ShieldAlertIcon />
            <AlertTitle>Setup action failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <section className="grid gap-4 lg:grid-cols-2" aria-label="Agent connections" aria-busy={loading}>
          {loading ? Array.from({ length: 4 }).map((_, index) => <Skeleton className="h-72" key={index} />) : null}
          {!loading ? connectors.map((connector, index) => {
            const Icon = connector.icon;
            const step = steps.get(connector.id);
            const active = Boolean(step?.credential);
            const completedBefore = Boolean(step?.completed_at);
            const result = results[connector.id];
            const status = active ? "Connected" : completedBefore ? "Needs reconnect" : "Not connected";
            return (
              <Card key={connector.id}>
                <CardHeader className="border-b">
                  <div className="flex min-w-0 items-start gap-3">
                    <span className="flex size-10 shrink-0 items-center justify-center border bg-muted">
                      <Icon />
                    </span>
                    <div className="flex min-w-0 flex-col gap-1">
                      <span className="font-mono text-[0.65rem] tracking-[0.1em] text-muted-foreground uppercase">0{index + 1} / {connector.eyebrow}</span>
                      <CardTitle>{connector.title}</CardTitle>
                      <CardDescription>{connector.description}</CardDescription>
                    </div>
                  </div>
                  <CardAction>
                    <Badge variant={active ? "default" : completedBefore ? "secondary" : "outline"}>{status}</Badge>
                  </CardAction>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                  <dl>
                    <DetailRow label="Messages appear as" value={connector.actor} />
                    <DetailRow label="Credential" value={step?.credential?.name ?? "Not created"} />
                    <DetailRow label="Updated" value={formatDate(step?.credential?.updated_at)} />
                  </dl>
                  {step?.credential ? <MonoValue>{step.credential.key_masked || step.credential.id}</MonoValue> : null}
                  <Button type="button" disabled={loading || busy !== null} onClick={() => requestConnection(connector.id)}>
                    {busy === connector.id ? <Spinner data-icon="inline-start" /> : active ? <RotateCwIcon data-icon="inline-start" /> : <PlugZapIcon data-icon="inline-start" />}
                    {busy === connector.id ? "Generating" : active ? "Rotate credential" : completedBefore ? "Recreate connection" : connector.title}
                  </Button>
                  {result ? <ConnectionOutput connector={connector.id} result={result} /> : null}
                </CardContent>
              </Card>
            );
          }) : null}
        </section>

        <Card>
          <CardHeader className="border-b">
            <CardTitle>Your inbox already works</CardTitle>
            <CardDescription>Skipping does not create credentials or block the account. Return from the navigation whenever you are ready.</CardDescription>
          </CardHeader>
          <CardFooter className="flex flex-wrap gap-2">
            <Button variant="outline" type="button" onClick={() => void skip()} disabled={busy !== null}>
              {busy === "skip" ? <Spinner data-icon="inline-start" /> : <SkipForwardIcon data-icon="inline-start" />}
              Skip for now
            </Button>
            <Link className={cn(buttonVariants({ variant: "default" }))} href="/threads">
              <InboxIcon data-icon="inline-start" />
              Open inbox
            </Link>
          </CardFooter>
        </Card>
      </PanelMain>

      <AlertDialog open={Boolean(rotateConnector)} onOpenChange={(open) => { if (!open) setRotateConnector(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia><RotateCwIcon /></AlertDialogMedia>
            <AlertDialogTitle>Rotate this connection?</AlertDialogTitle>
            <AlertDialogDescription>
              The current {rotatingStep?.credential?.name ?? "connector"} secret will stop working immediately. A replacement setup value will be shown once.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={Boolean(rotateConnector && busy === rotateConnector)}>Cancel</AlertDialogCancel>
            <AlertDialogAction disabled={!rotateConnector || busy === rotateConnector} onClick={() => { if (rotateConnector) void connect(rotateConnector); }}>
              {rotateConnector && busy === rotateConnector ? <Spinner data-icon="inline-start" /> : <RotateCwIcon data-icon="inline-start" />}
              Rotate credential
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PanelPage>
  );
}

function ConnectionOutput({ connector, result }: { connector: Connector; result: ConnectionResult }) {
  if (connector === "raycast") return <RaycastConnectionOutput result={result} />;
  const value = connector === "local" ? result.setup_prompt ?? "" : result.mcp_url ?? "";
  return (
    <Card size="sm">
      <CardHeader className="border-b">
        <CardTitle>{connector === "local" ? "Local setup prompt" : "Authenticated MCP URL"}</CardTitle>
        <CardDescription>Generated once. Save it before leaving this page.</CardDescription>
        <CardAction><CircleCheckIcon /></CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <CopyableValue value={value} label={connector === "local" ? "setup prompt" : "MCP URL"} multiline={connector === "local"} />
        {connector === "local" && result.profile_command ? <CopyableValue value={result.profile_command} label="profile command" /> : null}
        <Separator />
        <InstructionList instructions={result.instructions} />
      </CardContent>
    </Card>
  );
}

function RaycastConnectionOutput({ result }: { result: ConnectionResult }) {
  const setup = result.raycast_setup;
  if (!setup) return null;
  return (
    <Card size="sm">
      <CardHeader className="border-b">
        <CardTitle>Raycast developer-mode setup</CardTitle>
        <CardDescription>Generated once for this Mac.</CardDescription>
        <CardAction><CircleCheckIcon /></CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <dl>
          <DetailRow label="Repository" value={<MonoValue>{setup.repository_url}</MonoValue>} />
          <DetailRow label="Extension path" value={<MonoValue>{setup.extension_path}</MonoValue>} />
        </dl>
        <Separator />
        {setup.install_commands.map((command) => <CopyableValue key={command} value={command} label="install command" />)}
        <Separator />
        {setup.preferences.map((preference) => <CopyableValue key={preference.name} value={preference.value} label={preference.title} />)}
        <Alert>
          <AlertTitle>Final connection check</AlertTitle>
          <AlertDescription>{setup.final_check}</AlertDescription>
        </Alert>
        <InstructionList instructions={result.instructions} />
      </CardContent>
    </Card>
  );
}

function CopyableValue({ value, label, multiline = false }: { value: string; label: string; multiline?: boolean }) {
  return (
    <div className="flex min-w-0 items-start gap-2 border bg-muted/40 p-2">
      {multiline ? (
        <pre className="max-h-72 min-w-0 flex-1 overflow-auto whitespace-pre-wrap break-words font-mono text-[0.72rem] leading-relaxed">{value}</pre>
      ) : (
        <MonoValue className="flex-1 text-foreground">{value}</MonoValue>
      )}
      <CopyButton value={value} label={`Copy ${label}`} />
    </div>
  );
}

function InstructionList({ instructions }: { instructions: string[] }) {
  return (
    <ol className="flex list-decimal flex-col gap-2 pl-5 text-xs/relaxed text-muted-foreground">
      {instructions.map((instruction) => <li key={instruction}>{instruction}</li>)}
    </ol>
  );
}
