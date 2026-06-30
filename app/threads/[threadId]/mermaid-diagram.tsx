"use client";

import { useEffect, useId, useMemo, useState } from "react";
import { CopyButton } from "./copy-button";

type MermaidState =
  | { status: "loading" }
  | { status: "ready"; svg: string }
  | { status: "error"; error: string };

export function MermaidDiagram({ chart }: { chart: string }) {
  const reactId = useId();
  const renderId = useMemo(() => `agentbox-mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, "")}`, [reactId]);
  const [state, setState] = useState<MermaidState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;

    async function renderDiagram() {
      try {
        const mermaid = (await import("mermaid")).default;
        mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: "neutral" });
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
  }, [chart, renderId]);

  return (
    <div className="mermaid-card">
      <div className="message-toolbar">
        <span className="format-badge">Mermaid diagram</span>
        <CopyButton value={chart} label="Copy source" />
      </div>
      {state.status === "loading" && <p className="empty-state compact">Rendering diagram…</p>}
      {state.status === "ready" && <div className="mermaid-output" dangerouslySetInnerHTML={{ __html: state.svg }} />}
      {state.status === "error" && (
        <div className="mermaid-error">
          <strong>Could not render Mermaid.</strong>
          <span>{state.error}</span>
          <pre className="message-body">{chart}</pre>
        </div>
      )}
    </div>
  );
}
