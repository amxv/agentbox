"use client";

import { FileIcon, PaperclipIcon, SendIcon, UploadCloudIcon, XIcon } from "lucide-react";
import { FormEvent, useRef, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { PanelEyebrow } from "./panel-shell";

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
    <form onSubmit={handleSubmit}>
      <Card>
        <CardHeader className="border-b">
          <div className="flex min-w-0 flex-col gap-2">
            <PanelEyebrow>Post as user</PanelEyebrow>
            <CardTitle>{label}</CardTitle>
          </div>
          <CardAction>
            <Button
              disabled={submitting || !canSubmit || (!body.trim() && files.length === 0)}
              type="submit"
            >
              <SendIcon data-icon="inline-start" />
              {submitting ? "Posting" : submitLabel}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field>
              <FieldLabel className="sr-only">Message</FieldLabel>
              <Textarea
                className="min-h-40 resize-y"
                placeholder={placeholder}
                value={body}
                onChange={(event) => setBody(event.target.value)}
              />
              <FieldDescription>Markdown is detected automatically. Attachments keep their selected order.</FieldDescription>
            </Field>

            <div
              className={cn(
                "flex min-h-32 items-center justify-center border border-dashed p-6 text-center transition-colors",
                dragging ? "border-foreground bg-muted" : "border-border bg-muted/30 hover:bg-muted/60"
              )}
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
              <span className="flex flex-col items-center gap-3 text-sm text-muted-foreground">
                {dragging ? <UploadCloudIcon /> : <PaperclipIcon />}
                <span>{dragging ? "Release to attach files" : "Drop files here or click to attach"}</span>
              </span>
            </div>

            {files.length > 0 ? (
              <div className="grid gap-3" aria-label="Selected files">
                {files.map((file, index) => (
                  <div className="flex min-w-0 items-center gap-4 border bg-muted/20 p-3" key={`${file.name}-${file.size}-${index}`}>
                    <FileIcon className="shrink-0 text-muted-foreground" />
                    <span className="flex min-w-0 flex-1 flex-col gap-1">
                      <span className="truncate text-sm font-medium">{file.name}</span>
                      <span className="font-mono text-xs text-muted-foreground">{formatBytes(file.size)}</span>
                    </span>
                    <Button
                      aria-label={`Remove ${file.name}`}
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setFiles((current) => current.filter((_, fileIndex) => fileIndex !== index))}
                    >
                      <XIcon />
                    </Button>
                  </div>
                ))}
              </div>
            ) : null}

            {error ? (
              <Alert variant="destructive">
                <AlertTitle>Could not post</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}
          </FieldGroup>
        </CardContent>
      </Card>
    </form>
  );
}
