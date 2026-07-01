"use client";

import { FormEvent, useRef, useState } from "react";

type Props = {
  label: string;
  placeholder: string;
  submitLabel: string;
  onSubmit: (body: string, files: File[]) => Promise<void>;
  canSubmit?: boolean;
};

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** index;
  return `${value.toFixed(value >= 10 || index === 0 ? 0 : 1)} ${units[index]}`;
}

export function MessageComposer({ label, placeholder, submitLabel, onSubmit, canSubmit = true }: Props) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [body, setBody] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [dragging, setDragging] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function addFiles(nextFiles: FileList | File[]) {
    const incoming = Array.from(nextFiles);
    if (incoming.length === 0) return;
    setFiles((current) => [...current, ...incoming]);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting || !canSubmit || (!body.trim() && files.length === 0)) return;
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(body, files);
      setBody("");
      setFiles([]);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="composer-card" onSubmit={handleSubmit}>
      <div className="composer-card__header">
        <div>
          <p className="section-label">Post as user</p>
          <h2 className="card-title">{label}</h2>
        </div>
        <button className="button button--solid" disabled={submitting || !canSubmit || (!body.trim() && files.length === 0)} type="submit">
          {submitting ? "Posting…" : submitLabel}
        </button>
      </div>
      <textarea
        className="composer-textarea"
        placeholder={placeholder}
        value={body}
        onChange={(event) => setBody(event.target.value)}
      />
      <div
        className={dragging ? "dropzone dropzone--active" : "dropzone"}
        role="button"
        tabIndex={0}
        onClick={() => inputRef.current?.click()}
        onDragEnter={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragOver={(event) => event.preventDefault()}
        onDragLeave={(event) => {
          event.preventDefault();
          setDragging(false);
        }}
        onDrop={(event) => {
          event.preventDefault();
          setDragging(false);
          addFiles(event.dataTransfer.files);
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            inputRef.current?.click();
          }
        }}
      >
        <input
          ref={inputRef}
          multiple
          hidden
          type="file"
          onChange={(event) => {
            if (event.target.files) addFiles(event.target.files);
            event.target.value = "";
          }}
        />
        <span>Drop files here or click to attach</span>
      </div>
      {files.length > 0 && (
        <div className="file-chip-list" aria-label="Selected files">
          {files.map((file, index) => (
            <span className="file-chip" key={`${file.name}-${file.size}-${index}`}>
              <span>{file.name}</span>
              <span className="thread-meta">{formatBytes(file.size)}</span>
              <button
                aria-label={`Remove ${file.name}`}
                className="mini-button"
                type="button"
                onClick={() => setFiles((current) => current.filter((_, fileIndex) => fileIndex !== index))}
              >
                Remove
              </button>
            </span>
          ))}
        </div>
      )}
      {error && (
        <div className="error-card">
          <strong>Could not post.</strong>
          <span>{error}</span>
        </div>
      )}
    </form>
  );
}
