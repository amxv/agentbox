import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, spawnSync } from "node:child_process";
import assert from "node:assert/strict";
import { closeDb } from "../src/core/db";

Object.assign(process.env, {
  NODE_ENV: "production",
  AGENTBOX_TEST_DB: "memory",
  AGENTBOX_TEST_FAKE_R2: "1",
  AGENTBOX_API_KEYS: "primary:secret:test-author",
  AGENTBOX_ADMIN_KEYS: "admin:admin-secret",
  R2_PUBLIC_BASE_URL: "https://public-r2.test"
});

type HandlerContext = { params: Promise<Record<string, string>> };
type Handler = (request: Request, context: HandlerContext) => Promise<Response> | Response;
type RouteMatch = {
  handler: Handler;
  params?: Record<string, string>;
};

async function createRouteMatcher(): Promise<(method: string, pathname: string) => RouteMatch | null> {
  const healthRoute = await import("../app/api/health/route");
  const threadsRoute = await import("../app/api/threads/route");
  const threadRoute = await import("../app/api/threads/[threadId]/route");
  const messagesRoute = await import("../app/api/threads/[threadId]/messages/route");
  const assetDownloadRoute = await import("../app/api/assets/[assetId]/download-url/route");
  const viewerThreadsRoute = await import("../app/api/viewer/threads/route");
  const viewerThreadRoute = await import("../app/api/viewer/threads/[threadId]/route");
  const mcpRoute = await import("../app/api/mcp/route");

  return (method: string, pathname: string): RouteMatch | null => {
  if (method === "GET" && pathname === "/api/health") return { handler: healthRoute.GET as Handler };
  if (pathname === "/api/threads") {
    if (method === "GET") return { handler: threadsRoute.GET as Handler };
    if (method === "POST") return { handler: threadsRoute.POST as Handler };
  }

  const threadMessageMatch = pathname.match(/^\/api\/threads\/([^/]+)\/messages$/);
  if (method === "POST" && threadMessageMatch?.[1]) {
    return { handler: messagesRoute.POST as Handler, params: { threadId: decodeURIComponent(threadMessageMatch[1]) } };
  }

  const threadMatch = pathname.match(/^\/api\/threads\/([^/]+)$/);
  if (method === "GET" && threadMatch?.[1]) {
    return { handler: threadRoute.GET as Handler, params: { threadId: decodeURIComponent(threadMatch[1]) } };
  }

  const assetMatch = pathname.match(/^\/api\/assets\/([^/]+)\/download-url$/);
  if (method === "GET" && assetMatch?.[1]) {
    return { handler: assetDownloadRoute.GET as Handler, params: { assetId: decodeURIComponent(assetMatch[1]) } };
  }

  if (method === "GET" && pathname === "/api/viewer/threads") return { handler: viewerThreadsRoute.GET as Handler };

  const viewerThreadMatch = pathname.match(/^\/api\/viewer\/threads\/([^/]+)$/);
  if (method === "GET" && viewerThreadMatch?.[1]) {
    return { handler: viewerThreadRoute.GET as Handler, params: { threadId: decodeURIComponent(viewerThreadMatch[1]) } };
  }

  if (pathname === "/api/mcp") {
    if (method === "GET") return { handler: mcpRoute.GET as Handler };
    if (method === "POST") return { handler: mcpRoute.POST as Handler };
    if (method === "DELETE") return { handler: mcpRoute.DELETE as Handler };
  }

  return null;
  };
}

async function writeWebResponse(source: Response, target: ServerResponse) {
  target.statusCode = source.status;
  source.headers.forEach((value, key) => target.setHeader(key, value));
  const body = source.body ? Buffer.from(await source.arrayBuffer()) : null;
  target.end(body);
}

function requestFromIncoming(req: IncomingMessage, baseUrl: string): Request {
  const headers = new Headers();
  for (const [key, value] of Object.entries(req.headers)) {
    if (Array.isArray(value)) {
      for (const part of value) headers.append(key, part);
    } else if (value !== undefined) {
      headers.set(key, value);
    }
  }

  const init: RequestInit & { duplex?: "half" } = {
    method: req.method,
    headers
  };
  if (req.method !== "GET" && req.method !== "HEAD") {
    init.body = req as unknown as BodyInit;
    init.duplex = "half";
  }
  return new Request(new URL(req.url ?? "/", baseUrl), init);
}

async function startServer(): Promise<{ baseUrl: string; close: () => Promise<void> }> {
  const route = await createRouteMatcher();
  const server = createServer(async (req, res) => {
    try {
      const baseUrl = `http://${req.headers.host}`;
      const url = new URL(req.url ?? "/", baseUrl);
      const match = route(req.method ?? "GET", url.pathname);
      if (!match) {
        res.statusCode = 404;
        res.end(JSON.stringify({ error: "Not found" }));
        return;
      }

      const request = requestFromIncoming(req, baseUrl);
      const response = await match.handler(request, { params: Promise.resolve(match.params ?? {}) });
      await writeWebResponse(response, res);
    } catch (error) {
      res.statusCode = 500;
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ error: error instanceof Error ? error.message : String(error) }));
    }
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert(address && typeof address === "object");
  return {
    baseUrl: `http://127.0.0.1:${address.port}`,
    close: () => new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
  };
}

async function reservePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert(address && typeof address === "object");
  const port = address.port;
  await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  return port;
}

async function startGoServer(): Promise<{ baseUrl: string; close: () => Promise<void> }> {
  console.error("[parity] reserving Go server port");
  const port = await reservePort();
  console.error(`[parity] starting Go server on ${port}`);
  const child = spawn("go", ["run", "./cmd/api"], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      PORT: String(port),
      AGENTBOX_ENV: "production",
      AGENTBOX_API_KEYS: "primary:secret:test-author",
      AGENTBOX_ADMIN_KEYS: "admin:admin-secret",
      R2_PUBLIC_BASE_URL: "https://public-r2.test"
    },
    detached: true,
    stdio: ["ignore", "pipe", "pipe"]
  });
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => { stdout += String(chunk); });
  child.stderr.on("data", (chunk) => { stderr += String(chunk); });
  const baseUrl = `http://127.0.0.1:${port}`;
  for (let attempt = 0; attempt < 120; attempt += 1) {
    if (child.exitCode !== null) {
      throw new Error(`Go server exited before health check. stdout=${stdout} stderr=${stderr}`);
    }
    try {
      const response = await fetch(new URL("/api/health", baseUrl));
      if (response.ok) return {
        baseUrl,
        close: async () => {
          if (child.exitCode !== null) return;
          try {
            process.kill(-child.pid, "SIGTERM");
          } catch {
            child.kill();
          }
          await new Promise<void>((resolve) => child.once("close", () => resolve()));
        }
      };
    } catch {
      // Server is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  child.kill();
  throw new Error(`Go server did not become healthy. stdout=${stdout} stderr=${stderr}`);
}

async function jsonFetch(baseUrl: string, path: string, init?: RequestInit) {
  const response = await fetch(new URL(path, baseUrl), {
    signal: AbortSignal.timeout(15_000),
    ...init
  });
  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  return { response, data };
}

async function multipartPost(baseUrl: string, path: string) {
  if (process.env.AGENTBOX_PARITY_GO_SERVER !== "1" && !process.env.AGENTBOX_PARITY_BASE_URL) {
    const form = new FormData();
    form.set("body", "Multipart message");
    form.set("asset", new File([new Uint8Array([1, 2, 3, 4])], "report one.txt", { type: "text/plain" }));
    return jsonFetch(baseUrl, path, { method: "POST", body: form });
  }

  const dir = await mkdtemp(join(tmpdir(), "agentbox-parity-upload-"));
  const file = join(dir, "upload.bin");
  try {
    await writeFile(file, new Uint8Array([1, 2, 3, 4]));
    const response = spawnSync("curl", [
      "-sS",
      "-w",
      "\n%{http_code}",
      "-X",
      "POST",
      String(new URL(path, baseUrl)),
      "-F",
      "body=Multipart message",
      "-F",
      `asset=@${file};filename=report one.txt;type=text/plain;charset=utf-8`
    ], { cwd: process.cwd(), encoding: "utf8" });
    assert.equal(response.status, 0, response.stderr);
    const splitAt = response.stdout.lastIndexOf("\n");
    assert(splitAt >= 0, response.stdout);
    const text = response.stdout.slice(0, splitAt);
    const status = Number(response.stdout.slice(splitAt + 1));
    return {
      response: { status },
      data: text ? JSON.parse(text) : null
    };
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

function assertId(value: unknown, prefix: string) {
  assert.equal(typeof value, "string");
  assert.match(value as string, new RegExp(`^${prefix}_[0-9a-f-]{36}$`));
}

function assertIso(value: unknown) {
  assert.equal(typeof value, "string");
  assert.doesNotThrow(() => new Date(value as string).toISOString());
}

function assertThreadShape(thread: Record<string, unknown>) {
  assertId(thread.id, "thr");
  assert.equal(typeof thread.title, "string");
  assertIso(thread.created_at);
  assertIso(thread.updated_at);
  assert.equal(typeof thread.created_by, "string");
}

function assertMessageShape(message: Record<string, unknown>) {
  assertId(message.id, "msg");
  assertId(message.thread_id, "thr");
  assert.equal(typeof message.author, "string");
  assert.equal(typeof message.body, "string");
  assertIso(message.created_at);
  assert(Array.isArray(message.assets));
}

function assertAssetShape(asset: Record<string, unknown>) {
  assertId(asset.id, "asset");
  assertId(asset.message_id, "msg");
  assert.equal(typeof asset.storage_key, "string");
  assert.equal(typeof asset.file_name, "string");
  assert.equal(typeof asset.size_bytes, "number");
  assertIso(asset.created_at);
  assert.equal(typeof asset.created_by, "string");
}

function parseMcpPayload(text: string) {
  if (!text.startsWith("event:")) return JSON.parse(text);
  const dataLine = text.split("\n").find((line) => line.startsWith("data: "));
  assert(dataLine, "SSE MCP response should include a data line");
  return JSON.parse(dataLine.slice("data: ".length));
}

async function mcpCall(baseUrl: string, method: string, params: Record<string, unknown>, id: number) {
  const response = await fetch(new URL("/api/mcp?key=secret", baseUrl), {
    method: "POST",
    headers: {
      "accept": "application/json, text/event-stream",
      "content-type": "application/json"
    },
    body: JSON.stringify({ jsonrpc: "2.0", id, method, params })
  });
  const text = await response.text();
  assert.equal(response.status, 200, text);
  return parseMcpPayload(text);
}

async function assertError(baseUrl: string, path: string, status: number, error: string, init?: RequestInit) {
  const result = await jsonFetch(baseUrl, path, init);
  assert.equal(result.response.status, status);
  assert.deepEqual(result.data, { error });
}

function zodIssueError(issue: Record<string, unknown>): string {
  return JSON.stringify([issue], null, 2);
}

async function runCliProfileParity() {
  const configDir = await mkdtemp(join(tmpdir(), "agentbox-profiles-"));
  const env = { ...process.env, AGENTBOX_CONFIG_DIR: configDir };

  try {
    const add = spawnSync("bun", [
      "src/cli/index.ts",
      "profiles",
      "add",
      "local",
      "--base-url",
      "https://agentbox.example/",
      "--api-key",
      "profile-secret",
      "--activate",
      "--json"
    ], { cwd: process.cwd(), env, encoding: "utf8" });
    assert.equal(add.status, 0, add.stderr);
    assert.deepEqual(JSON.parse(add.stdout), {
      saved_profile: "local",
      active_profile: "local",
      config_path: join(configDir, "profiles.json"),
      profiles: [{ name: "local", base_url: "https://agentbox.example" }]
    });

    const stored = JSON.parse(await (await import("node:fs/promises")).readFile(join(configDir, "profiles.json"), "utf8"));
    assert.deepEqual(stored, {
      active_profile: "local",
      profiles: {
        local: {
          base_url: "https://agentbox.example",
          api_key: "profile-secret"
        }
      }
    });

    await writeFile(join(configDir, "profiles.json"), JSON.stringify({
      current_profile: "legacy",
      profiles: [
        { name: "legacy", baseUrl: "https://legacy.example/", apiKey: "legacy-secret" }
      ]
    }));

    const show = spawnSync("bun", [
      "src/cli/index.ts",
      "profiles",
      "show",
      "--json"
    ], { cwd: process.cwd(), env, encoding: "utf8" });
    assert.equal(show.status, 0, show.stderr);
    assert.deepEqual(JSON.parse(show.stdout), {
      name: "legacy",
      base_url: "https://legacy.example",
      api_key_masked: "leg********et",
      source: "config",
      config_path: join(configDir, "profiles.json")
    });
  } finally {
    await rm(configDir, { recursive: true, force: true });
  }
}

async function runApiParity(baseUrl: string, options: { skipDynamicR2MissingCheck?: boolean } = {}) {
  const mark = (message: string) => {
    if (process.env.AGENTBOX_PARITY_GO_SERVER === "1") console.error(`[parity] api: ${message}`);
  };
  mark("health");
  const health = await jsonFetch(baseUrl, "/api/health");
  assert.equal(health.response.status, 200);
  assert.deepEqual(health.data, { ok: true, service: "agentbox" });

  mark("auth error");
  await assertError(baseUrl, "/api/threads?key=wrong", 401, "Unauthorized");
  mark("invalid title");
  await assertError(baseUrl, "/api/threads?key=secret", 400, zodIssueError({
    origin: "string",
    code: "too_small",
    minimum: 1,
    inclusive: true,
    path: ["title"],
    message: "Too small: expected string to have >=1 characters"
  }), {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ title: "" })
  });
  mark("missing thread");
  await assertError(baseUrl, "/api/threads/thr_missing?key=secret", 404, "Thread not found.");

  mark("create thread");
  const created = await jsonFetch(baseUrl, "/api/threads?key=secret", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ title: "Parity thread" })
  });
  assert.equal(created.response.status, 201);
  assertThreadShape(created.data.thread);
  assert.equal(created.data.thread.title, "Parity thread");
  assert.equal(created.data.thread.created_by, "test-author");
  const threadId = created.data.thread.id as string;

  mark("list threads");
  const listed = await jsonFetch(baseUrl, "/api/threads?key=secret&limit=10");
  assert.equal(listed.response.status, 200);
  assert(Array.isArray(listed.data.threads));
  assertThreadShape(listed.data.threads[0]);

  mark("post json message");
  const posted = await jsonFetch(baseUrl, `/api/threads/${encodeURIComponent(threadId)}/messages?key=secret`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ body: "JSON message" })
  });
  assert.equal(posted.response.status, 201);
  assertMessageShape(posted.data.message);
  assert.equal(posted.data.message.body, "JSON message");
  assert.deepEqual(posted.data.message.assets, []);

  mark("invalid file reference");
  await assertError(baseUrl, `/api/threads/${encodeURIComponent(threadId)}/messages?key=secret`, 400, zodIssueError({
    code: "invalid_format",
    format: "url",
    path: ["download_url"],
    message: "Invalid URL"
  }), {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ body: "bad file", file: { download_url: "not-a-url", file_id: "file_bad" } })
  });

  mark("post multipart message");
  const multipart = await multipartPost(baseUrl, `/api/threads/${encodeURIComponent(threadId)}/messages?key=secret`);
  assert.equal(multipart.response.status, 201);
  assertMessageShape(multipart.data.message);
  assert.equal(multipart.data.message.body, "Multipart message");
  assert.equal(multipart.data.message.assets.length, 1);
  assertAssetShape(multipart.data.message.assets[0]);
  assert.equal(multipart.data.message.assets[0].file_name, "report-one.txt");
  assert.equal(multipart.data.message.assets[0].mime_type, "text/plain;charset=utf-8");
  assert.equal(multipart.data.message.assets[0].size_bytes, 4);
  const assetId = multipart.data.message.assets[0].id as string;

  mark("get thread");
  const fetched = await jsonFetch(baseUrl, `/api/threads/${encodeURIComponent(threadId)}?key=secret`);
  assert.equal(fetched.response.status, 200);
  assertThreadShape(fetched.data.thread);
  assert.equal(fetched.data.thread.messages.length, 2);

  mark("asset download url");
  const download = await jsonFetch(baseUrl, `/api/assets/${encodeURIComponent(assetId)}/download-url?key=secret&expires_in=1`);
  assert.equal(download.response.status, 200);
  assert.deepEqual(Object.keys(download.data).sort(), [
    "asset_id",
    "download_url",
    "expires_in",
    "file_name",
    "mime_type",
    "size_bytes"
  ]);
  assert.equal(download.data.asset_id, assetId);
  assert.equal(download.data.expires_in, 60);
  assert.match(download.data.download_url, /^https:\/\/r2\.test\/agentbox\//);

  mark("viewer list");
  const viewerList = await jsonFetch(baseUrl, "/api/viewer/threads?limit=500", {
    headers: { "x-agentbox-admin-key": "admin-secret" }
  });
  assert.equal(viewerList.response.status, 200);
  assert.equal(viewerList.data.threads.length, 1);

  mark("viewer get");
  const viewerGet = await jsonFetch(baseUrl, `/api/viewer/threads/${encodeURIComponent(threadId)}`, {
    headers: { authorization: "Bearer admin-secret" }
  });
  assert.equal(viewerGet.response.status, 200);
  assert.equal(viewerGet.data.thread.messages[1].assets[0].download_url.includes("X-Amz-Expires=300"), true);
  assert.equal(viewerGet.data.thread.messages[1].assets[0].preview_url, null);

  if (!options.skipDynamicR2MissingCheck) {
    mark("missing R2 error");
    process.env.AGENTBOX_TEST_FAKE_R2 = "0";
    delete process.env.R2_BUCKET;
    await assertError(baseUrl, `/api/assets/${encodeURIComponent(assetId)}/download-url?key=secret`, 500, "R2_BUCKET is required for asset downloads.");
    process.env.AGENTBOX_TEST_FAKE_R2 = "1";
  }

  return threadId;
}

async function runMcpParity(baseUrl: string) {
  const initialize = await mcpCall(baseUrl, "initialize", {
    protocolVersion: "2025-06-18",
    capabilities: {},
    clientInfo: { name: "parity", version: "0.0.0" }
  }, 1);
  assert.equal(initialize.result.serverInfo.name, "agentbox");
  assert.equal(initialize.result.serverInfo.version, "0.1.0");

  const tools = await mcpCall(baseUrl, "tools/list", {}, 2);
  assert.deepEqual(tools.result.tools.map((tool: { name: string }) => tool.name).sort(), [
    "create_thread",
    "get_thread",
    "list_threads",
    "post_message"
  ]);

  const created = await mcpCall(baseUrl, "tools/call", {
    name: "create_thread",
    arguments: { title: "MCP parity thread" }
  }, 3);
  assert.equal(created.result.content[0].text, "Created Agentbox thread.");
  assertThreadShape(created.result.structuredContent.thread);
  const threadId = created.result.structuredContent.thread.id as string;

  const listed = await mcpCall(baseUrl, "tools/call", {
    name: "list_threads",
    arguments: { limit: 10 }
  }, 4);
  assert.equal(listed.result.content[0].text, "Listed Agentbox threads.");
  assert(Array.isArray(listed.result.structuredContent.threads));

  const fetched = await mcpCall(baseUrl, "tools/call", {
    name: "get_thread",
    arguments: { thread_id: threadId }
  }, 5);
  assert.equal(fetched.result.content[0].text, "Fetched Agentbox thread.");
  assertThreadShape(fetched.result.structuredContent.thread);

  const posted = await mcpCall(baseUrl, "tools/call", {
    name: "post_message",
    arguments: { thread_id: threadId, body: "MCP message" }
  }, 6);
  assert.equal(posted.result.content[0].text, "Posted message to Agentbox.");
  assertMessageShape(posted.result.structuredContent.message);
  assert.equal(posted.result.structuredContent.message.body, "MCP message");
}

const externalBaseUrl = process.env.AGENTBOX_PARITY_BASE_URL;
if (process.env.AGENTBOX_PARITY_GO_SERVER === "1") {
  const server = await startGoServer();
  try {
    console.error(`[parity] running API checks against ${server.baseUrl}`);
    await runApiParity(server.baseUrl, { skipDynamicR2MissingCheck: true });
    console.error("[parity] running MCP checks");
    await runMcpParity(server.baseUrl);
    console.error("[parity] running CLI profile checks");
    await runCliProfileParity();
    console.log(`Parity workflow passed against Go server ${server.baseUrl}`);
  } finally {
    await server.close();
    await closeDb();
  }
} else if (externalBaseUrl) {
  await runApiParity(externalBaseUrl, { skipDynamicR2MissingCheck: true });
  await runMcpParity(externalBaseUrl);
  await runCliProfileParity();
  console.log(`Parity workflow passed against external server ${externalBaseUrl}`);
  await closeDb();
} else {
  const server = await startServer();
  try {
    await runApiParity(server.baseUrl);
    await runMcpParity(server.baseUrl);
    await runCliProfileParity();
    console.log(`Current TypeScript parity workflow passed against ${server.baseUrl}`);
  } finally {
    await server.close();
    await closeDb();
  }
}
