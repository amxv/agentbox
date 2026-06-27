import { basename, dirname, join } from "node:path";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { lookup } from "mime-types";
import { Command } from "commander";

type RequestOptions = {
  method?: string;
  body?: BodyInit;
  headers?: HeadersInit;
};

type CliAsset = {
  id: string;
  file_name: string;
  mime_type?: string | null;
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
  const url = new URL(path, `${baseUrl()}/`);
  url.searchParams.set("key", apiKey());

  const response = await fetch(url, { ...options, headers });
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
        console.log(`- ${asset.id} ${asset.file_name} ${asset.public_url ?? asset.storage_key}`);
      }
    }
    console.log("");
  }
}

function outputFilePath(outputDir: string, asset: CliAsset): string {
  return join(outputDir, `${asset.id}-${asset.file_name}`);
}

async function downloadAsset(asset: CliAsset, outputPath: string) {
  const data = await request(`/api/assets/${encodeURIComponent(asset.id)}/download-url`);
  const response = await fetch(data.download_url);

  if (!response.ok) {
    throw new Error(`Direct R2 download failed with HTTP ${response.status}`);
  }

  await mkdir(dirname(outputPath), { recursive: true });
  await writeFile(outputPath, new Uint8Array(await response.arrayBuffer()));
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
  .command("download")
  .description("Download all attachments from a thread to a local directory.")
  .argument("<thread-id>", "thread id")
  .option("-o, --output <dir>", "directory to save files into")
  .option("--json", "print raw JSON")
  .action(async (threadId, options) => {
    const data = await request(`/api/threads/${encodeURIComponent(threadId)}`);
    const outputDir = options.output ?? join("agentbox-downloads", threadId);
    const downloads = [];

    for (const message of data.thread.messages ?? []) {
      for (const asset of message.assets ?? []) {
        const outputPath = outputFilePath(outputDir, asset);
        await downloadAsset(asset, outputPath);
        downloads.push({
          message_id: message.id,
          asset_id: asset.id,
          file_name: asset.file_name,
          storage_key: asset.storage_key,
          output_path: outputPath
        });
      }
    }

    const result = { thread_id: threadId, output_dir: outputDir, downloads };

    if (options.json) return printJson(result);
    if (downloads.length === 0) {
      console.log(`No attachments found for ${threadId}.`);
      return;
    }

    console.log(`Saved ${downloads.length} attachment${downloads.length === 1 ? "" : "s"} to ${outputDir}`);
    for (const download of downloads) {
      console.log(`- ${download.file_name} -> ${download.output_path}`);
    }
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
