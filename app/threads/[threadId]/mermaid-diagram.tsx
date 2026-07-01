"use client";

import { useEffect, useId, useMemo, useState } from "react";
import { CopyButton } from "./copy-button";

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
  const dialogTitleId = useMemo(() => `${renderId}-dialog-title`, [renderId]);
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

  useEffect(() => {
    if (!fullscreenOpen) return;

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setFullscreenOpen(false);
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [fullscreenOpen]);

  return (
    <>
      <div className="mermaid-card">
        <div className="message-toolbar">
          <span className="format-badge">Mermaid diagram</span>
          <div className="message-actions">
            {state.status === "ready" && (
              <button
                aria-label="Open Mermaid diagram fullscreen"
                className="mini-button icon-button"
                title="Open Mermaid diagram fullscreen"
                type="button"
                onClick={() => setFullscreenOpen(true)}
              >
                <ExpandIcon />
              </button>
            )}
            <CopyButton value={chart} label="Copy diagram code" />
          </div>
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
      {fullscreenOpen && state.status === "ready" && (
        <div className="modal-backdrop mermaid-backdrop" role="presentation" onClick={() => setFullscreenOpen(false)}>
          <div
            aria-labelledby={dialogTitleId}
            aria-modal="true"
            className="modal-card mermaid-modal"
            role="dialog"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="message-toolbar">
              <div>
                <p className="section-label">Mermaid diagram</p>
                <h2 className="card-title mermaid-modal__title" id={dialogTitleId}>Fullscreen view</h2>
              </div>
              <button
                aria-label="Close Mermaid fullscreen"
                className="mini-button icon-button"
                title="Close Mermaid fullscreen"
                type="button"
                onClick={() => setFullscreenOpen(false)}
              >
                <CloseIcon />
              </button>
            </div>
            <div className="mermaid-output mermaid-output--fullscreen" dangerouslySetInnerHTML={{ __html: state.svg }} />
          </div>
        </div>
      )}
    </>
  );
}

function ExpandIcon() {
  return (
    <svg aria-hidden="true" fill="none" height="16" viewBox="0 0 24 24" width="16">
      <path d="M14 4h6v6" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />
      <path d="m20 4-7 7" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />
      <path d="M10 20H4v-6" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />
      <path d="m4 20 7-7" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />
    </svg>
  );
}

function CloseIcon() {
  return (
    <svg aria-hidden="true" fill="none" height="16" viewBox="0 0 24 24" width="16">
      <path d="M18 6 6 18" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />
      <path d="m6 6 12 12" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" />
    </svg>
  );
}
