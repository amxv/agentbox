export type AgentboxProfile = {
  name: string;
  baseUrl: string;
  apiKey: string;
};

type RawProfileRecord = {
  base_url?: unknown;
  baseUrl?: unknown;
  api_key?: unknown;
  apiKey?: unknown;
};

function normalizeProfile(name: string, value: RawProfileRecord): AgentboxProfile | null {
  const baseUrl = typeof value.base_url === "string"
    ? value.base_url
    : typeof value.baseUrl === "string"
      ? value.baseUrl
      : null;
  const apiKey = typeof value.api_key === "string"
    ? value.api_key
    : typeof value.apiKey === "string"
      ? value.apiKey
      : null;

  if (!name || !baseUrl || !apiKey) return null;

  return {
    name,
    baseUrl: baseUrl.replace(/\/$/, ""),
    apiKey
  };
}

export function parseProfilesConfig(raw: string | undefined): AgentboxProfile[] {
  if (!raw) return [];

  const trimmed = raw.trim();
  if (!trimmed) return [];

  const parsed = JSON.parse(trimmed) as unknown;

  if (Array.isArray(parsed)) {
    return parsed.flatMap((entry) => {
      if (!entry || typeof entry !== "object") return [];
      const candidate = entry as { name?: unknown } & RawProfileRecord;
      if (typeof candidate.name !== "string") return [];
      const profile = normalizeProfile(candidate.name, candidate);
      return profile ? [profile] : [];
    });
  }

  if (!parsed || typeof parsed !== "object") return [];

  return Object.entries(parsed as Record<string, RawProfileRecord>).flatMap(([name, value]) => {
    if (!value || typeof value !== "object") return [];
    const profile = normalizeProfile(name, value);
    return profile ? [profile] : [];
  });
}

export function resolveProfileSelection(options?: { profileName?: string | null }): AgentboxProfile | null {
  const configured = parseProfilesConfig(process.env.AGENTBOX_PROFILES);
  const selectedName = options?.profileName ?? process.env.AGENTBOX_PROFILE ?? null;

  if (selectedName) {
    const match = configured.find((profile) => profile.name === selectedName);
    if (!match) {
      throw new Error(`Unknown Agentbox profile "${selectedName}".`);
    }
    return match;
  }

  if (configured.length > 0) {
    return configured[0];
  }

  const legacyBaseUrl = process.env.AGENTBOX_BASE_URL ?? process.env.AGENTBOX_URL;
  const legacyApiKey = process.env.AGENTBOX_API_KEY;
  if (!legacyBaseUrl || !legacyApiKey) return null;

  return {
    name: "default",
    baseUrl: legacyBaseUrl.replace(/\/$/, ""),
    apiKey: legacyApiKey
  };
}
