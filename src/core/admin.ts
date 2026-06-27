import { timingSafeEqual } from "node:crypto";

function safeEqual(a: string, b: string): boolean {
  const left = Buffer.from(a);
  const right = Buffer.from(b);
  if (left.length !== right.length) return false;
  return timingSafeEqual(left, right);
}

function configuredAdminKey(): string | null {
  return process.env.AGENTBOX_ADMIN_KEY ?? null;
}

function allowLocalDev(): boolean {
  return !configuredAdminKey() && process.env.NODE_ENV !== "production";
}

export function requireAdminKey(provided: string | null): void {
  const configured = configuredAdminKey();

  if (!configured && allowLocalDev()) return;

  if (!configured) {
    throw new Error("AGENTBOX_ADMIN_KEY is required for the web thread viewer.");
  }

  if (!provided || !safeEqual(provided, configured)) {
    throw new Error("Unauthorized");
  }
}

export function requireAdminRequest(request: Request): void {
  const headerKey = request.headers.get("x-agentbox-admin-key");
  const bearer = request.headers.get("authorization")?.replace(/^Bearer\s+/i, "") ?? null;
  requireAdminKey(headerKey ?? bearer);
}
