"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { CopyButton } from "../components/copy-button";
import { AuthContext, fetchSession, signOutSession } from "../components/session";
import { ThemeSwitcher } from "../components/theme-switcher";

type APIKey = {
  name: string;
  key_masked: string;
  created_at: string;
  updated_at: string;
};

type CreatedAPIKey = APIKey & {
  key: string;
};

function formatDate(value: string) {
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}

function getMCPURL(secret: string) {
  if (typeof window === "undefined") return `/api/mcp?key=${encodeURIComponent(secret)}`;
  return `${window.location.origin}/api/mcp?key=${encodeURIComponent(secret)}`;
}

export function KeysView() {
  const router = useRouter();
  const [auth, setAuth] = useState<AuthContext | null>(null);
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [newKeyName, setNewKeyName] = useState("");
  const [createdKey, setCreatedKey] = useState<CreatedAPIKey | null>(null);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [deletingName, setDeletingName] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const loadKeys = useCallback(async function loadKeys() {
    setLoading(true);
    setError(null);
    try {
      const session = await fetchSession();
      if (!session) {
        router.replace("/login?next=/keys");
        return;
      }
      setAuth(session);
      const response = await fetch("/api/keys", { cache: "no-store" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setKeys(data.keys ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [router]);

  useEffect(() => {
    void loadKeys();
  }, [loadKeys]);

  const latestUpdatedAt = useMemo(() => {
    if (keys.length === 0) return null;
    return keys.reduce((latest, item) => {
      const current = new Date(item.updated_at).getTime();
      return current > latest ? current : latest;
    }, 0);
  }, [keys]);

  async function signOut() {
    try {
      await signOutSession();
    } finally {
      router.replace("/login");
    }
  }

  async function createKey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = newKeyName.trim();
    if (!name) return;
    setCreating(true);
    setError(null);
    setNotice(null);
    try {
      const response = await fetch("/api/keys", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ name })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setCreatedKey(data.key);
      setNewKeyName("");
      setNotice(`Created API key "${data.key.name}". Store the secret now; it will not appear in the key list.`);
      await loadKeys();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  }

  async function deleteKey(name: string) {
    const confirmed = window.confirm(`Delete API key "${name}"? Anything using this key will lose access immediately.`);
    if (!confirmed) return;
    setDeletingName(name);
    setError(null);
    setNotice(null);
    try {
      const response = await fetch(`/api/keys/${encodeURIComponent(name)}`, { method: "DELETE" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setNotice(`Deleted API key "${data.revoked ?? name}".`);
      if (createdKey?.name === name) setCreatedKey(null);
      await loadKeys();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeletingName(null);
    }
  }

  return (
    <div className="dashboard-page">
      <header className="site-header">
        <div className="shell site-header__inner">
          <Link className="brand" href="/">
            <span className="brand__eyebrow">Agentbox</span>
            <span className="brand__title">Key management</span>
          </Link>
          <nav className="site-nav" aria-label="Key management navigation">
            <Link className="site-nav__link" href="/threads">Inbox</Link>
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
              <p className="section-label">Tenant admin tools</p>
              <h1 className="dashboard-title">API keys</h1>
              <p className="dashboard-copy">Create tenant-scoped agent credentials, review active keys, and revoke access from your signed-in tenant.</p>
            </div>
            {auth && (
              <div className="card">
                <p className="stat-label">Active keys</p>
                <h2 className="card-title">{keys.length}</h2>
                <p className="copy">{latestUpdatedAt ? `Last changed ${formatDate(new Date(latestUpdatedAt).toISOString())}` : "No keys yet."}</p>
              </div>
            )}
          </div>
        </section>

        <div className="key-management-grid">
          <section className="sign-in-card key-create-card" aria-labelledby="create-key-title">
            <div>
              <p className="section-label">Create</p>
              <h2 id="create-key-title" className="card-title">New API key</h2>
              <p className="copy">Use clear names like local, chatgpt, codex, claude, or worker-prod. Reusing a name rotates that key inside this tenant.</p>
            </div>
            <form className="key-create-form" onSubmit={createKey}>
              <input
                className="form-input"
                value={newKeyName}
                onChange={(event) => setNewKeyName(event.target.value)}
                placeholder="key-name"
                type="text"
              />
              <button className="button button--solid" type="submit" disabled={creating || !newKeyName.trim()}>
                {creating ? "Creating..." : "Create key"}
              </button>
            </form>
          </section>

          <section className="key-list-card" aria-labelledby="active-keys-title">
            <div className="key-list-card__header">
              <div>
                <p className="section-label">Current</p>
                <h2 id="active-keys-title" className="card-title">Active keys</h2>
              </div>
              <button className="button button--ghost" type="button" onClick={() => void loadKeys()} disabled={loading}>Refresh</button>
            </div>

            {notice && <div className="notice-card">{notice}</div>}
            {error && (
              <div className="error-card">
                <strong>Could not manage keys.</strong>
                <span>{error}</span>
              </div>
            )}

            {createdKey && (
              <div className="secret-card">
                <div>
                  <p className="section-label">Secret shown once</p>
                  <h3>{createdKey.name}</h3>
                  <p className="copy">Copy this now. The key list only shows the masked value.</p>
                </div>
                <div className="secret-row">
                  <code>{createdKey.key}</code>
                  <CopyButton value={createdKey.key} label="Copy API key" />
                </div>
                <div className="secret-row">
                  <code>{getMCPURL(createdKey.key)}</code>
                  <CopyButton value={getMCPURL(createdKey.key)} label="Copy MCP URL" />
                </div>
              </div>
            )}

            {loading && (
              <div className="skeleton-list" aria-label="Loading keys" aria-busy="true">
                <div className="skeleton-key-table" aria-hidden="true">
                  <div className="skeleton-key-row skeleton-key-row--head">
                    <span className="skeleton-line skeleton-line--short" />
                    <span className="skeleton-line skeleton-line--medium" />
                    <span className="skeleton-line skeleton-line--medium" />
                    <span className="skeleton-line skeleton-line--tiny" />
                  </div>
                  {Array.from({ length: 3 }).map((_, index) => (
                    <div className="skeleton-key-row" key={index}>
                      <span className="skeleton-line skeleton-line--medium" />
                      <span className="skeleton-line skeleton-line--long" />
                      <span className="skeleton-line skeleton-line--medium" />
                      <span className="skeleton-pill" />
                    </div>
                  ))}
                </div>
              </div>
            )}
            {!loading && keys.length === 0 && <p className="empty-state">No API keys found.</p>}
            {!loading && keys.length > 0 && (
              <div className="key-table" role="table" aria-label="Active API keys">
                <div className="key-table__row key-table__row--head" role="row">
                  <span role="columnheader">Name</span>
                  <span role="columnheader">Masked key</span>
                  <span role="columnheader">Updated</span>
                  <span role="columnheader">Actions</span>
                </div>
                {keys.map((item) => (
                  <div className="key-table__row" role="row" key={item.name}>
                    <span className="key-name" role="cell">{item.name}</span>
                    <span className="mono key-masked" role="cell">{item.key_masked}</span>
                    <span className="thread-meta" role="cell">{formatDate(item.updated_at)}</span>
                    <span className="key-actions" role="cell">
                      <button
                        className="mini-button mini-button--danger"
                        type="button"
                        onClick={() => void deleteKey(item.name)}
                        disabled={deletingName === item.name}
                      >
                        {deletingName === item.name ? "Deleting..." : "Delete"}
                      </button>
                    </span>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>
      </main>
    </div>
  );
}
