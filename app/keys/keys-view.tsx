"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { CopyButton } from "../components/copy-button";
import { AuthContext, fetchSession, signOutSession } from "../components/session";
import { ThemeSwitcher } from "../components/theme-switcher";

type Credential = {
  id: string;
  user_id: string;
  name: string;
  purpose: string;
  key_masked: string;
  token_prefix: string;
  scopes: string[];
  created_at: string;
  updated_at: string;
  last_used_at?: string | null;
  revoked_at?: string | null;
};

type CreatedCredential = Credential & {
  key: string;
};

type PageInfo = {
  limit: number;
  offset: number;
  has_more: boolean;
  next_cursor?: string | null;
};

type RaycastSetupPreference = {
  name: string;
  title: string;
  value: string;
  secret?: boolean;
};

type RaycastSetup = {
  credential_id: string;
  label: string;
  base_url: string;
  api_key?: string;
  repository_url: string;
  extension_path: string;
  install_commands: string[];
  preferences: RaycastSetupPreference[];
  final_check: string;
};

type SecretReveal = {
  credential: CreatedCredential;
  raycastSetup?: RaycastSetup | null;
};

function formatDate(value?: string | null) {
  if (!value) return "Never";
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}

function getMCPURL(secret: string) {
  if (typeof window === "undefined") return `/api/mcp?key=${encodeURIComponent(secret)}`;
  return `${window.location.origin}/api/mcp?key=${encodeURIComponent(secret)}`;
}

function setupPreferenceValue(preference: RaycastSetupPreference, setup: RaycastSetup) {
  if (preference.name === "apiKey" && !preference.value) return "Secret is not stored. Rotate this credential to reveal a replacement once.";
  return preference.value || (preference.name === "baseUrl" ? setup.base_url : "Not stored");
}

export function KeysView() {
  const router = useRouter();
  const [auth, setAuth] = useState<AuthContext | null>(null);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [page, setPage] = useState<PageInfo | null>(null);
  const [newKeyName, setNewKeyName] = useState("");
  const [raycastLabel, setRaycastLabel] = useState("");
  const [secretReveal, setSecretReveal] = useState<SecretReveal | null>(null);
  const [setupPreview, setSetupPreview] = useState<RaycastSetup | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [creating, setCreating] = useState<"custom" | "raycast" | null>(null);
  const [actingCredentialID, setActingCredentialID] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const loadCredentials = useCallback(async function loadCredentials(cursor?: string) {
    const firstPage = !cursor;
    if (firstPage) setLoading(true);
    else setLoadingMore(true);
    setError(null);
    try {
      const session = await fetchSession();
      if (!session) {
        router.replace("/login?next=/keys");
        return;
      }
      setAuth(session);
      const query = new URLSearchParams({ limit: "25" });
      if (cursor) query.set("cursor", cursor);
      const response = await fetch(`/api/keys?${query.toString()}`, { cache: "no-store" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      const next = (data.credentials ?? data.keys ?? []) as Credential[];
      setCredentials((current) => firstPage ? next : [...current, ...next.filter((item) => !current.some((existing) => existing.id === item.id))]);
      setPage(data.page ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (firstPage) setLoading(false);
      else setLoadingMore(false);
    }
  }, [router]);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void loadCredentials();
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [loadCredentials]);

  const activeCount = useMemo(() => credentials.filter((item) => !item.revoked_at).length, [credentials]);
  const revokedCount = credentials.length - activeCount;

  async function signOut() {
    try {
      await signOutSession();
    } finally {
      router.replace("/login");
    }
  }

  async function createCustomKey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = newKeyName.trim();
    if (!name) return;
    setCreating("custom");
    setError(null);
    setNotice(null);
    setSetupPreview(null);
    try {
      const response = await fetch("/api/keys", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ name, purpose: "custom" })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setSecretReveal({ credential: data.key });
      setNewKeyName("");
      setNotice(`Created credential “${data.key.name}”. Store the secret now; it will not appear in the inventory.`);
      await loadCredentials();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(null);
    }
  }

  async function createRaycastInstallation(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const label = raycastLabel.trim();
    if (!label) return;
    setCreating("raycast");
    setError(null);
    setNotice(null);
    setSetupPreview(null);
    try {
      const response = await fetch("/api/raycast-installations", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ label })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setSecretReveal({ credential: data.credential, raycastSetup: data.raycast_setup });
      setRaycastLabel("");
      setNotice(`Created independent Raycast installation “${data.credential.name}”. The API key is shown only once.`);
      await loadCredentials();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(null);
    }
  }

  async function rotateCredential(credential: Credential) {
    const confirmed = window.confirm(`Rotate “${credential.name}” (${credential.id})? Its current secret will stop working immediately.`);
    if (!confirmed) return;
    setActingCredentialID(credential.id);
    setError(null);
    setNotice(null);
    setSetupPreview(null);
    try {
      const response = await fetch(`/api/keys/${encodeURIComponent(credential.id)}`, { method: "PATCH" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setSecretReveal({ credential: data.credential, raycastSetup: data.raycast_setup });
      setNotice(`Rotated “${credential.name}”. Store the replacement secret now.`);
      await loadCredentials();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActingCredentialID(null);
    }
  }

  async function revokeCredential(credential: Credential) {
    const confirmed = window.confirm(`Revoke “${credential.name}” (${credential.id})? This credential will stop working immediately, but its audit row will remain visible.`);
    if (!confirmed) return;
    setActingCredentialID(credential.id);
    setError(null);
    setNotice(null);
    try {
      const response = await fetch(`/api/keys/${encodeURIComponent(credential.id)}`, { method: "DELETE" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setNotice(`Revoked credential ${data.revoked ?? credential.id}.`);
      if (secretReveal?.credential.id === credential.id) setSecretReveal(null);
      await loadCredentials();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActingCredentialID(null);
    }
  }

  async function openRaycastSetup(credential: Credential) {
    setActingCredentialID(credential.id);
    setError(null);
    setNotice(null);
    setSecretReveal(null);
    try {
      const response = await fetch(`/api/keys/${encodeURIComponent(credential.id)}/setup`, { cache: "no-store" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setSetupPreview(data.raycast_setup);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActingCredentialID(null);
    }
  }

  return (
    <div className="dashboard-page">
      <header className="site-header">
        <div className="shell site-header__inner">
          <Link className="brand" href="/">
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">Credential management</span>
          </Link>
          <nav className="site-nav" aria-label="Credential management navigation">
            <Link className="site-nav__link" href="/threads">Inbox</Link>
            <Link className="site-nav__link" href="/onboarding">Connect agents</Link>
            <Link className="site-nav__link" href="/">Home</Link>
            {auth && <span className="session-chip">{auth.actor_name}</span>}
            {auth && <button className="site-nav__link" type="button" onClick={() => void signOut()}>Sign out</button>}
            <ThemeSwitcher />
          </nav>
        </div>
      </header>

      <main className="dashboard-main shell">
        <section className="dashboard-header">
          <div className="dashboard-header__row">
            <div>
              <p className="section-label">User credentials</p>
              <h1 className="dashboard-title">Credentials and installations</h1>
              <p className="dashboard-copy">Create independent credentials for each actor surface, inspect safe usage metadata, and retain revoked rows as audit history.</p>
            </div>
            {auth && (
              <div className="credential-stats" aria-label="Credential totals">
                <div className="card">
                  <p className="stat-label">Active</p>
                  <h2 className="card-title">{activeCount}</h2>
                </div>
                <div className="card">
                  <p className="stat-label">Revoked history</p>
                  <h2 className="card-title">{revokedCount}</h2>
                </div>
              </div>
            )}
          </div>
        </section>

        <div className="key-management-grid">
          <div className="credential-create-stack">
            <section className="sign-in-card key-create-card" aria-labelledby="create-key-title">
              <div>
                <p className="section-label">Custom actor</p>
                <h2 id="create-key-title" className="card-title">New API key</h2>
                <p className="copy">Use a clear label for a local agent, worker, or another supported client. Reusing an active custom label rotates that credential.</p>
              </div>
              <form className="key-create-form" onSubmit={createCustomKey}>
                <input className="form-input" value={newKeyName} onChange={(event) => setNewKeyName(event.target.value)} placeholder="Codex on MacBook" type="text" />
                <button className="button button--solid" type="submit" disabled={creating !== null || !newKeyName.trim()}>
                  {creating === "custom" ? "Creating..." : "Create API key"}
                </button>
              </form>
            </section>

            <section className="sign-in-card key-create-card" aria-labelledby="create-raycast-title">
              <div>
                <p className="section-label">Raycast</p>
                <h2 id="create-raycast-title" className="card-title">New installation</h2>
                <p className="copy">Every Raycast installation receives its own label, secret, least-privilege scopes, and developer-mode setup bundle.</p>
              </div>
              <form className="key-create-form" onSubmit={createRaycastInstallation}>
                <input className="form-input" value={raycastLabel} onChange={(event) => setRaycastLabel(event.target.value)} placeholder="MacBook Air" type="text" />
                <button className="button button--solid" type="submit" disabled={creating !== null || !raycastLabel.trim()}>
                  {creating === "raycast" ? "Creating..." : "Create Raycast installation"}
                </button>
              </form>
            </section>
          </div>

          <section className="key-list-card" aria-labelledby="credential-inventory-title">
            <div className="key-list-card__header">
              <div>
                <p className="section-label">Inventory</p>
                <h2 id="credential-inventory-title" className="card-title">Active and revoked credentials</h2>
              </div>
              <button className="button button--ghost" type="button" onClick={() => void loadCredentials()} disabled={loading}>Refresh</button>
            </div>

            {notice && <div className="notice-card">{notice}</div>}
            {error && (
              <div className="error-card">
                <strong>Could not manage credentials.</strong>
                <span>{error}</span>
              </div>
            )}

            {secretReveal && (
              <div className="secret-card">
                <div>
                  <p className="section-label">Secret shown once</p>
                  <h3>{secretReveal.credential.name}</h3>
                  <p className="copy">Credential ID: <code>{secretReveal.credential.id}</code>. Copy the secret now; the inventory stores only its hash and safe prefix.</p>
                </div>
                <div className="secret-row">
                  <code>{secretReveal.credential.key}</code>
                  <CopyButton value={secretReveal.credential.key} label="Copy API key" />
                </div>
                {secretReveal.raycastSetup ? (
                  <RaycastSetupPanel setup={secretReveal.raycastSetup} />
                ) : (
                  <div className="secret-row">
                    <code>{getMCPURL(secretReveal.credential.key)}</code>
                    <CopyButton value={getMCPURL(secretReveal.credential.key)} label="Copy MCP URL" />
                  </div>
                )}
              </div>
            )}

            {setupPreview && (
              <div className="secret-card">
                <div className="key-list-card__header">
                  <div>
                    <p className="section-label">Saved setup</p>
                    <h3>{setupPreview.label}</h3>
                    <p className="copy">These non-secret instructions remain available after refresh. Agentbox never redisplays the stored API key.</p>
                  </div>
                  <button className="mini-button" type="button" onClick={() => setSetupPreview(null)}>Close</button>
                </div>
                <RaycastSetupPanel setup={setupPreview} />
              </div>
            )}

            {loading && <p className="empty-state" aria-busy="true">Loading credential inventory…</p>}
            {!loading && credentials.length === 0 && <p className="empty-state">No credentials found.</p>}
            {!loading && credentials.length > 0 && (
              <div className="credential-inventory" aria-label="Credential inventory">
                {credentials.map((credential) => {
                  const revoked = Boolean(credential.revoked_at);
                  const acting = actingCredentialID === credential.id;
                  return (
                    <article className="credential-card" key={credential.id}>
                      <div className="credential-card__header">
                        <div>
                          <div className="credential-card__title-row">
                            <h3>{credential.name}</h3>
                            <span className={`credential-state ${revoked ? "credential-state--revoked" : "credential-state--active"}`}>{revoked ? "Revoked" : "Active"}</span>
                          </div>
                          <code className="credential-id">{credential.id}</code>
                        </div>
                        <div className="key-actions">
                          {credential.purpose === "raycast" && <button className="mini-button" type="button" onClick={() => void openRaycastSetup(credential)} disabled={acting}>Setup</button>}
                          {!revoked && <button className="mini-button" type="button" onClick={() => void rotateCredential(credential)} disabled={acting}>{acting ? "Working..." : "Rotate"}</button>}
                          {!revoked && <button className="mini-button mini-button--danger" type="button" onClick={() => void revokeCredential(credential)} disabled={acting}>Revoke</button>}
                        </div>
                      </div>
                      <dl className="credential-metadata">
                        <div><dt>Purpose</dt><dd>{credential.purpose || "custom"}</dd></div>
                        <div><dt>Masked token</dt><dd className="mono">{credential.key_masked || credential.token_prefix}</dd></div>
                        <div><dt>Scopes</dt><dd>{credential.scopes.length > 0 ? credential.scopes.join(", ") : "Legacy/default"}</dd></div>
                        <div><dt>Created</dt><dd>{formatDate(credential.created_at)}</dd></div>
                        <div><dt>Last used</dt><dd>{formatDate(credential.last_used_at)}</dd></div>
                        <div><dt>Revoked</dt><dd>{credential.revoked_at ? formatDate(credential.revoked_at) : "No"}</dd></div>
                      </dl>
                    </article>
                  );
                })}
              </div>
            )}

            {page?.has_more && page.next_cursor && (
              <button className="button button--ghost" type="button" onClick={() => void loadCredentials(page.next_cursor ?? undefined)} disabled={loadingMore}>
                {loadingMore ? "Loading…" : "Load more credentials"}
              </button>
            )}
          </section>
        </div>
      </main>
    </div>
  );
}

function RaycastSetupPanel({ setup }: { setup: RaycastSetup }) {
  return (
    <div className="raycast-setup-panel">
      <div className="credential-metadata">
        <div><dt>Repository</dt><dd><code>{setup.repository_url}</code></dd></div>
        <div><dt>Extension path</dt><dd><code>{setup.extension_path}</code></dd></div>
        <div><dt>Agentbox URL</dt><dd><code>{setup.base_url}</code></dd></div>
      </div>
      <div>
        <p className="section-label">Install commands</p>
        <div className="setup-command-list">
          {setup.install_commands.map((command) => (
            <div className="secret-row" key={command}>
              <code>{command}</code>
              <CopyButton value={command} label="Copy command" />
            </div>
          ))}
        </div>
      </div>
      <div>
        <p className="section-label">Raycast preferences</p>
        <div className="setup-command-list">
          {setup.preferences.map((preference) => {
            const value = setupPreferenceValue(preference, setup);
            const copyable = Boolean(preference.value);
            return (
              <div className="secret-row" key={preference.name}>
                <div>
                  <strong>{preference.title}</strong>
                  <code>{value}</code>
                </div>
                {copyable && <CopyButton value={preference.value} label={`Copy ${preference.title}`} />}
              </div>
            );
          })}
        </div>
      </div>
      <p className="copy"><strong>Final check:</strong> {setup.final_check}</p>
    </div>
  );
}
