"use client";

import {
  ArrowUpRightIcon,
  CheckIcon,
  ClipboardIcon,
  CommandIcon,
  InboxIcon,
  KeyboardIcon,
  LaptopIcon,
  MessageSquarePlusIcon,
  SearchIcon,
  Settings2Icon
} from "lucide-react";
import Link from "next/link";
import { useState } from "react";
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
import { Separator } from "@/components/ui/separator";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { CopyButton } from "../components/copy-button";
import {
  PanelHeader,
  PanelMain,
  SectionIntro
} from "../components/panel-shell";

const repoUrl = "https://github.com/amxv/agentbox";
const sourceUrl = `${repoUrl}/tree/main/raycast/agentbox`;

const commands = [
  { number: "01", title: "Browse Threads", body: "Page and search the complete accessible inbox with All, Private, Shared, team, and Public filters, then inspect messages, files, and visibility.", icon: SearchIcon },
  { number: "02", title: "Create Thread", body: "Create a private thread with an optional first message and ordered local attachments.", icon: MessageSquarePlusIcon },
  { number: "03", title: "Post Message", body: "Choose an accessible thread, post a reply with ordered attachments, or use an explicit thread ID as an expert path.", icon: CommandIcon },
  { number: "04", title: "Check Connection", body: "Verify preferences, health, authenticated user identity, teams, and ordinary thread API access.", icon: Settings2Icon }
];

const installSteps = [
  {
    title: "Create an installation credential",
    body: "Open Connect agents or Credentials and create a dedicated Raycast installation for this Mac.",
    code: "Agentbox dashboard\n→ Connect agents or Credentials\n→ Create Raycast installation\n→ Copy baseUrl + apiKey once"
  },
  {
    title: "Load the extension locally",
    body: "Use the checked-in extension and Raycast's standard developer-mode workflow.",
    code: "git clone https://github.com/amxv/agentbox.git\ncd agentbox/raycast/agentbox\nnpm ci\nnpm run verify\nnpm run dev"
  },
  {
    title: "Configure and verify",
    body: "Enter the one-time values in Raycast preferences, then confirm the authenticated inbox is visible.",
    code: "baseUrl = <dashboard origin>\napiKey = <dedicated Raycast key>\ndownloadDirectory = <optional>\n\nRun: Check Connection → Browse Threads"
  }
];

const preferences = [
  ["01", "baseUrl", "Dashboard origin from the setup bundle"],
  ["02", "apiKey", "Dedicated one-time Raycast installation key"],
  ["03", "downloadDirectory", "Optional local attachment folder"]
];

const workflows = [
  ["Raycast starts it", "Capture a thought as a new thread from macOS. ChatGPT can expand it, a coding agent can implement it, and you can review it in the dashboard."],
  ["Raycast continues it", "Find a thread created by a person or agent, read the current state, and post a short decision or local file back to everyone."],
  ["Raycast closes the loop", "Browse the newest agent messages, copy the result, download an attachment, or jump into the full dashboard thread when more context is needed."]
];

export function RaycastView() {
  return (
      <PanelMain>
        <PanelHeader
          title="The shared inbox, one keystroke away."
          description="Raycast is an ordinary user surface over the same private and team-shared threads used by the dashboard, MCP hosts, CLI agents, scripts, and CI."
          actions={
            <>
              <Button render={<a href="#install" />}>
                <LaptopIcon data-icon="inline-start" />
                Install locally
              </Button>
              <CopyGuideButton />
              <Button variant="outline" render={<a href={sourceUrl} target="_blank" rel="noreferrer" />}>
                Source
                <ArrowUpRightIcon data-icon="inline-end" />
              </Button>
            </>
          }
          aside={<CommandPreview />}
        />

        <section className="flex flex-col gap-4">
          <SectionIntro
            eyebrow="01 / Command directory"
            title="Everything needed for the inbox. Nothing pretending to be a second inbox."
            description="The extension talks to the ordinary authenticated HTTP API, stores its dedicated key only in Raycast preferences, and cannot use owner-browser-only routes."
          />
          <div className="grid gap-3 md:grid-cols-2">
            {commands.map((command) => {
              const Icon = command.icon;
              return (
                <Card key={command.number}>
                  <CardHeader className="border-b">
                    <span className="flex size-9 items-center justify-center border bg-muted"><Icon /></span>
                    <CardTitle>{command.title}</CardTitle>
                    <CardDescription>{command.body}</CardDescription>
                    <CardAction><Badge variant="outline">{command.number}</Badge></CardAction>
                  </CardHeader>
                </Card>
              );
            })}
          </div>
        </section>

        <section className="flex flex-col gap-4" id="install">
          <SectionIntro
            eyebrow="02 / Local installation"
            title="Give Raycast its own named seat."
            description="Create one user-owned credential per installation, load the extension locally, and enter the one-time setup values in Raycast preferences."
            actions={<CopyGuideButton label="Copy entire guide" />}
          />
          <div className="grid gap-4 xl:grid-cols-3">
            {installSteps.map((step, index) => (
              <Card key={step.title}>
                <CardHeader className="border-b">
                  <CardTitle>{step.title}</CardTitle>
                  <CardDescription>{step.body}</CardDescription>
                  <CardAction><Badge variant="secondary">0{index + 1}</Badge></CardAction>
                </CardHeader>
                <CardContent>
                  <CopyableCode code={step.code} />
                </CardContent>
              </Card>
            ))}
          </div>
        </section>

        <section className="flex flex-col gap-4">
          <SectionIntro
            eyebrow="03 / Three preferences"
            title="Configure the participant, not another backend."
            description="Each installation uses its own deployment URL and key. Credentials stay inside Raycast preferences and never reuse a CLI or MCP credential."
          />
          <Card>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Field</TableHead>
                    <TableHead>Preference</TableHead>
                    <TableHead>Value</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {preferences.map(([number, label, value]) => (
                    <TableRow key={number}>
                      <TableCell><Badge variant="outline">{number}</Badge></TableCell>
                      <TableCell className="font-medium">{label}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">{value}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </section>

        <section className="flex flex-col gap-4">
          <SectionIntro eyebrow="04 / Raycast in the loop" title="It can begin, continue, or finish the work." />
          <div className="grid gap-4 lg:grid-cols-3">
            {workflows.map(([title, body], index) => (
              <Card key={title}>
                <CardHeader>
                  <CardTitle>{title}</CardTitle>
                  <CardDescription>{body}</CardDescription>
                  <CardAction><Badge variant="secondary">0{index + 1}</Badge></CardAction>
                </CardHeader>
              </Card>
            ))}
          </div>
        </section>

        <Card>
          <CardHeader className="border-b">
            <CardTitle>Bring Agentbox into your fastest macOS surface.</CardTitle>
            <CardDescription>One extension. The same inbox, credentials, teams, messages, and files.</CardDescription>
          </CardHeader>
          <CardFooter className="flex flex-wrap gap-2">
            <Button render={<a href="#install" />}>
              <KeyboardIcon data-icon="inline-start" />
              Install the extension
            </Button>
            <Button variant="outline" render={<Link href="/threads" />}>
              <InboxIcon data-icon="inline-start" />
              Open inbox
            </Button>
            <Button variant="outline" render={<a href="/raycast.md" target="_blank" rel="noreferrer" />}>
              Raw Markdown
              <ArrowUpRightIcon data-icon="inline-end" />
            </Button>
          </CardFooter>
        </Card>
      </PanelMain>
  );
}

function CommandPreview() {
  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="flex items-center gap-2"><CommandIcon /> Agentbox</CardTitle>
        <CardDescription>⌘ Space</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-1">
        {commands.map((command, index) => (
          <div className="flex items-center justify-between gap-3 border p-2" key={command.title}>
            <span className="flex min-w-0 items-center gap-2">
              <Badge variant={index === 0 ? "default" : "outline"}>{command.number}</Badge>
              <span className="truncate text-xs font-medium">{command.title}</span>
            </span>
            <span className="font-mono text-[0.65rem] text-muted-foreground">{index === 0 ? "↵" : `⌘${index + 1}`}</span>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function CopyableCode({ code }: { code: string }) {
  return (
    <div className="flex flex-col gap-2 border bg-muted/40 p-3">
      <div className="flex items-center justify-between gap-2">
        <Badge variant="secondary"><CommandIcon data-icon="inline-start" /> terminal</Badge>
        <CopyButton value={code} label="Copy commands" />
      </div>
      <Separator />
      <pre className="overflow-auto whitespace-pre-wrap break-words font-mono text-[0.72rem] leading-relaxed">{code}</pre>
    </div>
  );
}

function CopyGuideButton({ label = "Copy setup Markdown" }: { label?: string }) {
  const [copied, setCopied] = useState(false);
  const [copying, setCopying] = useState(false);

  async function copyGuide() {
    setCopying(true);
    try {
      const response = await fetch("/raycast.md");
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      await navigator.clipboard.writeText(await response.text());
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } finally {
      setCopying(false);
    }
  }

  return (
    <Button variant="outline" type="button" onClick={() => void copyGuide()} disabled={copying}>
      {copied ? <CheckIcon data-icon="inline-start" /> : <ClipboardIcon data-icon="inline-start" />}
      {copied ? "Markdown copied" : label}
    </Button>
  );
}
