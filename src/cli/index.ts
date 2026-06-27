import { readFile } from "node:fs/promises";
import { basename } from "node:path";
import { lookup } from "mime-types";
import { Command } from "commander";

type RequestOptions = {
  method?: string;
  body?: BodyInit;
  headers?: HeadersInit;
};

type CliAsset = {
  file_name: string;
  public_url?: string | null;
  storage_key: string;
};

type CliMessage = {
  id: string;
  author: string;
  body: string;
  created_at: string;
  assets?: CliAsset[];
};

type CliThread = {
  id: string;
  title: string;
  updated_at: string;
  messages?: CliMessage[];
};

function baseUrl(): string {
  const value = process.env.AGENTBOX_BASE_URL ?? process.env.AGENTBOX_URL;
  if (!value) throw new Error("Set AGENTBOX_BASE_URL to your Agentbox deployment URL.");
  return value.replace(/\/$/, "");
}

function apiKey(): string {
  const value = process.env.AGENTBOX_API_KEY;
  if (!value) throw new Error("Set AGENTBOX_API_KEY.");
  return value;
}

async function request(path: string, options: RequestOptions = {}) {
  const headers = new Headers(options.headers);
  headers.set("authorization", `Bearer ${apiKey()}`);

  const response = await fetch(`${baseUrl()}${path}`, { ...options, headers });
  const text = await response.text();
  const data = text ? JSON.parse(text) : null;

  if (!response.ok) {
    throw new Error(data?.error ?? `Request failed with HTTP ${response.status}`);
  }

  return data;
}

async function readStdin(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}

function printJson(data: unknown) {
  console.log(JSON.stringify(data, null, 2));
}

function printThread(thread: CliThread) {
  console.log(`# ${thread.title}`);
  console.log(`id: ${thread.id}`);
  console.log(`updated: ${thread.updated_at}`);
  console.log("");

  for (const message of thread.messages ?? []) {
    console.log(`--- ${message.author} · ${message.created_at} · ${message.id}`);
    console.log(message.body);
    if (message.assets?.length) {
      console.log("");
      console.log("Assets:");
      for (const asset of message.assets) {
        console.log(`- ${asset.file_name} ${asset.public_url ?? asset.storage_key}`);
      }
    }
    console.log("");
  }
}

const program = new Command();
program
  .name("agentbox")
  .description("CLI for Agentbox, a small threaded message relay for ChatGPT and local agents.")
  .version("0.1.0");

program
  .command("list")
  .description("List recent threads.")
  .option("-n, --limit <number>", "maximum number of threads", "50")
  .option("--json", "print raw JSON")
  .action(async (options) => {
    const data = await request(`/api/threads?limit=${Number(options.limit)}`);
    if (options.json) return printJson(data);

    for (const thread of data.threads) {
      console.log(`${thread.id}\t${thread.updated_at}\t${thread.title}`);
    }
  });

program
  .command("create")
  .description("Create a thread.")
  .argument("<title>", "thread title")
  .option("--json", "print raw JSON")
  .action(async (title, options) => {
    const data = await request("/api/threads", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ title })
    });

    if (options.json) return printJson(data);
    console.log(`${data.thread.id}\t${data.thread.title}`);
  });

program
  .command("get")
  .description("Read a thread.")
  .argument("<thread-id>", "thread id")
  .option("--json", "print raw JSON")
  .action(async (threadId, options) => {
    const data = await request(`/api/threads/${encodeURIComponent(threadId)}`);
    if (options.json) return printJson(data);
    printThread(data.thread);
  });

program
  .command("post")
  .description("Post a message to a thread.")
  .argument("<thread-id>", "thread id")
  .argument("[message]", "message body")
  .option("-f, --file <path>", "read message body from a Markdown/text file")
  .option("-a, --asset <path>", "attach a local file")
  .option("--json", "print raw JSON")
  .action(async (threadId, message, options) => {
    let body = message ?? "";
    if (options.file) body = await readFile(options.file, "utf8");
    if (!body && !process.stdin.isTTY) body = await readStdin();

    let data;
    if (options.asset) {
      const form = new FormData();
      form.set("body", body);
      const bytes = await readFile(options.asset);
      const fileName = basename(options.asset);
      const type = lookup(fileName) || "application/octet-stream";
      form.set("asset", new Blob([bytes], { type }), fileName);
      data = await request(`/api/threads/${encodeURIComponent(threadId)}/messages`, {
        method: "POST",
        body: form
      });
    } else {
      data = await request(`/api/threads/${encodeURIComponent(threadId)}/messages`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ body })
      });
    }

    if (options.json) return printJson(data);
    console.log(data.message.id);
  });

program.parseAsync().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
