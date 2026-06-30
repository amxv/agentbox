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
import { CopyButton } from "./copy-button";
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
  inline?: boolean;
};

type TableProps = ComponentPropsWithoutRef<"table">;

function CodeBlock({ inline, className, children, ...props }: CodeProps) {
  const code = textFromNode(children).replace(/\n$/, "");
  const language = normalizeLanguage(className);

  if (inline) {
    return <code className="markdown-inline-code" {...props}>{children}</code>;
  }

  if (language === "mermaid") {
    return <MermaidDiagram chart={code} />;
  }

  registerLanguages();
  const supported = language && hljs.getLanguage(language);
  const highlighted = supported ? hljs.highlight(code, { language, ignoreIllegals: true }).value : null;

  return (
    <div className="code-card">
      <div className="message-toolbar">
        <span className="format-badge">{supported ? language : "code"}</span>
        <CopyButton value={code} label="Copy code" />
      </div>
      <pre className="markdown-code-block">
        {highlighted ? (
          <code className={`hljs language-${language}`} dangerouslySetInnerHTML={{ __html: highlighted }} />
        ) : (
          <code>{code}</code>
        )}
      </pre>
    </div>
  );
}

function Table(props: TableProps) {
  return (
    <div className="markdown-table-scroll">
      <table {...props} />
    </div>
  );
}

export function MarkdownMessage({ body }: { body: string }) {
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          code: CodeBlock,
          table: Table
        }}
      >
        {body}
      </ReactMarkdown>
    </div>
  );
}
