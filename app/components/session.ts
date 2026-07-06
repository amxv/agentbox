"use client";

export type AuthContext = {
  tenant_id: string;
  tenant_slug?: string;
  user_id?: string;
  subject_type: "user_session" | "api_key" | "admin";
  actor_name: string;
  key_id?: string;
  session_id?: string;
  scopes?: string[];
  role?: string;
};

export type SessionPayload = {
  auth: AuthContext;
};

export async function fetchSession(): Promise<AuthContext | null> {
  const response = await fetch("/api/auth/me", { cache: "no-store" });
  if (response.status === 401) return null;
  const data = await response.json() as SessionPayload & { error?: string };
  if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data.auth;
}

export async function signOutSession() {
  const response = await fetch("/api/auth/logout", { method: "POST" });
  if (!response.ok) {
    const data = await response.json().catch(() => null) as { error?: string } | null;
    throw new Error(data?.error ?? `HTTP ${response.status}`);
  }
}
