import { timingSafeEqual } from "node:crypto";

function safeEqual(a: string, b: string): boolean {
  const left = Buffer.from(a);
  const right = Buffer.from(b);
  if (left.length !== right.length) return false;
  return timingSafeEqual(left, right);
}

export function requireAdminFromSearch(searchParams: URLSearchParams): string {
  const configured = process.env.AGENTBOX_ADMIN_KEY;

  if (!configured && process.env.NODE_ENV !== "production") {
    return searchParams.get("admin_key") ?? "local-dev";
  }

  if (!configured) {
    throw new Error("AGENTBOX_ADMIN_KEY is required for the web thread viewer.");
  }

  const provided = searchParams.get("admin_key");
  if (!provided || !safeEqual(provided, configured)) {
    throw new Error("Unauthorized");
  }

  return provided;
}

export function adminQuery(adminKey: string): string {
  return `admin_key=${encodeURIComponent(adminKey)}`;
}
