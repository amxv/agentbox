"use client";

import type { ComponentPropsWithoutRef, ReactNode } from "react";
import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import css from "highlight.js/lib/languages/css";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import markdown from "highlight.js/lib/languages/markdown";
import python from "highlight.js/lib/languages/python";
import rust from "highlight.js/lib/languages/rust";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Badge } from "@/components/ui/badge";
import { CopyButton } from "../../components/copy-button";
import { MermaidDiagram } from "./mermaid-diagram";

let languagesRegistered = false;

function registerLanguages() {
  if (languagesRegistered) return;
  hljs.registerLanguage("bash", bash);
  hljs.registerLanguage("css", css);
  hljs.registerLanguage("go", go);
  hljs.registerLanguage("javascript", javascript);
  hljs.registerLanguage("json", json);
  hljs.registerLanguage("markdown", markdown);
  hljs.registerLanguage("python", python);
  hljs.registerLanguage("rust", rust);
  hljs.registerLanguage("typescript", typescript);
  hljs.registerLanguage("xml", xml);
  languagesRegistered = true;
}

const languageAliases: Record<string, string> = {
  sh: "bash",
  shell: "bash",
  shellscript: "bash",
  "shell-session": "bash",
  console: "bash",
  terminal: "bash",
  zsh: "bash",
  js: "javascript",
  jsx: "javascript",
  ts: "typescript",
  tsx: "typescript",
  py: "python",
  rs: "rust",
  html: "xml",
  svg: "xml",
  md: "markdown"
};

function textFromNode(node: ReactNode): string {
  if (node === null || node === undefined || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textFromNode).join("");
  return "";
}

function normalizeLanguage(className?: string) {
  const match = /language-([\w-]+)/.exec(className ?? "");
  const raw = match?.[1]?.toLowerCase();
  if (!raw) return null;
  return languageAliases[raw] ?? raw;
}

type CodeProps = ComponentPropsWithoutRef<"code"> & {
  node?: {
    position?: {
      start?: { line?: number };
      end?: { line?: number };
    };
  };
};

type TableProps = ComponentPropsWithoutRef<"table">;

function CodeBlock({ className, children, node, ...props }: CodeProps) {
  const code = textFromNode(children).replace(/\n$/, "");
  const language = normalizeLanguage(className);
  const isBlock = Boolean(language) || node?.position?.start?.line !== node?.position?.end?.line;

  if (!isBlock) {
    return (
      <code className="agentbox-inline-code" {...props}>
        {children}
      </code>
    );
  }

  if (language === "mermaid") return <MermaidDiagram chart={code} />;

  registerLanguages();
  const supported = language && hljs.getLanguage(language);
  const highlighted = supported ? hljs.highlight(code, { language, ignoreIllegals: true }).value : null;
  const label = language ?? "code";

  return (
    <div className="agentbox-code-card">
      <div className="agentbox-code-toolbar">
        <Badge variant="secondary">{label}</Badge>
        <CopyButton value={code} label="Copy code" />
      </div>
      <pre className="agentbox-code-block">
        {highlighted ? (
          <code
            className={`hljs${language ? ` language-${language}` : ""}`}
            dangerouslySetInnerHTML={{ __html: highlighted }}
          />
        ) : (
          <code>{code}</code>
        )}
      </pre>
    </div>
  );
}

function MarkdownTable({ className, ...props }: TableProps) {
  return (
    <div className="agentbox-table-wrap">
      <table className={className} {...props} />
    </div>
  );
}

export function MarkdownMessage({ body }: { body: string }) {
  return (
    <div className="agentbox-markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          code: CodeBlock,
          table: MarkdownTable
        }}
      >
        {body}
      </ReactMarkdown>
    </div>
  );
}
