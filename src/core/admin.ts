import { timingSafeEqual } from "node:crypto";

function safeEqual(a: string, b: string): boolean {
  const left = Buffer.from(a);
  const right = Buffer.from(b);
  if (left.length !== right.length) return false;
  return timingSafeEqual(left, right);
}

type AdminKeyConfig = {
  name: string;
  key: string;
};

function configuredAdminKeys(): AdminKeyConfig[] {
  const raw = process.env.AGENTBOX_ADMIN_KEYS?.trim();
  if (raw) {
    if (raw.startsWith("[")) {
      const parsed = JSON.parse(raw) as Array<{ name?: unknown; key?: unknown }>;
      return parsed.flatMap((entry) => (
        typeof entry?.name === "string" && typeof entry?.key === "string"
          ? [{ name: entry.name, key: entry.key }]
          : []
      ));
    }

    return raw
      .split(",")
      .map((part) => part.trim())
      .filter(Boolean)
      .flatMap((part) => {
        const [name, key] = part.split(":");
        return name && key ? [{ name, key }] : [];
      });
  }

  const legacy = process.env.AGENTBOX_ADMIN_KEY ?? null;
  return legacy ? [{ name: "default", key: legacy }] : [];
}

function allowLocalDev(): boolean {
  return configuredAdminKeys().length === 0 && process.env.NODE_ENV !== "production";
}

export function requireAdminKey(provided: string | null): void {
  const configured = configuredAdminKeys();
  if (configured.length === 0 && allowLocalDev()) return;

  if (configured.length === 0) {
    throw new Error("AGENTBOX_ADMIN_KEY or AGENTBOX_ADMIN_KEYS is required for the web thread viewer.");
  }

  if (!provided || !configured.some((entry) => safeEqual(provided, entry.key))) {
    throw new Error("Unauthorized");
  }
}

export function requireAdminRequest(request: Request): void {
  const headerKey = request.headers.get("x-agentbox-admin-key");
  const bearer = request.headers.get("authorization")?.replace(/^Bearer\s+/i, "") ?? null;
  requireAdminKey(headerKey ?? bearer);
}
