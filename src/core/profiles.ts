import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { mkdir, readFile, writeFile } from "node:fs/promises";

export type AgentboxProfile = {
  name: string;
  baseUrl: string;
  apiKey: string;
};

export type ProfileStore = {
  activeProfileName: string | null;
  profiles: AgentboxProfile[];
};

export type ResolvedProfile = AgentboxProfile & {
  source: "config" | "env" | "legacy-env";
};

type RawProfileRecord = {
  base_url?: unknown;
  baseUrl?: unknown;
  api_key?: unknown;
  apiKey?: unknown;
};

type ProfileStoreFile = {
  active_profile?: unknown;
  current_profile?: unknown;
  profiles?: unknown;
};

function normalizeProfile(name: string, value: RawProfileRecord): AgentboxProfile | null {
  const trimmedName = name.trim();
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

  if (!trimmedName || !baseUrl || !apiKey) return null;

  return {
    name: trimmedName,
    baseUrl: baseUrl.trim().replace(/\/+$/, ""),
    apiKey: apiKey.trim()
  };
}

function parseProfilesRecord(value: unknown): AgentboxProfile[] {
  if (Array.isArray(value)) {
    return value.flatMap((entry) => {
      if (!entry || typeof entry !== "object") return [];
      const candidate = entry as { name?: unknown } & RawProfileRecord;
      if (typeof candidate.name !== "string") return [];
      const profile = normalizeProfile(candidate.name, candidate);
      return profile ? [profile] : [];
    });
  }

  if (!value || typeof value !== "object") return [];

  return Object.entries(value as Record<string, RawProfileRecord>).flatMap(([name, record]) => {
    if (!record || typeof record !== "object") return [];
    const profile = normalizeProfile(name, record);
    return profile ? [profile] : [];
  });
}

function dedupeProfiles(profiles: AgentboxProfile[]): AgentboxProfile[] {
  const byName = new Map<string, AgentboxProfile>();
  for (const profile of profiles) {
    byName.set(profile.name, profile);
  }
  return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name));
}

export function parseProfilesConfig(raw: string | undefined): AgentboxProfile[] {
  if (!raw) return [];

  const trimmed = raw.trim();
  if (!trimmed) return [];

  const parsed = JSON.parse(trimmed) as unknown;
  return dedupeProfiles(parseProfilesRecord(parsed));
}

export function defaultConfigDir(): string {
  if (process.env.AGENTBOX_CONFIG_DIR?.trim()) return process.env.AGENTBOX_CONFIG_DIR.trim();
  if (process.platform === "win32" && process.env.APPDATA?.trim()) {
    return join(process.env.APPDATA.trim(), "agentbox");
  }
  if (process.platform === "darwin") {
    return join(homedir(), "Library", "Application Support", "agentbox");
  }
  if (process.env.XDG_CONFIG_HOME?.trim()) {
    return join(process.env.XDG_CONFIG_HOME.trim(), "agentbox");
  }
  return join(homedir(), ".config", "agentbox");
}

export function defaultConfigPath(): string {
  return join(defaultConfigDir(), "profiles.json");
}

export async function readProfileStore(): Promise<ProfileStore> {
  try {
    const raw = await readFile(defaultConfigPath(), "utf8");
    const parsed = JSON.parse(raw) as ProfileStoreFile;
    const profiles = dedupeProfiles(parseProfilesRecord(parsed.profiles));
    const candidate = typeof parsed.active_profile === "string"
      ? parsed.active_profile
      : typeof parsed.current_profile === "string"
        ? parsed.current_profile
        : null;
    const activeProfileName = candidate && profiles.some((profile) => profile.name === candidate)
      ? candidate
      : null;
    return { activeProfileName, profiles };
  } catch (error) {
    const code = (error as NodeJS.ErrnoException | undefined)?.code;
    if (code === "ENOENT") return { activeProfileName: null, profiles: [] };
    throw error;
  }
}

async function writeProfileStore(store: ProfileStore): Promise<void> {
  const filePath = defaultConfigPath();
  await mkdir(dirname(filePath), { recursive: true });
  const normalizedProfiles = dedupeProfiles(store.profiles);
  const activeProfileName = store.activeProfileName && normalizedProfiles.some((profile) => profile.name === store.activeProfileName)
    ? store.activeProfileName
    : normalizedProfiles[0]?.name ?? null;
  const payload = {
    active_profile: activeProfileName,
    profiles: Object.fromEntries(normalizedProfiles.map((profile) => [
      profile.name,
      {
        base_url: profile.baseUrl,
        api_key: profile.apiKey
      }
    ]))
  };
  await writeFile(filePath, `${JSON.stringify(payload, null, 2)}\n`, { mode: 0o600 });
}

export async function saveProfile(profile: AgentboxProfile, options?: { activate?: boolean }): Promise<ProfileStore> {
  const store = await readProfileStore();
  const profiles = [
    ...store.profiles.filter((entry) => entry.name !== profile.name),
    normalizeProfile(profile.name, { base_url: profile.baseUrl, api_key: profile.apiKey }) ?? profile
  ];
  const nextStore: ProfileStore = {
    activeProfileName: options?.activate ? profile.name : store.activeProfileName,
    profiles
  };
  await writeProfileStore(nextStore);
  return readProfileStore();
}

export async function removeProfile(name: string): Promise<ProfileStore> {
  const store = await readProfileStore();
  const profiles = store.profiles.filter((profile) => profile.name !== name);
  if (profiles.length === store.profiles.length) {
    throw new Error(`Unknown Agentbox profile "${name}".`);
  }
  const nextStore: ProfileStore = {
    activeProfileName: store.activeProfileName === name ? (profiles[0]?.name ?? null) : store.activeProfileName,
    profiles
  };
  await writeProfileStore(nextStore);
  return readProfileStore();
}

export async function setActiveProfile(name: string): Promise<ProfileStore> {
  const store = await readProfileStore();
  if (!store.profiles.some((profile) => profile.name === name)) {
    throw new Error(`Unknown Agentbox profile "${name}".`);
  }
  await writeProfileStore({ ...store, activeProfileName: name });
  return readProfileStore();
}

export function maskSecret(secret: string, options?: { visiblePrefix?: number; visibleSuffix?: number }): string {
  const visiblePrefix = options?.visiblePrefix ?? 3;
  const visibleSuffix = options?.visibleSuffix ?? 2;
  if (secret.length <= visiblePrefix + visibleSuffix) return "*".repeat(Math.max(secret.length, 4));
  return `${secret.slice(0, visiblePrefix)}${"*".repeat(secret.length - visiblePrefix - visibleSuffix)}${secret.slice(-visibleSuffix)}`;
}

export function sanitizeUrl(url: URL): string {
  const clone = new URL(url.toString());
  const key = clone.searchParams.get("key");
  if (key) clone.searchParams.set("key", maskSecret(key));
  return clone.toString();
}

export async function resolveProfileSelection(options?: { profileName?: string | null }): Promise<ResolvedProfile | null> {
  const envProfiles = parseProfilesConfig(process.env.AGENTBOX_PROFILES);
  const selectedName = options?.profileName ?? process.env.AGENTBOX_PROFILE ?? null;

  if (envProfiles.length > 0) {
    if (selectedName) {
      const match = envProfiles.find((profile) => profile.name === selectedName);
      if (!match) throw new Error(`Unknown Agentbox profile "${selectedName}".`);
      return { ...match, source: "env" };
    }
    return { ...envProfiles[0], source: "env" };
  }

  const store = await readProfileStore();
  if (store.profiles.length > 0) {
    const activeName = selectedName ?? store.activeProfileName ?? store.profiles[0]?.name ?? null;
    const match = activeName ? store.profiles.find((profile) => profile.name === activeName) : null;
    if (selectedName && !match) throw new Error(`Unknown Agentbox profile "${selectedName}".`);
    if (match) return { ...match, source: "config" };
  }

  const legacyBaseUrl = process.env.AGENTBOX_BASE_URL ?? process.env.AGENTBOX_URL;
  const legacyApiKey = process.env.AGENTBOX_API_KEY;
  if (!legacyBaseUrl || !legacyApiKey) return null;
  if (selectedName && selectedName !== "default") {
    throw new Error(`Unknown Agentbox profile "${selectedName}".`);
  }

  return {
    name: selectedName ?? "default",
    baseUrl: legacyBaseUrl.trim().replace(/\/+$/, ""),
    apiKey: legacyApiKey.trim(),
    source: "legacy-env"
  };
}
