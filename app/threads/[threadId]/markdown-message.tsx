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
import { cn } from "@/lib/utils";
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
      <code
        className="rounded-none border bg-muted px-1 py-0.5 font-mono text-[0.85em] text-foreground"
        {...props}
      >
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
    <div className="my-4 min-w-0 overflow-hidden border bg-muted/30">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b px-3 py-2">
        <Badge variant="secondary">{label}</Badge>
        <CopyButton value={code} label="Copy code" />
      </div>
      <pre className="min-w-0 overflow-x-auto p-4 font-mono text-xs/relaxed text-foreground">
        {highlighted ? (
          <code
            className={cn("hljs", language ? `language-${language}` : undefined)}
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
    <div className="my-4 min-w-0 overflow-x-auto border">
      <table className={cn("w-full min-w-[32rem] border-collapse text-xs", className)} {...props} />
    </div>
  );
}

export function MarkdownMessage({ body }: { body: string }) {
  return (
    <div
      className={cn(
        "min-w-0 text-sm/relaxed text-foreground",
        "[&>*:first-child]:mt-0 [&>*:last-child]:mb-0",
        "[&_p]:my-4 [&_p]:text-pretty",
        "[&_h1]:mt-8 [&_h1]:mb-4 [&_h1]:font-heading [&_h1]:text-3xl [&_h1]:font-semibold [&_h1]:tracking-[-0.04em]",
        "[&_h2]:mt-8 [&_h2]:mb-3 [&_h2]:border-b [&_h2]:pb-2 [&_h2]:font-heading [&_h2]:text-2xl [&_h2]:font-semibold [&_h2]:tracking-[-0.035em]",
        "[&_h3]:mt-6 [&_h3]:mb-3 [&_h3]:font-heading [&_h3]:text-xl [&_h3]:font-semibold [&_h3]:tracking-[-0.025em]",
        "[&_h4]:mt-5 [&_h4]:mb-2 [&_h4]:font-heading [&_h4]:text-base [&_h4]:font-semibold",
        "[&_ul]:my-4 [&_ul]:list-disc [&_ul]:pl-6 [&_ol]:my-4 [&_ol]:list-decimal [&_ol]:pl-6",
        "[&_li]:my-1 [&_li]:pl-1 [&_li>ul]:my-1 [&_li>ol]:my-1",
        "[&_blockquote]:my-5 [&_blockquote]:border-l-2 [&_blockquote]:pl-4 [&_blockquote]:text-muted-foreground",
        "[&_a]:font-medium [&_a]:underline [&_a]:underline-offset-4 [&_a:hover]:text-muted-foreground",
        "[&_hr]:my-8 [&_hr]:border-0 [&_hr]:border-t",
        "[&_thead]:border-b [&_th]:bg-muted/50 [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:font-medium",
        "[&_td]:border-t [&_td]:px-3 [&_td]:py-2 [&_td]:align-top",
        "[&_img]:my-5 [&_img]:max-w-full [&_img]:border"
      )}
    >
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
