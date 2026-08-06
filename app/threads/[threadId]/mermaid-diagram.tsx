"use client";

import { Maximize2Icon, ShieldAlertIcon, WorkflowIcon } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { CopyButton } from "../../components/copy-button";

function getResolvedTheme() {
  if (typeof window === "undefined") return "light";
  const explicit = document.documentElement.dataset.theme;
  if (explicit === "light" || explicit === "dark") return explicit;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function mermaidThemeConfig(theme: string) {
  if (theme !== "dark") return { theme: "neutral" as const };
  return {
    theme: "base" as const,
    themeVariables: {
      background: "#141414",
      mainBkg: "#1d1d1d",
      secondBkg: "#262626",
      primaryColor: "#1d1d1d",
      primaryTextColor: "#f2f2f2",
      primaryBorderColor: "#666666",
      secondaryColor: "#262626",
      secondaryTextColor: "#f2f2f2",
      secondaryBorderColor: "#666666",
      tertiaryColor: "#141414",
      tertiaryTextColor: "#f2f2f2",
      tertiaryBorderColor: "#666666",
      lineColor: "#d4d4d4",
      textColor: "#f2f2f2",
      nodeTextColor: "#f2f2f2",
      edgeLabelBackground: "#141414",
      clusterBkg: "#181818",
      clusterBorder: "#525252"
    }
  };
}

type MermaidState =
  | { status: "loading" }
  | { status: "ready"; svg: string }
  | { status: "error"; error: string };

export function MermaidDiagram({ chart }: { chart: string }) {
  const reactId = useId();
  const renderId = useMemo(() => `agentbox-mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, "")}`, [reactId]);
  const [state, setState] = useState<MermaidState>({ status: "loading" });
  const [fullscreenOpen, setFullscreenOpen] = useState(false);
  const [resolvedTheme, setResolvedTheme] = useState(() => getResolvedTheme());

  useEffect(() => {
    const updateTheme = () => setResolvedTheme(getResolvedTheme());
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    window.addEventListener("agentbox-theme-change", updateTheme);
    media.addEventListener("change", updateTheme);
    return () => {
      window.removeEventListener("agentbox-theme-change", updateTheme);
      media.removeEventListener("change", updateTheme);
    };
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function renderDiagram() {
      try {
        const mermaid = (await import("mermaid")).default;
        mermaid.initialize({ startOnLoad: false, securityLevel: "strict", ...mermaidThemeConfig(resolvedTheme) });
        const { svg } = await mermaid.render(renderId, chart);
        if (!cancelled) setState({ status: "ready", svg });
      } catch (err) {
        if (!cancelled) setState({ status: "error", error: err instanceof Error ? err.message : String(err) });
      }
    }

    void renderDiagram();
    return () => {
      cancelled = true;
    };
  }, [chart, renderId, resolvedTheme]);

  return (
    <>
      <Card size="sm" className="my-4">
        <CardHeader className="border-b">
          <div className="flex min-w-0 items-center gap-2">
            <WorkflowIcon />
            <div>
              <CardTitle>Mermaid diagram</CardTitle>
              <CardDescription>Rendered from the message source.</CardDescription>
            </div>
          </div>
          <CardAction className="flex items-center gap-2">
            {state.status === "ready" ? (
              <Button
                aria-label="Open Mermaid diagram fullscreen"
                title="Open Mermaid diagram fullscreen"
                type="button"
                variant="outline"
                size="icon-sm"
                onClick={() => setFullscreenOpen(true)}
              >
                <Maximize2Icon />
              </Button>
            ) : null}
            <CopyButton value={chart} label="Copy diagram code" />
          </CardAction>
        </CardHeader>
        <CardContent>
          {state.status === "loading" ? <Skeleton className="h-64 w-full" /> : null}
          {state.status === "ready" ? (
            <div
              className="min-h-48 overflow-x-auto border bg-card p-4 [&_svg]:mx-auto [&_svg]:h-auto [&_svg]:max-w-full"
              dangerouslySetInnerHTML={{ __html: state.svg }}
            />
          ) : null}
          {state.status === "error" ? (
            <div className="flex flex-col gap-3">
              <Alert variant="destructive">
                <ShieldAlertIcon />
                <AlertTitle>Could not render Mermaid</AlertTitle>
                <AlertDescription>{state.error}</AlertDescription>
              </Alert>
              <pre className="max-h-96 overflow-auto whitespace-pre-wrap border bg-muted/40 p-4 font-mono text-xs/relaxed">{chart}</pre>
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Dialog open={fullscreenOpen} onOpenChange={setFullscreenOpen}>
        <DialogContent className="max-h-[92vh] w-[min(96vw,75rem)] max-w-none grid-rows-[auto_minmax(0,1fr)] sm:max-w-none">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <WorkflowIcon />
              Mermaid diagram
            </DialogTitle>
            <DialogDescription>Fullscreen view. Press Escape or use the close button to return.</DialogDescription>
          </DialogHeader>
          {state.status === "ready" ? (
            <div
              className="min-h-0 overflow-auto border bg-card p-6 [&_svg]:mx-auto [&_svg]:h-auto [&_svg]:max-h-full [&_svg]:max-w-full"
              dangerouslySetInnerHTML={{ __html: state.svg }}
            />
          ) : (
            <div className="flex min-h-64 items-center justify-center"><Badge variant="secondary">Diagram unavailable</Badge></div>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
