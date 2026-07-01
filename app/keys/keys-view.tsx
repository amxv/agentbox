"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { CopyButton } from "../components/copy-button";

const STORAGE_KEY = "agentbox_admin_key";

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
  const [key, setKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });
  const [draftKey, setDraftKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [newKeyName, setNewKeyName] = useState("");
  const [createdKey, setCreatedKey] = useState<CreatedAPIKey | null>(null);
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [deletingName, setDeletingName] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const loadKeys = useCallback(async function loadKeys(adminKey: string) {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/admin/keys", {
        headers: { "x-agentbox-admin-key": adminKey }
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setKeys(data.keys ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!key) return;
    const timeout = window.setTimeout(() => {
      void loadKeys(key);
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [key, loadKeys]);

  const latestUpdatedAt = useMemo(() => {
    if (keys.length === 0) return null;
    return keys.reduce((latest, item) => {
      const current = new Date(item.updated_at).getTime();
      return current > latest ? current : latest;
    }, 0);
  }, [keys]);

  function saveKey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = draftKey.trim();
    if (!trimmed) return;
    window.localStorage.setItem(STORAGE_KEY, trimmed);
    setKey(trimmed);
    setNotice(null);
    setCreatedKey(null);
  }

  function signOut() {
    window.localStorage.removeItem(STORAGE_KEY);
    setKey("");
    setDraftKey("");
    setKeys([]);
    setCreatedKey(null);
    setError(null);
    setNotice(null);
  }

  async function createKey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = newKeyName.trim();
    if (!name || !key) return;
    setCreating(true);
    setError(null);
    setNotice(null);
    try {
      const response = await fetch("/api/admin/keys", {
        method: "POST",
        headers: {
          "content-type": "application/json",
          "x-agentbox-admin-key": key
        },
        body: JSON.stringify({ name })
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setCreatedKey(data.key);
      setNewKeyName("");
      setNotice(`Created API key “${data.key.name}”. Store the secret now; it will not appear in the key list.`);
      await loadKeys(key);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  }

  async function deleteKey(name: string) {
    if (!key) return;
    const confirmed = window.confirm(`Delete API key “${name}”? Anything using this key will lose access immediately.`);
    if (!confirmed) return;
    setDeletingName(name);
    setError(null);
    setNotice(null);
    try {
      const response = await fetch(`/api/admin/keys/${encodeURIComponent(name)}`, {
        method: "DELETE",
        headers: { "x-agentbox-admin-key": key }
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setNotice(`Deleted API key “${data.revoked ?? name}”.`);
      if (createdKey?.name === name) setCreatedKey(null);
      await loadKeys(key);
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
            {key && <button className="site-nav__link" type="button" onClick={signOut}>Forget key</button>}
          </nav>
        </div>
      </header>

      <main className="dashboard-main shell">
        <section className="dashboard-header">
          <div className="dashboard-header__row">
            <div>
              <p className="section-label">Admin tools</p>
              <h1 className="dashboard-title">API keys</h1>
              <p className="dashboard-copy">Create lightweight agent credentials, review active keys, and revoke keys from the dashboard.</p>
            </div>
            {key && (
              <div className="card">
                <p className="stat-label">Active keys</p>
                <h2 className="card-title">{keys.length}</h2>
                <p className="copy">{latestUpdatedAt ? `Last changed ${formatDate(new Date(latestUpdatedAt).toISOString())}` : "No keys yet."}</p>
              </div>
            )}
          </div>
        </section>

        {!key ? (
          <form className="sign-in-card" onSubmit={saveKey}>
            <div>
              <p className="section-label">Sign in</p>
              <h2 className="card-title">Enter your admin key</h2>
              <p className="copy">The key is saved in this browser and sent as a request header to the admin API.</p>
            </div>
            <input
              className="form-input"
              value={draftKey}
              onChange={(event) => setDraftKey(event.target.value)}
              placeholder="ADMIN_KEY"
              type="password"
            />
            <button className="button button--solid" type="submit">Manage keys</button>
          </form>
        ) : (
          <div className="key-management-grid">
            <section className="sign-in-card key-create-card" aria-labelledby="create-key-title">
              <div>
                <p className="section-label">Create</p>
                <h2 id="create-key-title" className="card-title">New API key</h2>
                <p className="copy">Use clear names like local, chatgpt, codex, claude, or worker-prod. Reusing a name rotates that key.</p>
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
                  {creating ? "Creating…" : "Create key"}
                </button>
              </form>
            </section>

            <section className="key-list-card" aria-labelledby="active-keys-title">
              <div className="key-list-card__header">
                <div>
                  <p className="section-label">Current</p>
                  <h2 id="active-keys-title" className="card-title">Active keys</h2>
                </div>
                <button className="button button--ghost" type="button" onClick={() => void loadKeys(key)} disabled={loading}>Refresh</button>
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

              {loading && <p className="empty-state">Loading keys…</p>}
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
                          {deletingName === item.name ? "Deleting…" : "Delete"}
                        </button>
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </section>
          </div>
        )}
      </main>
    </div>
  );
}
