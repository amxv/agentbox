import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import {
  AgentboxAPIError,
  AgentboxClient,
  assetAvailability,
  attributionLabel,
  normalizeAgentboxConfig,
  visibilityLabels,
  type PresignedUpload,
  type Thread,
  type ThreadVisibilitySummary,
} from "../src/api-client.ts";
import {
  mutationHasChanges,
  visibilityMutation,
  visibilityTeamOptions,
  wouldSelfRevoke,
} from "../src/visibility-model.ts";
import { disambiguateUploadFileNames } from "../src/upload-file-names.ts";

const timestamp = "2026-08-03T12:34:56Z";

const privateVisibility: ThreadVisibilitySummary = {
  owned_by_me: true,
  private: true,
  shared_with_me: false,
  shared_teams: [],
  matched_teams: [],
  public: false,
};

function threadFixture(id: string, title = id): Thread {
  return {
    id,
    owner_user_id: "usr_ashray",
    title,
    created_at: timestamp,
    updated_at: timestamp,
    created_by: "Raycast",
    created_by_user_id: "usr_ashray",
    created_by_key_id: "key_raycast",
    created_by_user_display_name: "Ashray",
    created_by_actor_name: "Raycast",
    message_count: 0,
    last_message_preview: "",
    visibility_summary: privateVisibility,
  };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

test("configuration and URL construction keep auth explicit and path-safe", () => {
  assert.deepEqual(
    normalizeAgentboxConfig({ baseUrl: " https://agentbox.example/root/?ignored=1#hash ", apiKey: " key " }),
    {
      baseUrl: "https://agentbox.example/root",
      apiKey: "key",
    },
  );
  assert.throws(() => normalizeAgentboxConfig({ baseUrl: "ftp://agentbox.example", apiKey: "key" }), /HTTP or HTTPS/);
  assert.throws(
    () => normalizeAgentboxConfig({ baseUrl: "https://user:pass@agentbox.example", apiKey: "key" }),
    /credentials/,
  );
  assert.throws(() => normalizeAgentboxConfig({ baseUrl: "https://agentbox.example", apiKey: " " }), /API key/);

  const client = new AgentboxClient({ baseUrl: "https://agentbox.example/root", apiKey: "raycast-secret" });
  const query = new URLSearchParams({ filter: "team", team: "platform", key: "wrong" });
  const url = client.url("/api/threads", { query });
  assert.equal(url.pathname, "/root/api/threads");
  assert.equal(url.searchParams.get("filter"), "team");
  assert.equal(url.searchParams.get("team"), "platform");
  assert.equal(url.searchParams.get("key"), "raycast-secret");
  assert.equal(client.url("api/health", { authenticated: false }).searchParams.has("key"), false);
  assert.equal(client.dashboardThreadUrl("thr one"), "https://agentbox.example/root/threads/thr%20one");
});

test("auth and caller-team DTOs contain user metadata without tenant assumptions", async () => {
  const requests: URL[] = [];
  const client = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async (input) => {
    const url = new URL(String(input));
    requests.push(url);
    if (url.pathname === "/api/auth/me") {
      return json({
        auth: {
          user_id: "usr_ashray",
          user_display_name: "Ashray",
          subject_type: "api_key",
          actor_id: "key_raycast",
          actor_name: "Raycast",
          key_id: "key_raycast",
          scopes: ["threads:read", "threads:write", "assets:read", "assets:write"],
        },
      });
    }
    if (url.pathname === "/api/me/teams") {
      return json({
        teams: [
          { id: "team_platform", slug: "platform", name: "Platform", created_at: timestamp, updated_at: timestamp },
        ],
      });
    }
    throw new Error(`unexpected URL ${url}`);
  });

  const auth = await client.authMe();
  assert.equal(auth.user_id, "usr_ashray");
  assert.equal(auth.actor_name, "Raycast");
  assert.deepEqual(auth.scopes, ["threads:read", "threads:write", "assets:read", "assets:write"]);
  assert.equal("tenant_id" in auth, false);
  assert.deepEqual(
    (await client.listTeams()).map((team) => team.slug),
    ["platform"],
  );
  assert.ok(requests.every((url) => url.searchParams.get("key") === "secret"));
});

test("all five filters and search traverse opaque continuation pages without duplicates", async () => {
  const requests: URL[] = [];
  const filters = ["all", "private", "shared", "team", "public"] as const;
  const client = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async (input) => {
    const url = new URL(String(input));
    requests.push(url);
    const cursor = url.searchParams.get("cursor");
    if (url.searchParams.has("query")) {
      const first = {
        ...threadFixture("thr_search_1", "Search one"),
        message_count: 2,
        last_message_preview: "needle one",
        matched_snippets: ["needle one"],
      };
      const second = {
        ...threadFixture("thr_search_2", "Search two"),
        message_count: 1,
        last_message_preview: "needle two",
        matched_snippets: ["needle two"],
      };
      return cursor
        ? json({ threads: [first, second], page: { limit: 2, has_more: false } })
        : json({ threads: [first], page: { limit: 2, has_more: true, next_cursor: "search-next" } });
    }
    return cursor
      ? json({ threads: [threadFixture("thr_2"), threadFixture("thr_1")], page: { limit: 2, has_more: false } })
      : json({ threads: [threadFixture("thr_1")], page: { limit: 2, has_more: true, next_cursor: "list-next" } });
  });

  for (const filter of filters) {
    await client.listThreadPage({ limit: 2, filter, team: filter === "team" ? "platform" : undefined });
  }
  const filterRequests = requests.slice(0, filters.length);
  assert.equal(filterRequests[0].searchParams.has("filter"), false);
  assert.equal(filterRequests[1].searchParams.get("filter"), "private");
  assert.equal(filterRequests[2].searchParams.get("filter"), "shared");
  assert.equal(filterRequests[3].searchParams.get("filter"), "team");
  assert.equal(filterRequests[3].searchParams.get("team"), "platform");
  assert.equal(filterRequests[4].searchParams.get("filter"), "public");
  await assert.rejects(() => client.listThreadPage({ filter: "team" }), /team filter/);

  const listed = await client.listAllThreads({ limit: 2, filter: "all" });
  assert.deepEqual(
    listed.map((thread) => thread.id),
    ["thr_1", "thr_2"],
  );
  assert.equal(listed[0].message_count, 0);
  assert.equal(listed[0].last_message_preview, "");
  const searched = await client.searchAllThreads({
    query: "needle",
    limit: 2,
    filter: "shared",
    createdBy: "Raycast",
    updatedAfter: timestamp,
  });
  assert.deepEqual(
    searched.map((thread) => thread.id),
    ["thr_search_1", "thr_search_2"],
  );
  const searchRequest = requests.find((url) => url.searchParams.get("query") === "needle");
  assert.equal(searchRequest?.searchParams.get("filter"), "shared");
  assert.equal(searchRequest?.searchParams.get("created_by"), "Raycast");
  assert.equal(searchRequest?.searchParams.get("updated_after"), timestamp);
});

test("detail decoding exposes signed URLs and tombstones but rejects persistence fields", async () => {
  const safeThread = {
    ...threadFixture("thr_detail", "Detail"),
    messages: [
      {
        id: "msg_1",
        thread_id: "thr_detail",
        author: "Raycast",
        body: "Attachments",
        body_content_type: "text/markdown",
        created_at: timestamp,
        assets: [
          {
            id: "asset_signed",
            message_id: "msg_1",
            file_name: "signed.png",
            filename: "signed.png",
            mime_type: "image/png",
            size_bytes: 10,
            download_url: "https://r2.example/signed",
            preview_url: "https://r2.example/preview",
            created_at: timestamp,
            created_by: "Raycast",
          },
          {
            id: "asset_purged",
            message_id: "msg_1",
            file_name: "purged.txt",
            filename: "purged.txt",
            mime_type: "text/plain",
            size_bytes: 4,
            created_at: timestamp,
            created_by: "Raycast",
            purged_at: timestamp,
          },
          {
            id: "asset_missing",
            message_id: "msg_1",
            file_name: "missing.txt",
            filename: "missing.txt",
            mime_type: "text/plain",
            size_bytes: 4,
            created_at: timestamp,
            created_by: "Raycast",
            unavailable: true,
            unavailable_reason: "Attachment unavailable",
          },
        ],
      },
    ],
  };
  const client = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async () =>
    json({ thread: safeThread }),
  );
  const thread = await client.getThread("thr_detail");
  assert.equal(thread.messages[0].assets[0].preview_url, "https://r2.example/preview");
  assert.deepEqual(assetAvailability(thread.messages[0].assets[1]), {
    available: false,
    label: "Attachment deleted by deployment owner",
  });
  assert.deepEqual(assetAvailability(thread.messages[0].assets[2]), {
    available: false,
    label: "Attachment unavailable",
  });

  const unsafeClient = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async () =>
    json({
      thread: {
        ...safeThread,
        messages: [
          {
            ...safeThread.messages[0],
            assets: [{ ...safeThread.messages[0].assets[0], storage_key: "internal/object" }],
          },
        ],
      },
    }),
  );
  await assert.rejects(() => unsafeClient.getThread("thr_detail"), /forbidden field storage_key/);
});

test("visibility reads and patches preserve team and public-link metadata", async () => {
  let patchBody: unknown;
  const visibility = {
    thread_id: "thr_visibility",
    owner_user_id: "usr_ashray",
    shared_teams: [
      { id: "team_platform", slug: "platform", name: "Platform", created_at: timestamp, updated_at: timestamp },
    ],
    available_teams: [
      { id: "team_design", slug: "design", name: "Design", created_at: timestamp, updated_at: timestamp },
    ],
    public: true,
    public_link: {
      thread_id: "thr_visibility",
      token_prefix: "agpub_123",
      created_by_user_id: "usr_ashray",
      created_at: timestamp,
      updated_at: timestamp,
    },
    public_url: "https://agentbox.example/public/agpub",
  };
  const client = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async (input, init) => {
    assert.equal(new URL(String(input)).pathname, "/api/threads/thr_visibility/visibility");
    if (init?.method === "PATCH") patchBody = JSON.parse(String(init.body));
    return json({ visibility });
  });
  assert.equal((await client.getThreadVisibility("thr_visibility")).public_url, visibility.public_url);
  const managed = await client.manageThreadVisibility("thr_visibility", {
    add_teams: ["team_design"],
    remove_teams: ["team_platform"],
    public: true,
    regenerate_public_link: true,
  });
  assert.deepEqual(patchBody, {
    add_teams: ["team_design"],
    remove_teams: ["team_platform"],
    public: true,
    regenerate_public_link: true,
  });
  assert.equal(managed.public_link?.token_prefix, "agpub_123");
});

test("visibility mutations calculate exact deltas and warn only for real self-revocation", () => {
  const platform = {
    id: "team_platform",
    slug: "platform",
    name: "Platform",
    created_at: timestamp,
    updated_at: timestamp,
  };
  const design = { id: "team_design", slug: "design", name: "Design", created_at: timestamp, updated_at: timestamp };
  const external = {
    id: "team_external",
    slug: "external",
    name: "External",
    created_at: timestamp,
    updated_at: timestamp,
  };
  const current = {
    thread_id: "thr_visibility",
    owner_user_id: "usr_owner",
    shared_teams: [external, platform],
    available_teams: [platform, design],
    public: true,
    public_url: "https://agentbox.example/public/agpub",
  };

  const mutation = visibilityMutation(current, {
    selectedTeamIDs: [platform.id, design.id, design.id],
    publicEnabled: false,
  });
  assert.deepEqual(mutation, {
    add_teams: [design.id],
    remove_teams: [external.id],
    public: false,
  });
  assert.equal(mutationHasChanges(mutation), true);
  assert.equal(mutationHasChanges({}), false);
  assert.equal(
    wouldSelfRevoke({ currentUserID: "usr_member", current, selectedTeamIDs: [external.id] }),
    true,
    "a public link and a team the caller does not belong to must not preserve authenticated access",
  );
  assert.equal(wouldSelfRevoke({ currentUserID: "usr_member", current, selectedTeamIDs: [platform.id] }), false);
  assert.equal(wouldSelfRevoke({ currentUserID: "usr_member", current, selectedTeamIDs: [design.id] }), false);
  assert.equal(wouldSelfRevoke({ currentUserID: "usr_owner", current, selectedTeamIDs: [] }), false);
  assert.deepEqual(
    visibilityTeamOptions(current).map((team) => team.id),
    [design.id, external.id, platform.id],
  );
});

test("create, post, and signed-download methods use the final envelopes", async () => {
  const calls: Array<{ url: URL; init?: RequestInit }> = [];
  const createdThread = threadFixture("thr_created", "Created in Raycast");
  const createdMessage = {
    id: "msg_created",
    thread_id: createdThread.id,
    author: "Raycast",
    body: "Initial body",
    body_content_type: "text/markdown",
    created_at: timestamp,
    assets: [],
    created_by_user_display_name: "Ashray",
    created_by_actor_name: "Raycast",
  };
  const replyMessage = { ...createdMessage, id: "msg_reply", body: "Reply body" };
  const client = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async (input, init) => {
    const url = new URL(String(input));
    calls.push({ url, init });
    if (url.pathname === "/api/threads" && init?.method === "POST") {
      return json({ thread: createdThread, message: createdMessage }, 201);
    }
    if (url.pathname === "/api/threads/thr_created/messages") {
      return json({ message: replyMessage }, 201);
    }
    if (url.pathname === "/api/assets/asset_1/download-url") {
      return json({
        asset_id: "asset_1",
        file_name: "report.md",
        mime_type: "text/markdown",
        size_bytes: 42,
        expires_in: 600,
        download_url: "https://r2.example/download",
      });
    }
    throw new Error(`unexpected URL ${url}`);
  });

  const created = await client.createThread({
    title: "Created in Raycast",
    initialMessage: "Initial body",
    bodyContentType: "text/markdown",
  });
  assert.equal(created.thread.created_by_actor_name, "Raycast");
  assert.equal(created.message?.body_content_type, "text/markdown");
  assert.deepEqual(JSON.parse(String(calls[0].init?.body)), {
    title: "Created in Raycast",
    initial_message: "Initial body",
    body_content_type: "text/markdown",
  });

  const reply = await client.postMessage({
    threadId: createdThread.id,
    body: "Reply body",
    bodyContentType: "text/plain",
    uploadedAssets: [{ upload_id: "upload_1" }],
  });
  assert.equal(reply.id, "msg_reply");
  assert.deepEqual(JSON.parse(String(calls[1].init?.body)), {
    body: "Reply body",
    body_content_type: "text/plain",
    uploaded_assets: [{ upload_id: "upload_1" }],
  });

  const download = await client.getAssetDownloadUrl("asset_1", 600);
  assert.equal(download.download_url, "https://r2.example/download");
  assert.equal(calls[2].url.searchParams.get("expires_in"), "600");
  assert.equal(calls[2].url.searchParams.get("key"), "secret");
});

test("batch upload validates response safety, metadata, ordering, and external PUT isolation", async () => {
  const files = [
    { file_name: "one.md", mime_type: "text/markdown", size_bytes: 11 },
    { file_name: "two.txt", mime_type: "text/plain", size_bytes: 22 },
  ];
  const puts: Array<{ url: string; init?: RequestInit }> = [];
  const uploads: PresignedUpload[] = files.map((file, index) => ({
    upload_id: `upload_${index + 1}`,
    file_name: file.file_name,
    mime_type: file.mime_type,
    size_bytes: file.size_bytes,
    upload_url: `https://uploads.example/${index + 1}`,
    expires_in: 900,
    required_headers: { "content-type": file.mime_type },
  }));
  const client = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async (input, init) => {
    const url = new URL(String(input));
    if (url.hostname === "uploads.example") {
      puts.push({ url: url.toString(), init });
      return new Response(null, { status: 200 });
    }
    assert.equal(url.pathname, "/api/threads/thr_upload/uploads");
    assert.equal(url.searchParams.get("key"), "secret");
    assert.deepEqual(JSON.parse(String(init?.body)), { files });
    return json({ uploads }, 201);
  });
  const prepared = await client.createUploadIntents("thr_upload", files);
  assert.deepEqual(
    prepared.map((upload) => upload.upload_id),
    ["upload_1", "upload_2"],
  );
  await client.uploadBytesToPresignedUrl(prepared[0], new Uint8Array([1, 2, 3]));
  assert.equal(puts[0].url, "https://uploads.example/1");
  assert.equal(new URL(puts[0].url).searchParams.has("key"), false);
  assert.deepEqual(puts[0].init?.headers, { "content-type": "text/markdown" });

  const reordered = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async () =>
    json({ uploads: [uploads[1], uploads[0]] }, 201),
  );
  await assert.rejects(() => reordered.createUploadIntents("thr_upload", files), /changed file metadata at index 0/);
  const leaked = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async () =>
    json({ uploads: [{ ...uploads[0], storage_key: "internal/object" }, uploads[1]] }, 201),
  );
  await assert.rejects(() => leaked.createUploadIntents("thr_upload", files), /forbidden field storage_key/);
});

test("duplicate attachment basenames are disambiguated without changing source order", () => {
  assert.deepEqual(
    disambiguateUploadFileNames([
      "/tmp/first/report.txt",
      "/tmp/second/report.txt",
      "/tmp/third/report (2).txt",
      "/tmp/fourth/report.txt",
      "/tmp/fifth/REPORT.TXT",
      "/tmp/sixth/archive",
      "/tmp/seventh/archive",
    ]),
    ["report.txt", "report (3).txt", "report (2).txt", "report (4).txt", "REPORT (5).TXT", "archive", "archive (2)"],
  );
  assert.deepEqual(disambiguateUploadFileNames([]), []);
});

test("coded errors and attribution/visibility fallbacks remain stable", async () => {
  const client = new AgentboxClient({ baseUrl: "https://agentbox.example", apiKey: "secret" }, async () =>
    json({ code: "SCOPE_REQUIRED", error: "threads:read scope is required." }, 403),
  );
  await assert.rejects(
    () => client.listThreadPage(),
    (error: unknown) =>
      error instanceof AgentboxAPIError &&
      error.status === 403 &&
      error.code === "SCOPE_REQUIRED" &&
      error.backendError === "threads:read scope is required.",
  );
  assert.equal(
    attributionLabel({ created_by_user_display_name: "Ashray", created_by_actor_name: "Raycast" }, "legacy"),
    "Ashray · Raycast",
  );
  assert.equal(attributionLabel({}, "legacy"), "legacy");
  assert.deepEqual(
    visibilityLabels({
      owned_by_me: false,
      private: false,
      shared_with_me: true,
      shared_teams: [{ id: "team", slug: "platform", name: "Platform" }],
      matched_teams: [],
      public: true,
    }),
    ["Platform", "Public"],
  );
});

test("Raycast source cannot regain tenant, persistence, attachment-public-URL, or MCP assumptions", async () => {
  const testDirectory = path.dirname(fileURLToPath(import.meta.url));
  const sourceDirectory = path.join(testDirectory, "..", "src");
  const sourceFiles = (await readdir(sourceDirectory)).filter((file) => file.endsWith(".ts") || file.endsWith(".tsx"));
  for (const file of sourceFiles) {
    const source = await readFile(path.join(sourceDirectory, file), "utf8");
    assert.doesNotMatch(source, /tenant_(?:id|slug)/, `${file} contains tenant-era identity fields`);
    assert.doesNotMatch(source, /\.storage_key\b/, `${file} reads persistence storage keys`);
    assert.doesNotMatch(source, /asset\.public_url\b/, `${file} reads direct attachment public URLs`);
    assert.doesNotMatch(source, /\bmcpUrl\b|\/api\/mcp|Copy MCP URL/, `${file} reuses an MCP credential or endpoint`);
  }

  assert.equal(sourceFiles.includes("latest-messages.tsx"), false, "the unbounded Latest Messages fan-out returned");
  const browseSource = await readFile(path.join(sourceDirectory, "search-threads.tsx"), "utf8");
  assert.match(browseSource, /pagination=\{\{/);
  assert.match(browseSource, /<List\.Dropdown/);
  assert.match(browseSource, /listThreadPage/);
  assert.match(browseSource, /searchThreadPage/);
  assert.match(browseSource, /LocalStorage\.setItem\(INBOX_FILTER_STORAGE_KEY/);
  assert.match(browseSource, /requestId\.current/);

  const postSource = await readFile(path.join(sourceDirectory, "post-message.tsx"), "utf8");
  assert.match(postSource, /listThreadPage/);
  assert.match(postSource, /searchThreadPage/);
  assert.match(postSource, /pagination=\{\{/);
  assert.match(postSource, /Enter Thread ID Manually/);
  assert.match(postSource, /requestId\.current/);
  assert.doesNotMatch(postSource, /id="target"|target === "new"|createThread\(/);
  assert.match(postSource, /thread\.message_count/);
  assert.match(postSource, /thread\.last_message_preview/);
  assert.match(browseSource, /thread\.message_count/);
  assert.match(browseSource, /thread\.last_message_preview/);

  const formHelpersSource = await readFile(path.join(sourceDirectory, "form-helpers.ts"), "utf8");
  assert.match(formHelpersSource, /disambiguateUploadFileNames/);
  assert.match(formHelpersSource, /paths\.map\(async \(filePath, index\)/);

  const attachmentActionsSource = await readFile(path.join(sourceDirectory, "attachment-actions.tsx"), "utf8");
  assert.match(attachmentActionsSource, /assetAvailability\(asset\)/);
  assert.match(attachmentActionsSource, /availability\.available \?/);
  const markdownSource = await readFile(path.join(sourceDirectory, "markdown.ts"), "utf8");
  assert.match(markdownSource, /asset\.purged_at \|\| asset\.unavailable/);

  const manifest = JSON.parse(await readFile(path.join(testDirectory, "..", "package.json"), "utf8")) as {
    commands: Array<{ name: string; title: string }>;
  };
  assert.deepEqual(
    manifest.commands.map((command) => command.name),
    ["list-threads", "create-thread", "post-message", "doctor"],
  );
  assert.equal(manifest.commands.find((command) => command.name === "list-threads")?.title, "Browse Threads");
});
