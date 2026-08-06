import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { once } from "node:events";

let backend;
let fixture;
let route;
let temporaryDirectory;

async function readFixture(stream) {
  const lines = createInterface({ input: stream, crlfDelay: Infinity });
  for await (const line of lines) {
    if (line.trim()) return JSON.parse(line);
  }
  throw new Error("Visibility backend exited before publishing its fixture.");
}

function request(method, body, threadID = fixture.thread_id, extraHeaders = {}) {
  return new Request(`https://dashboard.example/api/threads/${encodeURIComponent(threadID)}/visibility?key=${encodeURIComponent(fixture.api_key)}`, {
    method,
    headers: {
      ...(body === undefined ? {} : { "content-type": "application/json" }),
      ...extraHeaders
    },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
}

async function patch(body, threadID) {
  return route.PATCH(request("PATCH", body, threadID), {
    params: Promise.resolve({ threadId: threadID ?? fixture.thread_id })
  });
}

beforeAll(async () => {
  temporaryDirectory = mkdtempSync(join(tmpdir(), "agentbox-visibility-proxy-"));
  const executable = join(temporaryDirectory, "visibility-backend");
  execFileSync("go", ["build", "-o", executable, "./internal/contracttest/visibilitybackend"], {
    cwd: process.cwd(),
    stdio: "inherit"
  });
  backend = spawn(executable, [], { cwd: process.cwd(), stdio: ["ignore", "pipe", "inherit"] });
  fixture = await readFixture(backend.stdout);
  process.env.AGENTBOX_BACKEND_URL = fixture.backend_url;
  route = await import("../app/api/threads/[threadId]/visibility/route.ts");
}, 60_000);

afterAll(async () => {
  if (backend && backend.exitCode === null) {
    backend.kill("SIGTERM");
    await once(backend, "close");
  }
  if (temporaryDirectory) rmSync(temporaryDirectory, { recursive: true, force: true });
}, 10_000);

describe("dashboard-origin visibility proxy", () => {
  test("exports GET and PATCH, but no obsolete PUT", () => {
    expect(typeof route.GET).toBe("function");
    expect(typeof route.PATCH).toBe("function");
    expect(route.PUT).toBeUndefined();
  });

  test("overwrites client-supplied forwarding headers", async () => {
    const read = await route.GET(request("GET", undefined, fixture.thread_id, {
      forwarded: "host=evil.example;proto=http",
      "x-forwarded-host": "evil.example",
      "x-forwarded-proto": "http",
      "x-forwarded-port": "80"
    }), { params: Promise.resolve({ threadId: fixture.thread_id }) });
    expect(read.status).toBe(200);
  });

  test("PATCH reaches the canonical Go visibility handler", async () => {
    const publish = await patch({ add_teams: [fixture.team_a], public: true });
    expect(publish.status).toBe(200);
    const published = await publish.json();
    expect(published.visibility.shared_teams.map((team) => team.id)).toEqual([fixture.team_a]);
    expect(published.visibility.public).toBe(true);
    expect(published.visibility.public_url).toStartWith("https://dashboard.example/share/");
    const firstPublicURL = published.visibility.public_url;

    const replaceTeam = await patch({ add_teams: [fixture.team_b], remove_teams: [fixture.team_a] });
    expect(replaceTeam.status).toBe(200);
    const replaced = await replaceTeam.json();
    expect(replaced.visibility.shared_teams.map((team) => team.id)).toEqual([fixture.team_b]);
    expect(replaced.visibility.public_url).toBe(firstPublicURL);

    const regenerate = await patch({ regenerate_public_link: true });
    expect(regenerate.status).toBe(200);
    const regenerated = await regenerate.json();
    expect(regenerated.visibility.public).toBe(true);
    expect(regenerated.visibility.public_url).not.toBe(firstPublicURL);

    const makePrivate = await patch({ remove_teams: [fixture.team_b], public: false });
    expect(makePrivate.status).toBe(200);
    const privateState = await makePrivate.json();
    expect(privateState.visibility.shared_teams).toEqual([]);
    expect(privateState.visibility.public).toBe(false);
    expect(privateState.visibility.public_url).toBeUndefined();
  });

  test("normal Go errors propagate without a second contract", async () => {
    const unavailableTeam = await patch({ add_teams: ["team_not_available"] });
    expect(unavailableTeam.status).toBe(400);
    expect(await unavailableTeam.json()).toMatchObject({ code: "TEAM_NOT_AVAILABLE" });

    const missingThread = await patch({ public: true }, "thr_missing");
    expect(missingThread.status).toBe(404);
    expect(await missingThread.json()).toMatchObject({ code: "THREAD_NOT_FOUND" });

    const read = await route.GET(request("GET"), { params: Promise.resolve({ threadId: fixture.thread_id }) });
    expect(read.status).toBe(200);
    expect(await read.json()).toMatchObject({ visibility: { public: false, shared_teams: [] } });
  });
});
