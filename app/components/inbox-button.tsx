"use client";

import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";
import {
  getActiveViewerProfile,
  loadViewerProfiles,
  setActiveViewerProfileId,
  upsertViewerProfile,
  type ViewerProfile
} from "./viewer-profiles";

type InboxButtonProps = {
  className?: string;
  label?: string;
};

function loadDraftState() {
  const savedProfiles = loadViewerProfiles();
  const active = getActiveViewerProfile();
  return {
    profiles: savedProfiles,
    selectedId: active?.id ?? savedProfiles[0]?.id ?? "",
    name: active?.name ?? "",
    key: active?.adminKey ?? ""
  };
}

export function InboxButton({ className, label = "View inbox" }: InboxButtonProps) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [profiles, setProfiles] = useState<ViewerProfile[]>(() => loadDraftState().profiles);
  const [selectedId, setSelectedId] = useState(() => loadDraftState().selectedId);
  const [name, setName] = useState(() => loadDraftState().name);
  const [key, setKey] = useState(() => loadDraftState().key);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedName = name.trim();
    const trimmed = key.trim();
    if (!trimmedName || !trimmed) return;
    const profile = upsertViewerProfile({
      id: selectedId || null,
      name: trimmedName,
      adminKey: trimmed
    });
    setProfiles(loadViewerProfiles());
    setSelectedId(profile.id);
    setOpen(false);
    router.push("/threads");
  }

  return (
    <>
      <button
        className={className}
        type="button"
        onClick={() => {
          const state = loadDraftState();
          setProfiles(state.profiles);
          setSelectedId(state.selectedId);
          setName(state.name);
          setKey(state.key);
          setOpen(true);
        }}
      >
        {label}
      </button>
      {open && (
        <div style={styles.backdrop} role="presentation" onClick={() => setOpen(false)}>
          <form style={styles.modal} onSubmit={submit} onClick={(event) => event.stopPropagation()}>
            <div>
              <p style={styles.eyebrow}>Agentbox inbox</p>
              <h2 style={styles.title}>Open a viewer profile</h2>
              <p style={styles.copy}>Save multiple admin keys in this browser and choose which profile to use for the read-only inbox.</p>
            </div>
            {profiles.length > 0 && (
              <label style={styles.field}>
                <span style={styles.label}>Saved profiles</span>
                <select
                  value={selectedId}
                  onChange={(event) => {
                    const nextId = event.target.value;
                    const profile = profiles.find((entry) => entry.id === nextId) ?? null;
                    setSelectedId(nextId);
                    setActiveViewerProfileId(nextId);
                    setName(profile?.name ?? "");
                    setKey(profile?.adminKey ?? "");
                  }}
                  style={styles.input}
                >
                  {profiles.map((profile) => (
                    <option key={profile.id} value={profile.id}>{profile.name}</option>
                  ))}
                </select>
              </label>
            )}
            <label style={styles.field}>
              <span style={styles.label}>Profile name</span>
              <input
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="Production"
                type="text"
                style={styles.input}
              />
            </label>
            <input
              autoFocus
              value={key}
              onChange={(event) => setKey(event.target.value)}
              placeholder="ADMIN_KEY"
              type="password"
              style={styles.input}
            />
            <div style={styles.actions}>
              <button
                type="button"
                style={styles.secondary}
                onClick={() => {
                  setSelectedId("");
                  setName("");
                  setKey("");
                }}
              >
                New profile
              </button>
              <button type="button" style={styles.secondary} onClick={() => setOpen(false)}>Cancel</button>
              <button type="submit" style={styles.primary}>Save and open</button>
            </div>
          </form>
        </div>
      )}
    </>
  );
}

const styles: Record<string, React.CSSProperties> = {
  backdrop: {
    position: "fixed",
    inset: 0,
    zIndex: 50,
    display: "grid",
    placeItems: "center",
    padding: 18,
    background: "rgba(28, 25, 21, 0.42)",
    backdropFilter: "blur(8px)"
  },
  modal: {
    width: "min(420px, 100%)",
    display: "grid",
    gap: 18,
    border: "1px solid rgba(39, 31, 22, 0.12)",
    borderRadius: 24,
    padding: 22,
    background: "#fffaf0",
    boxShadow: "0 24px 80px rgba(26, 20, 14, 0.25)",
    color: "#1c1915"
  },
  eyebrow: {
    margin: "0 0 8px",
    color: "#a24f2f",
    fontSize: 12,
    fontWeight: 800,
    letterSpacing: "0.12em",
    textTransform: "uppercase"
  },
  title: {
    margin: "0 0 8px",
    fontFamily: "ui-serif, Georgia, Cambria, Times New Roman, Times, serif",
    fontSize: 34,
    letterSpacing: "-0.05em",
    lineHeight: 1
  },
  copy: {
    margin: 0,
    color: "#5e574f",
    lineHeight: 1.55
  },
  input: {
    width: "100%",
    border: "1px solid rgba(39, 31, 22, 0.18)",
    borderRadius: 14,
    padding: "13px 14px",
    background: "#fff",
    color: "#1c1915",
    font: "inherit"
  },
  field: {
    display: "grid",
    gap: 8
  },
  label: {
    fontSize: 12,
    fontWeight: 800,
    letterSpacing: "0.08em",
    textTransform: "uppercase",
    color: "#756d63"
  },
  actions: {
    display: "flex",
    justifyContent: "flex-end",
    gap: 10
  },
  primary: {
    border: 0,
    borderRadius: 999,
    padding: "11px 16px",
    background: "#1c1915",
    color: "#fffaf0",
    cursor: "pointer",
    fontWeight: 800
  },
  secondary: {
    border: "1px solid rgba(39, 31, 22, 0.14)",
    borderRadius: 999,
    padding: "11px 16px",
    background: "transparent",
    color: "#1c1915",
    cursor: "pointer",
    fontWeight: 800
  }
};
