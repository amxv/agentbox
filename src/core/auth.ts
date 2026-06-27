import { timingSafeEqual } from "node:crypto";
import type { Actor } from "./types";

type KeyConfig = {
  name: string;
  key: string;
  author: string;
};

function safeEqual(a: string, b: string): boolean {
  const left = Buffer.from(a);
  const right = Buffer.from(b);
  if (left.length !== right.length) return false;
  return timingSafeEqual(left, right);
}

function parseKeyConfig(raw: string | undefined): KeyConfig[] {
  if (!raw) return [];
  const trimmed = raw.trim();
  if (!trimmed) return [];

  if (trimmed.startsWith("[")) {
    const parsed = JSON.parse(trimmed) as KeyConfig[];
    return parsed.filter((entry) => entry.name && entry.key && entry.author);
  }

  return trimmed
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part) => {
      const [name, key, author] = part.split(":");
      return { name, key, author: author ?? name };
    })
    .filter((entry) => entry.name && entry.key && entry.author);
}

export function getBearerToken(request: Request): string | null {
  const header = request.headers.get("authorization");
  if (!header) return null;
  const match = header.match(/^Bearer\s+(.+)$/i);
  return match?.[1] ?? null;
}

export function authenticateRequest(request: Request): Actor | null {
  const keys = parseKeyConfig(process.env.AGENTBOX_API_KEYS);

  if (keys.length === 0 && process.env.NODE_ENV !== "production") {
    return { name: "local-dev", keyName: "local-dev" };
  }

  const token = getBearerToken(request);
  if (!token) return null;

  const match = keys.find((entry) => safeEqual(entry.key, token));
  if (!match) return null;

  return { name: match.author, keyName: match.name };
}

export function requireActor(request: Request): Actor {
  const actor = authenticateRequest(request);
  if (!actor) {
    throw new Response(JSON.stringify({ error: "Unauthorized" }), {
      status: 401,
      headers: { "content-type": "application/json" }
    });
  }
  return actor;
}

export function validateOrigin(request: Request): Response | null {
  const allowed = process.env.AGENTBOX_ALLOWED_ORIGINS?.split(",").map((v) => v.trim()).filter(Boolean) ?? [];
  if (allowed.length === 0) return null;

  const origin = request.headers.get("origin");
  if (!origin) return null;

  if (!allowed.includes(origin)) {
    return new Response(JSON.stringify({ error: "Forbidden origin" }), {
      status: 403,
      headers: { "content-type": "application/json" }
    });
  }

  return null;
}
