"use client";

export const LEGACY_STORAGE_KEY = "agentbox_admin_key";
export const VIEWER_PROFILES_STORAGE_KEY = "agentbox_viewer_profiles_v1";
export const ACTIVE_VIEWER_PROFILE_ID_STORAGE_KEY = "agentbox_active_viewer_profile_id";

export type ViewerProfile = {
  id: string;
  name: string;
  adminKey: string;
};

function isViewerProfile(value: unknown): value is ViewerProfile {
  return Boolean(
    value
    && typeof value === "object"
    && typeof (value as ViewerProfile).id === "string"
    && typeof (value as ViewerProfile).name === "string"
    && typeof (value as ViewerProfile).adminKey === "string"
  );
}

function normalizeProfiles(profiles: ViewerProfile[]): ViewerProfile[] {
  const seen = new Set<string>();
  const normalized: ViewerProfile[] = [];

  for (const profile of profiles) {
    const name = profile.name.trim();
    const adminKey = profile.adminKey.trim();
    if (!name || !adminKey || seen.has(profile.id)) continue;
    normalized.push({ ...profile, name, adminKey });
    seen.add(profile.id);
  }

  return normalized;
}

export function loadViewerProfiles(): ViewerProfile[] {
  if (typeof window === "undefined") return [];

  const raw = window.localStorage.getItem(VIEWER_PROFILES_STORAGE_KEY);
  const legacyKey = window.localStorage.getItem(LEGACY_STORAGE_KEY)?.trim();

  let parsedProfiles: ViewerProfile[] = [];
  if (raw) {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (Array.isArray(parsed)) {
        parsedProfiles = parsed.filter(isViewerProfile);
      }
    } catch {
      parsedProfiles = [];
    }
  }

  const profiles = normalizeProfiles(parsedProfiles);
  if (profiles.length > 0) return profiles;

  if (!legacyKey) return [];

  const migrated = [{ id: "default", name: "Default", adminKey: legacyKey }];
  saveViewerProfiles(migrated);
  setActiveViewerProfileId("default");
  window.localStorage.removeItem(LEGACY_STORAGE_KEY);
  return migrated;
}

export function saveViewerProfiles(profiles: ViewerProfile[]): void {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(VIEWER_PROFILES_STORAGE_KEY, JSON.stringify(normalizeProfiles(profiles)));
}

export function getActiveViewerProfileId(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(ACTIVE_VIEWER_PROFILE_ID_STORAGE_KEY);
}

export function setActiveViewerProfileId(profileId: string | null): void {
  if (typeof window === "undefined") return;
  if (!profileId) {
    window.localStorage.removeItem(ACTIVE_VIEWER_PROFILE_ID_STORAGE_KEY);
    return;
  }
  window.localStorage.setItem(ACTIVE_VIEWER_PROFILE_ID_STORAGE_KEY, profileId);
}

export function getActiveViewerProfile(): ViewerProfile | null {
  const profiles = loadViewerProfiles();
  if (profiles.length === 0) return null;

  const activeId = getActiveViewerProfileId();
  const active = activeId ? profiles.find((profile) => profile.id === activeId) : null;
  if (active) return active;

  const fallback = profiles[0];
  setActiveViewerProfileId(fallback.id);
  return fallback;
}

export function upsertViewerProfile(input: { id?: string | null; name: string; adminKey: string }): ViewerProfile {
  const profiles = loadViewerProfiles();
  const profile: ViewerProfile = {
    id: input.id?.trim() || `viewer_${crypto.randomUUID()}`,
    name: input.name.trim(),
    adminKey: input.adminKey.trim()
  };

  const next = normalizeProfiles([
    ...profiles.filter((existing) => existing.id !== profile.id),
    profile
  ]).sort((left, right) => left.name.localeCompare(right.name));

  saveViewerProfiles(next);
  setActiveViewerProfileId(profile.id);
  return profile;
}

export function removeViewerProfile(profileId: string): ViewerProfile[] {
  const next = loadViewerProfiles().filter((profile) => profile.id !== profileId);
  saveViewerProfiles(next);

  const activeId = getActiveViewerProfileId();
  if (activeId === profileId) {
    setActiveViewerProfileId(next[0]?.id ?? null);
  }

  return next;
}
