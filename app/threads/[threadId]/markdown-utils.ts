export type MessageContentType = "text/plain" | "text/markdown";

const fencedCodeRe = /^```[\s\S]*```/m;
const mermaidFenceRe = /^```mermaid\b/im;
const headingRe = /^\s{0,3}#{1,6}\s+\S/m;
const tableRe = /^\s*\|.+\|\s*\n\s*\|?\s*:?-{3,}:?\s*\|/m;
const taskListRe = /^\s*[-*+]\s+\[[ xX]\]\s+/m;
const bulletListRe = /^\s*[-*+]\s+\S/gm;
const numberListRe = /^\s*\d+\.\s+\S/gm;
const quoteRe = /^\s{0,3}>\s+\S/m;
const linkRe = /\[[^\]]+\]\([^)]+\)/;
const mermaidRe = /^\s*(graph|flowchart|sequenceDiagram|classDiagram|stateDiagram|stateDiagram-v2|erDiagram|gantt|pie|journey|mindmap|timeline)\b/m;

export function normalizeContentType(value: string | null | undefined): MessageContentType | null {
  if (value === "text/plain" || value === "text/markdown") return value;
  return null;
}

export function inferBodyContentType(body: string): MessageContentType {
  const text = body.trim();
  if (!text) return "text/plain";
  let score = 0;
  if (fencedCodeRe.test(text)) score += 4;
  if (mermaidFenceRe.test(text)) score += 6;
  if (headingRe.test(text)) score += 2;
  if (tableRe.test(text)) score += 4;
  if (taskListRe.test(text)) score += 3;
  if ((text.match(bulletListRe) ?? []).length >= 2) score += 2;
  if ((text.match(numberListRe) ?? []).length >= 2) score += 2;
  if (quoteRe.test(text)) score += 2;
  if (linkRe.test(text)) score += 2;
  if (mermaidRe.test(text)) score += 5;
  if (text.length < 80 && score < 4) return "text/plain";
  return score >= 3 ? "text/markdown" : "text/plain";
}

export function messageFormatLabel(contentType: MessageContentType, wasInferred: boolean) {
  const label = contentType === "text/markdown" ? "Markdown" : "Plain text";
  return wasInferred ? `${label} · auto` : label;
}

