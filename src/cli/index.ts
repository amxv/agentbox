import { basename, dirname, join } from "node:path";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { lookup } from "mime-types";
import { Command } from "commander";
import {
  defaultConfigPath,
  maskSecret,
  parseProfilesConfig,
  readProfileStore,
  removeProfile,
  resolveProfileSelection,
  sanitizeUrl,
  saveProfile,
  setActiveProfile
} from "../core/profiles";

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
type DoctorCheck = {
  name: string;
  status: "pass" | "fail" | "skip";
  detail?: string;
};

type RuntimeConfig = {
  profileName: string;
  baseUrl: string;
  apiKey: string;
  source: "config" | "env" | "legacy-env";
};

function commandProfileName(command?: Command): string | null {
  const options = command?.optsWithGlobals() as { profile?: string } | undefined;
  return options?.profile ?? null;
}

function isCommand(value: unknown): value is Command {
  return typeof value === "object"
    && value !== null
    && "optsWithGlobals" in value
    && typeof (value as { optsWithGlobals?: unknown }).optsWithGlobals === "function";
}

function commandJson(command?: Command | null): boolean {
  const options = command?.optsWithGlobals() as { json?: boolean } | undefined;
  return options?.json ?? false;
}

async function runtimeConfig(command?: Command): Promise<RuntimeConfig> {
  const profileName = commandProfileName(command);
  const profile = await resolveProfileSelection({ profileName: profileName ?? null });
  if (profile) {
    return {
      profileName: profile.name,
      baseUrl: profile.baseUrl,
      apiKey: profile.apiKey,
      source: profile.source
    };
  }

  const value = process.env.AGENTBOX_BASE_URL ?? process.env.AGENTBOX_URL;
  if (!value) throw new Error(`Set AGENTBOX_BASE_URL or configure profiles in ${defaultConfigPath()}.`);

  const apiKey = process.env.AGENTBOX_API_KEY;
  if (!apiKey) throw new Error(`Set AGENTBOX_API_KEY or configure profiles in ${defaultConfigPath()}.`);

  return {
    profileName: "default",
    baseUrl: value.replace(/\/$/, ""),
    apiKey,
    source: "legacy-env"
  };
}

async function endpoint(path: string, command?: Command): Promise<URL> {
  const config = await runtimeConfig(command);
  const url = new URL(path, `${config.baseUrl}/`);
  url.searchParams.set("key", config.apiKey);
  return url;
}


async function request(path: string, options: RequestOptions = {}, command?: Command) {
  const headers = new Headers(options.headers);
  const response = await fetch(await endpoint(path, command), { ...options, headers });
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

async function downloadAsset(asset: CliAsset, outputPath: string, command?: Command) {
  const data = await request(`/api/assets/${encodeURIComponent(asset.id)}/download-url`, {}, command);
  const response = await fetch(data.download_url);

  if (!response.ok) {
    throw new Error(`Direct R2 download failed with HTTP ${response.status}`);
  }

  await mkdir(dirname(outputPath), { recursive: true });
  await writeFile(outputPath, new Uint8Array(await response.arrayBuffer()));
}


async function runDoctor(command?: Command): Promise<DoctorCheck[]> {
  const checks: DoctorCheck[] = [];

  const add = (check: DoctorCheck) => {
    checks.push(check);
  };

  try {
    const config = await runtimeConfig(command);
    add({ name: "profile", status: "pass", detail: `${config.profileName} (${config.source})` });
    add({ name: "base URL", status: "pass", detail: config.baseUrl });
  } catch (error) {
    add({ name: "profile", status: "fail", detail: error instanceof Error ? error.message : String(error) });
    add({ name: "base URL", status: "fail", detail: error instanceof Error ? error.message : String(error) });
  }

  try {
    const config = await runtimeConfig(command);
    add({ name: "API key", status: "pass", detail: `Profile ${config.profileName} includes key ${maskSecret(config.apiKey)}` });
  } catch (error) {
    add({ name: "API key", status: "fail", detail: error instanceof Error ? error.message : String(error) });
  }

  try {
    const config = await runtimeConfig(command);
    const response = await fetch(new URL("/api/health", `${config.baseUrl}/`));
    add({ name: "health endpoint", status: response.ok ? "pass" : "fail", detail: `HTTP ${response.status}` });
  } catch (error) {
    add({ name: "health endpoint", status: "fail", detail: error instanceof Error ? error.message : String(error) });
  }

  try {
    const data = await request("/api/threads?limit=10", {}, command);
    add({ name: "authenticated API", status: "pass", detail: `${data.threads?.length ?? 0} thread(s) visible` });

    let asset: CliAsset | null = null;
    for (const thread of data.threads ?? []) {
      const threadData = await request(`/api/threads/${encodeURIComponent(thread.id)}`, {}, command);
      for (const message of threadData.thread.messages ?? []) {
        asset = message.assets?.[0] ?? null;
        if (asset) break;
      }
      if (asset) break;
    }

    if (!asset) {
      add({ name: "signed download URL", status: "skip", detail: "No attachments found in recent threads" });
    } else {
      const signed = await request(`/api/assets/${encodeURIComponent(asset.id)}/download-url`, {}, command);
      add({ name: "signed download URL", status: signed.download_url ? "pass" : "fail", detail: asset.file_name });
    }
  } catch (error) {
    add({ name: "authenticated API", status: "fail", detail: error instanceof Error ? error.message : String(error) });
  }

  try {
    const url = await endpoint("/api/mcp", command);
    add({ name: "ChatGPT MCP URL", status: "pass", detail: sanitizeUrl(url) });
  } catch (error) {
    add({ name: "ChatGPT MCP URL", status: "fail", detail: error instanceof Error ? error.message : String(error) });
  }

  return checks;
}

function printDoctor(checks: DoctorCheck[]) {
  for (const check of checks) {
    const icon = check.status === "pass" ? "✓" : check.status === "skip" ? "-" : "✗";
    console.log(`${icon} ${check.name}${check.detail ? ` — ${check.detail}` : ""}`);
  }

  const failed = checks.filter((check) => check.status === "fail").length;
  if (failed > 0) {
    throw new Error(`${failed} check${failed === 1 ? "" : "s"} failed.`);
  }
}

const program = new Command();
program
  .name("agentbox")
  .description("CLI for Agentbox, a small threaded message relay for ChatGPT and local agents.")
  .version("0.1.0")
  .option("-p, --profile <name>", "use a named profile");

const profilesCommand = program
  .command("profiles")
  .description("Inspect and manage CLI profiles.");

profilesCommand
  .option("--json", "print raw JSON")
  .action(async (_options, command) => {
    if (!isCommand(command)) throw new Error("Unexpected command state.");
    const envProfiles = parseProfilesConfig(process.env.AGENTBOX_PROFILES);
    const store = await readProfileStore();
    const selectedName = commandProfileName(command) ?? process.env.AGENTBOX_PROFILE ?? null;
    const legacyBaseUrl = process.env.AGENTBOX_BASE_URL ?? process.env.AGENTBOX_URL;
    const resolved = await resolveProfileSelection({ profileName: selectedName });
    const source = envProfiles.length > 0
      ? "env"
      : store.profiles.length > 0
        ? "config"
        : legacyBaseUrl && process.env.AGENTBOX_API_KEY
          ? "legacy-env"
          : "none";
    const listedProfiles = source === "env"
      ? envProfiles.map((profile) => ({ ...profile, source: "env" as const }))
      : source === "config"
        ? store.profiles.map((profile) => ({ ...profile, source: "config" as const }))
        : source === "legacy-env"
          ? [{ name: "default", baseUrl: legacyBaseUrl!.replace(/\/$/, ""), apiKey: process.env.AGENTBOX_API_KEY!, source: "legacy-env" as const }]
          : [];
    const data = {
      source,
      config_path: defaultConfigPath(),
      active_profile: resolved?.name ?? null,
      stored_active_profile: store.activeProfileName,
      profiles: listedProfiles.map((profile) => ({
        name: profile.name,
        base_url: profile.baseUrl,
        source: profile.source
      }))
    };

    if (commandJson(command)) return printJson(data);

    if (listedProfiles.length === 0) {
      console.log(`No CLI profiles configured. Add one with "agentbox profiles add" or set AGENTBOX_BASE_URL and AGENTBOX_API_KEY.`);
      console.log(`Config path: ${data.config_path}`);
      return;
    }

    console.log(`Config path: ${data.config_path}`);
    console.log(`Source: ${data.source}`);
    for (const profile of data.profiles) {
      const prefix = profile.name === data.active_profile ? "*" : " ";
      console.log(`${prefix} ${profile.name}\t${profile.base_url}\t${profile.source}`);
    }
  });

profilesCommand
  .command("add")
  .description("Create or update a stored CLI profile.")
  .argument("<name>", "profile name")
  .requiredOption("--base-url <url>", "Agentbox deployment base URL")
  .requiredOption("--api-key <key>", "Agentbox API key")
  .option("--activate", "make this the active stored profile")
  .option("--json", "print raw JSON")
  .action(async (name, options, command) => {
    const store = await saveProfile({
      name,
      baseUrl: options.baseUrl,
      apiKey: options.apiKey
    }, { activate: options.activate });
    const result = {
      saved_profile: name,
      active_profile: store.activeProfileName,
      config_path: defaultConfigPath(),
      profiles: store.profiles.map((profile) => ({
        name: profile.name,
        base_url: profile.baseUrl
      }))
    };
    if (commandJson(command)) return printJson(result);
    console.log(`Saved profile "${name}" in ${result.config_path}.`);
    if (store.activeProfileName === name) console.log(`Active profile: ${name}`);
  });

profilesCommand
  .command("remove")
  .description("Delete a stored CLI profile.")
  .argument("<name>", "profile name")
  .option("--json", "print raw JSON")
  .action(async (name, _options, command) => {
    const store = await removeProfile(name);
    const result = {
      removed_profile: name,
      active_profile: store.activeProfileName,
      config_path: defaultConfigPath(),
      profiles: store.profiles.map((profile) => ({
        name: profile.name,
        base_url: profile.baseUrl
      }))
    };
    if (commandJson(command)) return printJson(result);
    console.log(`Removed profile "${name}".`);
    console.log(`Active profile: ${result.active_profile ?? "none"}`);
  });

profilesCommand
  .command("use")
  .description("Switch the active stored CLI profile.")
  .argument("<name>", "profile name")
  .option("--json", "print raw JSON")
  .action(async (name, _options, command) => {
    const store = await setActiveProfile(name);
    const result = {
      active_profile: store.activeProfileName,
      config_path: defaultConfigPath()
    };
    if (commandJson(command)) return printJson(result);
    console.log(`Active profile: ${result.active_profile}`);
  });

profilesCommand
  .command("show")
  .description("Show the resolved profile for this invocation.")
  .argument("[name]", "profile name to inspect")
  .option("--json", "print raw JSON")
  .action(async (name, _options, command) => {
    const resolved = await resolveProfileSelection({ profileName: name ?? commandProfileName(command) });
    if (!resolved) {
      throw new Error(`No CLI profile resolved. Add one with "agentbox profiles add" or set AGENTBOX_BASE_URL and AGENTBOX_API_KEY.`);
    }
    const result = {
      name: resolved.name,
      base_url: resolved.baseUrl,
      api_key_masked: maskSecret(resolved.apiKey),
      source: resolved.source,
      config_path: defaultConfigPath()
    };
    if (commandJson(command)) return printJson(result);
    console.log(`${result.name}\t${result.base_url}\t${result.source}\t${result.api_key_masked}`);
  });

program
  .command("doctor")
  .description("Check local CLI configuration and the Agentbox deployment.")
  .option("--json", "print raw JSON")
  .action(async (_options, command) => {
    if (!isCommand(command)) throw new Error("Unexpected command state.");
    const checks = await runDoctor(command);
    if (commandJson(command)) return printJson({ checks });
    printDoctor(checks);
  });

program
  .command("list")
  .description("List recent threads.")
  .option("-n, --limit <number>", "maximum number of threads", "50")
  .option("--json", "print raw JSON")
  .action(async (options, command) => {
    const data = await request(`/api/threads?limit=${Number(options.limit)}`, {}, command);
    if (commandJson(command)) return printJson(data);

    for (const thread of data.threads) {
      console.log(`${thread.id}\t${thread.updated_at}\t${thread.title}`);
    }
  });

program
  .command("create")
  .description("Create a thread.")
  .argument("<title>", "thread title")
  .option("--json", "print raw JSON")
  .action(async (title, options, command) => {
    const data = await request("/api/threads", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ title })
    }, command);

    if (commandJson(command)) return printJson(data);
    console.log(`${data.thread.id}\t${data.thread.title}`);
  });

program
  .command("get")
  .description("Read a thread.")
  .argument("<thread-id>", "thread id")
  .option("--json", "print raw JSON")
  .action(async (threadId, options, command) => {
    const data = await request(`/api/threads/${encodeURIComponent(threadId)}`, {}, command);
    if (commandJson(command)) return printJson(data);
    printThread(data.thread);
  });

program
  .command("download")
  .description("Download all attachments from a thread to a local directory.")
  .argument("<thread-id>", "thread id")
  .option("-o, --output <dir>", "directory to save files into")
  .option("--json", "print raw JSON")
  .action(async (threadId, options, command) => {
    const data = await request(`/api/threads/${encodeURIComponent(threadId)}`, {}, command);
    const outputDir = options.output ?? join("agentbox-downloads", threadId);
    const downloads = [];

    for (const message of data.thread.messages ?? []) {
      for (const asset of message.assets ?? []) {
        const outputPath = outputFilePath(outputDir, asset);
        await downloadAsset(asset, outputPath, command);
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

    if (commandJson(command)) return printJson(result);
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
  .action(async (threadId, message, options, command) => {
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
      }, command);
    } else {
      data = await request(`/api/threads/${encodeURIComponent(threadId)}/messages`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ body })
      }, command);
    }

    if (commandJson(command)) return printJson(data);
    console.log(data.message.id);
  });

program.parseAsync().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
