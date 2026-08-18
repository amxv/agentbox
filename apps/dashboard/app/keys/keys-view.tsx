"use client";

import {
  CableIcon,
  KeyRoundIcon,
  LaptopIcon,
  PlusIcon,
  RefreshCwIcon,
  RotateCwIcon,
  Settings2Icon,
  ShieldAlertIcon,
  TerminalIcon,
  Trash2Icon,
  XIcon
} from "lucide-react";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle
} from "@/components/ui/card";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle
} from "@/components/ui/empty";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { CopyButton } from "../components/copy-button";
import { fetchSession } from "../components/session";
import {
  DetailRow,
  MetricStrip,
  MonoValue,
  PanelHeader,
  PanelMain,
} from "../components/panel-shell";

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

type CredentialConfirmation = {
  action: "rotate" | "revoke";
  credential: Credential;
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
  const [confirmation, setConfirmation] = useState<CredentialConfirmation | null>(null);

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
      setConfirmation(null);
      await loadCredentials();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActingCredentialID(null);
    }
  }

  async function revokeCredential(credential: Credential) {
    setActingCredentialID(credential.id);
    setError(null);
    setNotice(null);
    try {
      const response = await fetch(`/api/keys/${encodeURIComponent(credential.id)}`, { method: "DELETE" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
      setNotice(`Revoked credential ${data.revoked ?? credential.id}.`);
      if (secretReveal?.credential.id === credential.id) setSecretReveal(null);
      setConfirmation(null);
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
    <>
      <PanelMain>
        <PanelHeader
          title="Credentials and installations."
          description="Create an independent credential for every actor surface, inspect safe usage metadata, and retain revoked rows as audit history."
          aside={
            <MetricStrip
              items={[
                { label: "Active", value: activeCount, detail: "currently authorized" },
                { label: "Revoked history", value: revokedCount, detail: "retained for audit" }
              ]}
            />
          }
        />

        {notice ? (
          <Alert>
            <KeyRoundIcon />
            <AlertTitle>Credential inventory updated</AlertTitle>
            <AlertDescription>{notice}</AlertDescription>
          </Alert>
        ) : null}
        {error ? (
          <Alert variant="destructive">
            <ShieldAlertIcon />
            <AlertTitle>Could not manage credentials</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <section className="grid gap-4 lg:grid-cols-2" aria-label="Create credentials">
          <Card>
            <CardHeader className="border-b">
              <CardTitle>New API key</CardTitle>
              <CardDescription>Use a clear label for a local agent, worker, or another supported client.</CardDescription>
              <CardAction><TerminalIcon /></CardAction>
            </CardHeader>
            <CardContent>
              <form onSubmit={createCustomKey}>
                <FieldGroup>
                  <Field>
                    <FieldLabel htmlFor="custom-credential-name">Credential label</FieldLabel>
                    <Input
                      id="custom-credential-name"
                      value={newKeyName}
                      onChange={(event) => setNewKeyName(event.target.value)}
                      placeholder="Codex on MacBook"
                    />
                  </Field>
                  <Button type="submit" disabled={creating !== null || !newKeyName.trim()}>
                    {creating === "custom" ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                    Create API key
                  </Button>
                </FieldGroup>
              </form>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="border-b">
              <CardTitle>New Raycast installation</CardTitle>
              <CardDescription>Give each Mac its own least-privilege key and developer-mode setup bundle.</CardDescription>
              <CardAction><LaptopIcon /></CardAction>
            </CardHeader>
            <CardContent>
              <form onSubmit={createRaycastInstallation}>
                <FieldGroup>
                  <Field>
                    <FieldLabel htmlFor="raycast-installation-label">Installation label</FieldLabel>
                    <Input
                      id="raycast-installation-label"
                      value={raycastLabel}
                      onChange={(event) => setRaycastLabel(event.target.value)}
                      placeholder="MacBook Air"
                    />
                  </Field>
                  <Button type="submit" disabled={creating !== null || !raycastLabel.trim()}>
                    {creating === "raycast" ? <Spinner data-icon="inline-start" /> : <CableIcon data-icon="inline-start" />}
                    Create Raycast installation
                  </Button>
                </FieldGroup>
              </form>
            </CardContent>
          </Card>
        </section>

        {secretReveal ? (
          <Card>
            <CardHeader className="border-b">
              <CardTitle>Secret shown once</CardTitle>
              <CardDescription>
                Copy the secret now. Agentbox stores only its hash and safe prefix after this page is refreshed.
              </CardDescription>
              <CardAction><Badge>{secretReveal.credential.name}</Badge></CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-5">
              <SecretRow label="API key" value={secretReveal.credential.key} />
              {secretReveal.raycastSetup ? (
                <RaycastSetupPanel setup={secretReveal.raycastSetup} />
              ) : (
                <SecretRow label="Authenticated MCP URL" value={getMCPURL(secretReveal.credential.key)} />
              )}
            </CardContent>
            <CardFooter className="flex flex-wrap justify-between gap-3">
              <MonoValue>{secretReveal.credential.id}</MonoValue>
              <Button variant="outline" onClick={() => setSecretReveal(null)}>Dismiss</Button>
            </CardFooter>
          </Card>
        ) : null}

        {setupPreview ? (
          <Card>
            <CardHeader className="border-b">
              <CardTitle>{setupPreview.label}</CardTitle>
              <CardDescription>Saved non-secret setup instructions for this Raycast installation.</CardDescription>
              <CardAction>
                <Button size="icon-sm" variant="ghost" aria-label="Close setup preview" onClick={() => setSetupPreview(null)}>
                  <XIcon />
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent>
              <RaycastSetupPanel setup={setupPreview} />
            </CardContent>
          </Card>
        ) : null}

        <Card>
          <CardHeader className="border-b">
            <CardTitle>Credential inventory</CardTitle>
            <CardDescription>Active and revoked credentials, including scopes and safe usage metadata.</CardDescription>
            <CardAction>
              <Button variant="outline" onClick={() => void loadCredentials()} disabled={loading}>
                {loading ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
                Refresh
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {loading ? <CredentialSkeleton /> : null}
            {!loading && credentials.length === 0 ? (
              <Empty className="border py-16">
                <EmptyHeader>
                  <EmptyMedia variant="icon"><KeyRoundIcon /></EmptyMedia>
                  <EmptyTitle>No credentials found</EmptyTitle>
                  <EmptyDescription>Create a custom key or Raycast installation above.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : null}
            {!loading ? credentials.map((credential) => {
              const revoked = Boolean(credential.revoked_at);
              const acting = actingCredentialID === credential.id;
              return (
                <Card size="sm" key={credential.id}>
                  <CardHeader className="border-b">
                    <div className="flex min-w-0 flex-col gap-1">
                      <CardTitle className="flex flex-wrap items-center gap-2">
                        {credential.name}
                        <Badge variant={revoked ? "destructive" : "outline"}>{revoked ? "Revoked" : "Active"}</Badge>
                        <Badge variant="secondary">{credential.purpose || "custom"}</Badge>
                      </CardTitle>
                      <MonoValue>{credential.id}</MonoValue>
                    </div>
                    <CardAction className="flex flex-wrap gap-2">
                      {credential.purpose === "raycast" ? (
                        <Button variant="outline" size="sm" onClick={() => void openRaycastSetup(credential)} disabled={acting}>
                          {acting ? <Spinner data-icon="inline-start" /> : <Settings2Icon data-icon="inline-start" />}
                          Setup
                        </Button>
                      ) : null}
                      {!revoked ? (
                        <>
                          <Button variant="outline" size="sm" onClick={() => setConfirmation({ action: "rotate", credential })} disabled={acting}>
                            <RotateCwIcon data-icon="inline-start" />
                            Rotate
                          </Button>
                          <Button variant="destructive" size="sm" onClick={() => setConfirmation({ action: "revoke", credential })} disabled={acting}>
                            <Trash2Icon data-icon="inline-start" />
                            Revoke
                          </Button>
                        </>
                      ) : null}
                    </CardAction>
                  </CardHeader>
                  <CardContent>
                    <dl>
                      <DetailRow label="Masked token" value={<MonoValue>{credential.key_masked || credential.token_prefix}</MonoValue>} />
                      <DetailRow label="Scopes" value={credential.scopes.length > 0 ? credential.scopes.join(", ") : "Legacy/default"} />
                      <DetailRow label="Created" value={formatDate(credential.created_at)} />
                      <DetailRow label="Last used" value={formatDate(credential.last_used_at)} />
                      <DetailRow label="Revoked" value={credential.revoked_at ? formatDate(credential.revoked_at) : "No"} />
                    </dl>
                  </CardContent>
                </Card>
              );
            }) : null}
          </CardContent>
          {page?.has_more && page.next_cursor ? (
            <CardFooter>
              <Button variant="outline" onClick={() => void loadCredentials(page.next_cursor ?? undefined)} disabled={loadingMore}>
                {loadingMore ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                Load more credentials
              </Button>
            </CardFooter>
          ) : null}
        </Card>
      </PanelMain>

      <AlertDialog open={Boolean(confirmation)} onOpenChange={(open) => { if (!open) setConfirmation(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>{confirmation?.action === "rotate" ? <RotateCwIcon /> : <Trash2Icon />}</AlertDialogMedia>
            <AlertDialogTitle>{confirmation?.action === "rotate" ? "Rotate this credential?" : "Revoke this credential?"}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirmation?.action === "rotate"
                ? `The current secret for “${confirmation.credential.name}” will stop working immediately. A replacement secret will be shown once.`
                : `“${confirmation?.credential.name ?? "This credential"}” will stop working immediately, but its audit row will remain visible.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={Boolean(confirmation && actingCredentialID === confirmation.credential.id)}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmation?.action === "revoke" ? "destructive" : "default"}
              disabled={!confirmation || actingCredentialID === confirmation.credential.id}
              onClick={() => {
                if (!confirmation) return;
                if (confirmation.action === "rotate") void rotateCredential(confirmation.credential);
                else void revokeCredential(confirmation.credential);
              }}
            >
              {confirmation && actingCredentialID === confirmation.credential.id ? <Spinner data-icon="inline-start" /> : confirmation?.action === "rotate" ? <RotateCwIcon data-icon="inline-start" /> : <Trash2Icon data-icon="inline-start" />}
              {confirmation?.action === "rotate" ? "Rotate credential" : "Revoke credential"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function SecretRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-2">
      <span className="font-mono text-[0.65rem] tracking-[0.1em] text-muted-foreground uppercase">{label}</span>
      <div className="flex min-w-0 items-center gap-2 border bg-muted/40 p-2">
        <MonoValue className="flex-1 text-foreground">{value}</MonoValue>
        <CopyButton value={value} label={`Copy ${label}`} />
      </div>
    </div>
  );
}

function RaycastSetupPanel({ setup }: { setup: RaycastSetup }) {
  return (
    <div className="flex flex-col gap-5">
      <dl>
        <DetailRow label="Repository" value={<MonoValue>{setup.repository_url}</MonoValue>} />
        <DetailRow label="Extension path" value={<MonoValue>{setup.extension_path}</MonoValue>} />
        <DetailRow label="Agentbox URL" value={<MonoValue>{setup.base_url}</MonoValue>} />
      </dl>
      <Separator />
      <section className="flex flex-col gap-3">
        <span className="font-mono text-[0.65rem] tracking-[0.1em] text-muted-foreground uppercase">Install commands</span>
        {setup.install_commands.map((command) => <SecretRow key={command} label="Terminal" value={command} />)}
      </section>
      <Separator />
      <section className="flex flex-col gap-3">
        <span className="font-mono text-[0.65rem] tracking-[0.1em] text-muted-foreground uppercase">Raycast preferences</span>
        {setup.preferences.map((preference) => {
          const value = setupPreferenceValue(preference, setup);
          return preference.value ? (
            <SecretRow key={preference.name} label={preference.title} value={preference.value} />
          ) : (
            <Alert key={preference.name}>
              <AlertTitle>{preference.title}</AlertTitle>
              <AlertDescription>{value}</AlertDescription>
            </Alert>
          );
        })}
      </section>
      <Alert>
        <Settings2Icon />
        <AlertTitle>Final check</AlertTitle>
        <AlertDescription>{setup.final_check}</AlertDescription>
      </Alert>
    </div>
  );
}

function CredentialSkeleton() {
  return (
    <div className="flex flex-col gap-3" aria-label="Loading credentials" aria-busy="true">
      {Array.from({ length: 3 }).map((_, index) => (
        <Card size="sm" key={index}>
          <CardHeader className="border-b">
            <Skeleton className="h-4 w-48" />
            <Skeleton className="h-3 w-72" />
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
