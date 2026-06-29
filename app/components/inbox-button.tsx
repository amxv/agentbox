"use client";

import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

const STORAGE_KEY = "agentbox_admin_key";

type InboxButtonProps = {
  className?: string;
  label?: string;
};

export function InboxButton({ className = "button button--solid", label = "View inbox" }: InboxButtonProps) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [key, setKey] = useState(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? "";
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = key.trim();
    if (!trimmed) return;
    window.localStorage.setItem(STORAGE_KEY, trimmed);
    setOpen(false);
    router.push("/threads");
  }

  return (
    <>
      <button className={className} type="button" onClick={() => setOpen(true)}>
        {label}
      </button>
      {open && (
        <div className="modal-backdrop" role="presentation" onClick={() => setOpen(false)}>
          <form className="modal-card" onSubmit={submit} onClick={(event) => event.stopPropagation()}>
            <div>
              <p className="section-label">Agentbox inbox</p>
              <h2 className="card-title">Enter your admin key</h2>
              <p className="copy">The key is stored in this browser and used only to load the read-only thread viewer.</p>
            </div>
            <input
              autoFocus
              className="form-input"
              value={key}
              onChange={(event) => setKey(event.target.value)}
              placeholder="ADMIN_KEY"
              type="password"
            />
            <div className="modal-actions">
              <button className="button button--ghost" type="button" onClick={() => setOpen(false)}>Cancel</button>
              <button className="button button--solid" type="submit">Open inbox</button>
            </div>
          </form>
        </div>
      )}
    </>
  );
}
