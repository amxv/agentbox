export type BodyContentType = "auto" | "text/markdown" | "text/plain";

export type ThreadFilter = "all" | "private" | "shared" | "team" | "public";

export type AgentboxClientConfig = {
  baseUrl: string;
  apiKey: string;
};

export type BackendErrorPayload = {
  error?: string;
  code?: string;
};

export class AgentboxAPIError extends Error {
  readonly status: number;
  readonly code?: string;
  readonly backendError?: string;
  readonly payload?: unknown;

  constructor(status: number, payload?: BackendErrorPayload, fallback = `Request failed with HTTP ${status}`) {
    const message = payload?.error ? (payload.code ? `${payload.code}: ${payload.error}` : payload.error) : fallback;
    super(message);
    this.name = "AgentboxAPIError";
    this.status = status;
    this.code = payload?.code;
    this.backendError = payload?.error;
    this.payload = payload;
  }
}

export type AuthContext = {
  user_id: string;
  user_display_name?: string;
  subject_type: "user_session" | "api_key" | string;
  actor_id?: string;
  actor_name: string;
  key_id?: string;
  session_id?: string;
  scopes?: string[];
  is_owner?: boolean;
};

export type Team = {
  id: string;
  slug: string;
  name: string;
  created_at: string;
  updated_at: string;
};

export type AttributionSnapshot = {
  created_by_user_display_name?: string;
  created_by_actor_name?: string;
};

export type ThreadTeamSummary = {
  id: string;
  slug: string;
  name: string;
};

export type ThreadVisibilitySummary = {
  owned_by_me: boolean;
  private: boolean;
  shared_with_me: boolean;
  shared_teams: ThreadTeamSummary[];
  matched_teams: ThreadTeamSummary[];
  public: boolean;
};

export type Thread = AttributionSnapshot & {
  id: string;
  owner_user_id: string;
  title: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  created_by_user_id?: string;
  created_by_key_id?: string;
  message_count?: number;
  last_message_preview?: string;
  visibility_summary: ThreadVisibilitySummary;
};

export type Asset = AttributionSnapshot & {
  id: string;
  message_id: string;
  file_name: string;
  filename: string;
  mime_type: string | null;
  size_bytes: number;
  download_url?: string;
  preview_url?: string;
  created_at: string;
  created_by: string;
  created_by_user_id?: string;
  created_by_key_id?: string;
  purged_at?: string;
  unavailable?: boolean;
  unavailable_reason?: string;
};

export type Message = AttributionSnapshot & {
  id: string;
  thread_id: string;
  author: string;
  body: string;
  body_content_type: string | null;
  created_at: string;
  assets: Asset[];
  created_by_user_id?: string;
  created_by_key_id?: string;
};

export type ThreadWithMessages = Thread & {
  messages: Message[];
};

export type SearchThreadResult = AttributionSnapshot & {
  id: string;
  owner_user_id: string;
  title: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  message_count: number;
  last_message_preview: string;
  matched_snippets: string[];
  visibility_summary: ThreadVisibilitySummary;
};

export type ThreadPageInfo = {
  limit: number;
  has_more: boolean;
  next_cursor?: string;
};

export type ThreadPage<T extends Thread | SearchThreadResult> = {
  threads: T[];
  page: ThreadPageInfo;
};

export type ThreadPageParams = {
  limit?: number;
  cursor?: string;
  filter?: ThreadFilter;
  team?: string;
};

export type SearchThreadsParams = ThreadPageParams & {
  query: string;
  createdBy?: string;
  updatedAfter?: string;
};

export type UploadIntentFile = {
  file_name: string;
  mime_type?: string | null;
  size_bytes: number;
};

export type PresignedUpload = {
  upload_id: string;
  file_name: string;
  mime_type: string | null;
  size_bytes: number;
  upload_url: string;
  expires_in: number;
  required_headers: Record<string, string>;
};

export type UploadedAssetReference = {
  upload_id: string;
};

export type AssetDownloadURL = {
  asset_id: string;
  file_name: string;
  mime_type: string | null;
  size_bytes: number;
  expires_in: number;
  download_url: string;
};

export type ThreadPublicLink = {
  thread_id: string;
  token_prefix: string;
  created_by_user_id?: string;
  created_at: string;
  updated_at: string;
  revoked_at?: string;
};

export type ManagedThreadVisibility = {
  thread_id: string;
  owner_user_id: string;
  shared_teams: Team[];
  available_teams: Team[];
  public: boolean;
  public_link?: ThreadPublicLink;
  public_url?: string;
};

export type ManageThreadVisibilityInput = {
  add_teams?: string[];
  remove_teams?: string[];
  public?: boolean;
  regenerate_public_link?: boolean;
};

export type HealthResponse = {
  ok: boolean;
  service: string;
};

export type CreateThreadInput = {
  title: string;
  initialMessage?: string;
  bodyContentType?: BodyContentType | string;
};

export type CreateThreadResponse = {
  thread: Thread;
  message?: Message;
};

export type PostMessageInput = {
  threadId: string;
  body: string;
  bodyContentType?: BodyContentType | string;
  uploadedAssets?: UploadedAssetReference[];
};

type FetchLike = (input: string | URL | Request, init?: RequestInit) => Promise<Response>;

type AgentboxRequestInit = RequestInit & {
  authenticated?: boolean;
  query?: URLSearchParams;
};

type AuthMeResponse = { auth: AuthContext };
type TeamsResponse = { teams: Team[] };
type ThreadResponse = { thread: ThreadWithMessages };
type MessageResponse = { message: Message };
type UploadsResponse = { uploads: unknown[] };
type VisibilityResponse = { visibility: ManagedThreadVisibility };

export class AgentboxClient {
  readonly config: AgentboxClientConfig;
  private readonly fetcher: FetchLike;

  constructor(config: AgentboxClientConfig, fetcher: FetchLike = fetch) {
    this.config = normalizeAgentboxConfig(config);
    this.fetcher = fetcher;
  }

  url(requestPath: string, options: { authenticated?: boolean; query?: URLSearchParams } = {}): URL {
    const authenticated = options.authenticated ?? true;
    const url = new URL(trimLeadingSlashes(requestPath), ensureTrailingSlash(this.config.baseUrl));
    options.query?.forEach((value, key) => url.searchParams.append(key, value));
    if (authenticated) url.searchParams.set("key", this.config.apiKey);
    return url;
  }

  dashboardThreadUrl(threadId: string): string {
    return new URL(
      `threads/${encodeURIComponent(nonEmpty(threadId, "thread ID"))}`,
      ensureTrailingSlash(this.config.baseUrl),
    ).toString();
  }

  async request<T>(requestPath: string, init: AgentboxRequestInit = {}): Promise<T> {
    const { authenticated = true, query, ...requestInit } = init;
    const response = await this.fetcher(this.url(requestPath, { authenticated, query }), requestInit);
    return parseResponse<T>(response);
  }

  health(): Promise<HealthResponse> {
    return this.request<HealthResponse>("api/health", { authenticated: false });
  }

  async authMe(): Promise<AuthContext> {
    const data = await this.request<AuthMeResponse>("api/auth/me");
    return decodeAuthContext(data.auth);
  }

  async listTeams(): Promise<Team[]> {
    const data = await this.request<TeamsResponse>("api/me/teams");
    return expectArray(data.teams, "teams").map((team, index) => decodeTeam(team, `teams[${index}]`));
  }

  async listThreadPage(params: ThreadPageParams = {}): Promise<ThreadPage<Thread>> {
    const query = threadPageQuery(params);
    const data = await this.request<unknown>("api/threads", { query });
    return decodeThreadPage(data, decodeThread);
  }

  async searchThreadPage(params: SearchThreadsParams): Promise<ThreadPage<SearchThreadResult>> {
    const queryText = nonEmpty(params.query, "search query");
    const query = threadPageQuery(params);
    query.set("query", queryText);
    if (params.createdBy?.trim()) query.set("created_by", params.createdBy.trim());
    if (params.updatedAfter?.trim()) query.set("updated_after", params.updatedAfter.trim());
    const data = await this.request<unknown>("api/threads", { query });
    return decodeThreadPage(data, decodeSearchThreadResult);
  }

  async listAllThreads(params: Omit<ThreadPageParams, "cursor"> = {}, maxPages = 100): Promise<Thread[]> {
    return collectThreadPages((cursor) => this.listThreadPage({ ...params, cursor }), maxPages);
  }

  async searchAllThreads(params: Omit<SearchThreadsParams, "cursor">, maxPages = 100): Promise<SearchThreadResult[]> {
    return collectThreadPages((cursor) => this.searchThreadPage({ ...params, cursor }), maxPages);
  }

  async getThread(threadId: string): Promise<ThreadWithMessages> {
    const data = await this.request<ThreadResponse>(
      `api/threads/${encodeURIComponent(nonEmpty(threadId, "thread ID"))}/view`,
    );
    return decodeThreadWithMessages(data.thread);
  }

  async getThreadVisibility(threadId: string): Promise<ManagedThreadVisibility> {
    const data = await this.request<VisibilityResponse>(
      `api/threads/${encodeURIComponent(nonEmpty(threadId, "thread ID"))}/visibility`,
    );
    return decodeManagedThreadVisibility(data.visibility);
  }

  async manageThreadVisibility(threadId: string, input: ManageThreadVisibilityInput): Promise<ManagedThreadVisibility> {
    const data = await this.request<VisibilityResponse>(
      `api/threads/${encodeURIComponent(nonEmpty(threadId, "thread ID"))}/visibility`,
      { method: "PATCH", headers: jsonHeaders(), body: JSON.stringify(input) },
    );
    return decodeManagedThreadVisibility(data.visibility);
  }

  async createThread(input: CreateThreadInput): Promise<CreateThreadResponse> {
    const body: Record<string, string> = { title: nonEmpty(input.title, "thread title") };
    if (input.initialMessage !== undefined) body.initial_message = input.initialMessage;
    if (input.bodyContentType !== undefined) body.body_content_type = input.bodyContentType;
    const data = await this.request<unknown>("api/threads", {
      method: "POST",
      headers: jsonHeaders(),
      body: JSON.stringify(body),
    });
    const record = expectRecord(data, "response");
    const result: CreateThreadResponse = { thread: decodeThread(record.thread) };
    if (record.message !== undefined) result.message = decodeMessage(record.message);
    return result;
  }

  async postMessage(input: PostMessageInput): Promise<Message> {
    const body: {
      body: string;
      body_content_type?: string;
      uploaded_assets?: UploadedAssetReference[];
    } = { body: input.body };
    if (input.bodyContentType !== undefined) body.body_content_type = input.bodyContentType;
    if (input.uploadedAssets !== undefined) body.uploaded_assets = input.uploadedAssets;
    const data = await this.request<MessageResponse>(
      `api/threads/${encodeURIComponent(nonEmpty(input.threadId, "thread ID"))}/messages`,
      { method: "POST", headers: jsonHeaders(), body: JSON.stringify(body) },
    );
    return decodeMessage(data.message);
  }

  async createUploadIntents(threadId: string, files: UploadIntentFile[]): Promise<PresignedUpload[]> {
    const data = await this.request<UploadsResponse>(
      `api/threads/${encodeURIComponent(nonEmpty(threadId, "thread ID"))}/uploads`,
      { method: "POST", headers: jsonHeaders(), body: JSON.stringify({ files }) },
    );
    const uploads = expectArray(data.uploads, "uploads").map((upload, index) => decodePresignedUpload(upload, index));
    if (uploads.length !== files.length) {
      throw new Error(`Upload preparation returned ${uploads.length} items for ${files.length} files.`);
    }
    uploads.forEach((upload, index) => {
      const requested = files[index];
      if (
        upload.file_name !== requested.file_name ||
        upload.size_bytes !== requested.size_bytes ||
        (upload.mime_type ?? null) !== (requested.mime_type ?? null)
      ) {
        throw new Error(`Upload preparation changed file metadata at index ${index}.`);
      }
    });
    return uploads;
  }

  async uploadBytesToPresignedUrl(
    upload: PresignedUpload,
    bytes: Exclude<RequestInit["body"], null | undefined>,
  ): Promise<void> {
    const response = await this.fetcher(upload.upload_url, {
      method: "PUT",
      headers: upload.required_headers,
      body: bytes,
    });
    if (!response.ok) throw await responseError(response);
  }

  async getAssetDownloadUrl(assetId: string, expiresIn?: number): Promise<AssetDownloadURL> {
    const query = new URLSearchParams();
    if (expiresIn !== undefined) query.set("expires_in", String(expiresIn));
    const data = await this.request<unknown>(
      `api/assets/${encodeURIComponent(nonEmpty(assetId, "asset ID"))}/download-url`,
      {
        query,
      },
    );
    const record = expectRecord(data, "download");
    rejectForbiddenKeys(record, "download", ["storage_key", "public_url"]);
    return {
      asset_id: expectString(record.asset_id, "download.asset_id"),
      file_name: expectString(record.file_name, "download.file_name"),
      mime_type: nullableString(record.mime_type, "download.mime_type"),
      size_bytes: expectNumber(record.size_bytes, "download.size_bytes"),
      expires_in: expectNumber(record.expires_in, "download.expires_in"),
      download_url: expectString(record.download_url, "download.download_url"),
    };
  }
}

export function normalizeAgentboxConfig(config: AgentboxClientConfig): AgentboxClientConfig {
  const baseUrl = config.baseUrl.trim();
  const apiKey = config.apiKey.trim();
  if (!baseUrl) throw new Error("Agentbox URL is required.");
  if (!apiKey) throw new Error("Agentbox API key is required.");
  let parsed: URL;
  try {
    parsed = new URL(baseUrl);
  } catch {
    throw new Error("Agentbox URL must be a valid HTTP or HTTPS URL.");
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("Agentbox URL must use HTTP or HTTPS.");
  }
  if (parsed.username || parsed.password) throw new Error("Agentbox URL must not contain credentials.");
  parsed.hash = "";
  parsed.search = "";
  return { baseUrl: trimTrailingSlashes(parsed.toString()), apiKey };
}

export async function collectThreadPages<T extends Thread | SearchThreadResult>(
  loadPage: (cursor?: string) => Promise<ThreadPage<T>>,
  maxPages = 100,
): Promise<T[]> {
  if (!Number.isInteger(maxPages) || maxPages < 1) throw new Error("maxPages must be a positive integer.");
  const results: T[] = [];
  const seenIDs = new Set<string>();
  const seenCursors = new Set<string>();
  let cursor: string | undefined;
  for (let pageNumber = 0; pageNumber < maxPages; pageNumber += 1) {
    const page = await loadPage(cursor);
    for (const thread of page.threads) {
      if (!seenIDs.has(thread.id)) {
        seenIDs.add(thread.id);
        results.push(thread);
      }
    }
    if (!page.page.has_more) return results;
    const next = page.page.next_cursor?.trim();
    if (!next) throw new Error("Agentbox continuation response omitted next_cursor.");
    if (seenCursors.has(next)) throw new Error("Agentbox continuation response repeated a cursor.");
    seenCursors.add(next);
    cursor = next;
  }
  throw new Error(`Agentbox continuation exceeded ${maxPages} pages.`);
}

export function attributionLabel(snapshot: AttributionSnapshot, fallback: string): string {
  const user = snapshot.created_by_user_display_name?.trim();
  const actor = snapshot.created_by_actor_name?.trim();
  if (user && actor && user.toLowerCase() !== actor.toLowerCase()) return `${user} · ${actor}`;
  return user || actor || (fallback !== "" ? fallback : "Agentbox user");
}

export function visibilityLabels(summary: ThreadVisibilitySummary): string[] {
  const labels: string[] = [];
  if (summary.private) labels.push("Private");
  labels.push(...summary.shared_teams.map((team) => team.name));
  if (summary.public) labels.push("Public");
  if (labels.length > 0) return labels;
  if (summary.owned_by_me) return ["Owned"];
  if (summary.shared_with_me) return ["Shared with me"];
  return ["Unshared"];
}

export function assetAvailability(asset: Asset): { available: boolean; label?: string } {
  if (asset.purged_at) return { available: false, label: "Attachment deleted by deployment owner" };
  if (asset.unavailable) return { available: false, label: asset.unavailable_reason || "Attachment unavailable" };
  return { available: true };
}

function threadPageQuery(params: ThreadPageParams): URLSearchParams {
  const query = new URLSearchParams({ limit: String(params.limit ?? 50) });
  if (params.cursor?.trim()) query.set("cursor", params.cursor.trim());
  const filter = params.filter ?? "all";
  if (filter !== "all") query.set("filter", filter);
  if (filter === "team") query.set("team", nonEmpty(params.team ?? "", "team filter"));
  return query;
}

function decodeThreadPage<T extends Thread | SearchThreadResult>(
  value: unknown,
  decodeItem: (value: unknown, path: string) => T,
): ThreadPage<T> {
  const record = expectRecord(value, "response");
  const threads = expectArray(record.threads, "threads").map((thread, index) =>
    decodeItem(thread, `threads[${index}]`),
  );
  const pageRecord = expectRecord(record.page, "page");
  const page: ThreadPageInfo = {
    limit: expectNumber(pageRecord.limit, "page.limit"),
    has_more: expectBoolean(pageRecord.has_more, "page.has_more"),
  };
  if (pageRecord.next_cursor !== undefined) page.next_cursor = expectString(pageRecord.next_cursor, "page.next_cursor");
  if (page.has_more && !page.next_cursor) throw new Error("Agentbox continuation response omitted next_cursor.");
  return { threads, page };
}

function decodeAuthContext(value: unknown): AuthContext {
  const record = expectRecord(value, "auth");
  return {
    user_id: expectString(record.user_id, "auth.user_id"),
    user_display_name: optionalString(record.user_display_name, "auth.user_display_name"),
    subject_type: expectString(record.subject_type, "auth.subject_type"),
    actor_id: optionalString(record.actor_id, "auth.actor_id"),
    actor_name: expectString(record.actor_name, "auth.actor_name"),
    key_id: optionalString(record.key_id, "auth.key_id"),
    session_id: optionalString(record.session_id, "auth.session_id"),
    scopes: optionalStringArray(record.scopes, "auth.scopes"),
    is_owner: optionalBoolean(record.is_owner, "auth.is_owner"),
  };
}

function decodeTeam(value: unknown, path: string): Team {
  const record = expectRecord(value, path);
  return {
    id: expectString(record.id, `${path}.id`),
    slug: expectString(record.slug, `${path}.slug`),
    name: expectString(record.name, `${path}.name`),
    created_at: expectString(record.created_at, `${path}.created_at`),
    updated_at: expectString(record.updated_at, `${path}.updated_at`),
  };
}

function decodeVisibilitySummary(value: unknown, path: string): ThreadVisibilitySummary {
  const record = expectRecord(value, path);
  return {
    owned_by_me: expectBoolean(record.owned_by_me, `${path}.owned_by_me`),
    private: expectBoolean(record.private, `${path}.private`),
    shared_with_me: expectBoolean(record.shared_with_me, `${path}.shared_with_me`),
    shared_teams: expectArray(record.shared_teams, `${path}.shared_teams`).map((team, index) =>
      decodeThreadTeamSummary(team, `${path}.shared_teams[${index}]`),
    ),
    matched_teams: expectArray(record.matched_teams, `${path}.matched_teams`).map((team, index) =>
      decodeThreadTeamSummary(team, `${path}.matched_teams[${index}]`),
    ),
    public: expectBoolean(record.public, `${path}.public`),
  };
}

function decodeThreadTeamSummary(value: unknown, path: string): ThreadTeamSummary {
  const record = expectRecord(value, path);
  return {
    id: expectString(record.id, `${path}.id`),
    slug: expectString(record.slug, `${path}.slug`),
    name: expectString(record.name, `${path}.name`),
  };
}

function decodeThread(value: unknown, path = "thread"): Thread {
  const record = expectRecord(value, path);
  return {
    id: expectString(record.id, `${path}.id`),
    owner_user_id: expectString(record.owner_user_id, `${path}.owner_user_id`),
    title: expectString(record.title, `${path}.title`),
    created_at: expectString(record.created_at, `${path}.created_at`),
    updated_at: expectString(record.updated_at, `${path}.updated_at`),
    created_by: expectString(record.created_by, `${path}.created_by`),
    created_by_user_id: optionalString(record.created_by_user_id, `${path}.created_by_user_id`),
    created_by_key_id: optionalString(record.created_by_key_id, `${path}.created_by_key_id`),
    created_by_user_display_name: optionalString(
      record.created_by_user_display_name,
      `${path}.created_by_user_display_name`,
    ),
    created_by_actor_name: optionalString(record.created_by_actor_name, `${path}.created_by_actor_name`),
    message_count: optionalNumber(record.message_count, `${path}.message_count`),
    last_message_preview: optionalString(record.last_message_preview, `${path}.last_message_preview`),
    visibility_summary: decodeVisibilitySummary(record.visibility_summary, `${path}.visibility_summary`),
  };
}

function decodeSearchThreadResult(value: unknown, path: string): SearchThreadResult {
  const record = expectRecord(value, path);
  const thread = decodeThread(record, path);
  return {
    ...thread,
    message_count: expectNumber(record.message_count, `${path}.message_count`),
    last_message_preview: expectString(record.last_message_preview, `${path}.last_message_preview`),
    matched_snippets: expectStringArray(record.matched_snippets, `${path}.matched_snippets`),
  };
}

function decodeThreadWithMessages(value: unknown): ThreadWithMessages {
  const record = expectRecord(value, "thread");
  return {
    ...decodeThread(record),
    messages: expectArray(record.messages, "thread.messages").map((message, index) =>
      decodeMessage(message, `thread.messages[${index}]`),
    ),
  };
}

function decodeMessage(value: unknown, path = "message"): Message {
  const record = expectRecord(value, path);
  return {
    id: expectString(record.id, `${path}.id`),
    thread_id: expectString(record.thread_id, `${path}.thread_id`),
    author: expectString(record.author, `${path}.author`),
    body: expectString(record.body, `${path}.body`),
    body_content_type: nullableString(record.body_content_type, `${path}.body_content_type`),
    created_at: expectString(record.created_at, `${path}.created_at`),
    assets: expectArray(record.assets, `${path}.assets`).map((asset, index) =>
      decodeAsset(asset, `${path}.assets[${index}]`),
    ),
    created_by_user_id: optionalString(record.created_by_user_id, `${path}.created_by_user_id`),
    created_by_key_id: optionalString(record.created_by_key_id, `${path}.created_by_key_id`),
    created_by_user_display_name: optionalString(
      record.created_by_user_display_name,
      `${path}.created_by_user_display_name`,
    ),
    created_by_actor_name: optionalString(record.created_by_actor_name, `${path}.created_by_actor_name`),
  };
}

function decodeAsset(value: unknown, path: string): Asset {
  const record = expectRecord(value, path);
  rejectForbiddenKeys(record, path, ["storage_key", "public_url"]);
  return {
    id: expectString(record.id, `${path}.id`),
    message_id: expectString(record.message_id, `${path}.message_id`),
    file_name: expectString(record.file_name, `${path}.file_name`),
    filename: expectString(record.filename, `${path}.filename`),
    mime_type: nullableString(record.mime_type, `${path}.mime_type`),
    size_bytes: expectNumber(record.size_bytes, `${path}.size_bytes`),
    download_url: optionalString(record.download_url, `${path}.download_url`),
    preview_url: optionalString(record.preview_url, `${path}.preview_url`),
    created_at: expectString(record.created_at, `${path}.created_at`),
    created_by: expectString(record.created_by, `${path}.created_by`),
    created_by_user_id: optionalString(record.created_by_user_id, `${path}.created_by_user_id`),
    created_by_key_id: optionalString(record.created_by_key_id, `${path}.created_by_key_id`),
    created_by_user_display_name: optionalString(
      record.created_by_user_display_name,
      `${path}.created_by_user_display_name`,
    ),
    created_by_actor_name: optionalString(record.created_by_actor_name, `${path}.created_by_actor_name`),
    purged_at: optionalString(record.purged_at, `${path}.purged_at`),
    unavailable: optionalBoolean(record.unavailable, `${path}.unavailable`),
    unavailable_reason: optionalString(record.unavailable_reason, `${path}.unavailable_reason`),
  };
}

function decodePresignedUpload(value: unknown, index: number): PresignedUpload {
  const path = `uploads[${index}]`;
  const record = expectRecord(value, path);
  rejectForbiddenKeys(record, path, ["storage_key", "public_url"]);
  return {
    upload_id: expectString(record.upload_id, `${path}.upload_id`),
    file_name: expectString(record.file_name, `${path}.file_name`),
    mime_type: nullableString(record.mime_type, `${path}.mime_type`),
    size_bytes: expectNumber(record.size_bytes, `${path}.size_bytes`),
    upload_url: expectString(record.upload_url, `${path}.upload_url`),
    expires_in: expectNumber(record.expires_in, `${path}.expires_in`),
    required_headers: expectStringRecord(record.required_headers, `${path}.required_headers`),
  };
}

function decodeManagedThreadVisibility(value: unknown): ManagedThreadVisibility {
  const record = expectRecord(value, "visibility");
  return {
    thread_id: expectString(record.thread_id, "visibility.thread_id"),
    owner_user_id: expectString(record.owner_user_id, "visibility.owner_user_id"),
    shared_teams: expectArray(record.shared_teams, "visibility.shared_teams").map((team, index) =>
      decodeTeam(team, `visibility.shared_teams[${index}]`),
    ),
    available_teams: expectArray(record.available_teams, "visibility.available_teams").map((team, index) =>
      decodeTeam(team, `visibility.available_teams[${index}]`),
    ),
    public: expectBoolean(record.public, "visibility.public"),
    public_link: record.public_link === undefined ? undefined : decodePublicLink(record.public_link),
    public_url: optionalString(record.public_url, "visibility.public_url"),
  };
}

function decodePublicLink(value: unknown): ThreadPublicLink {
  const record = expectRecord(value, "visibility.public_link");
  return {
    thread_id: expectString(record.thread_id, "visibility.public_link.thread_id"),
    token_prefix: expectString(record.token_prefix, "visibility.public_link.token_prefix"),
    created_by_user_id: optionalString(record.created_by_user_id, "visibility.public_link.created_by_user_id"),
    created_at: expectString(record.created_at, "visibility.public_link.created_at"),
    updated_at: expectString(record.updated_at, "visibility.public_link.updated_at"),
    revoked_at: optionalString(record.revoked_at, "visibility.public_link.revoked_at"),
  };
}

function expectRecord(value: unknown, path: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    throw new Error(`${path} must be an object.`);
  return value as Record<string, unknown>;
}

function expectArray(value: unknown, path: string): unknown[] {
  if (!Array.isArray(value)) throw new Error(`${path} must be an array.`);
  return value;
}

function expectString(value: unknown, path: string): string {
  if (typeof value !== "string") throw new Error(`${path} must be a string.`);
  return value;
}

function optionalString(value: unknown, path: string): string | undefined {
  if (value === undefined || value === null) return undefined;
  return expectString(value, path);
}

function nullableString(value: unknown, path: string): string | null {
  if (value === null || value === undefined) return null;
  return expectString(value, path);
}

function expectNumber(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isFinite(value)) throw new Error(`${path} must be a finite number.`);
  return value;
}

function expectBoolean(value: unknown, path: string): boolean {
  if (typeof value !== "boolean") throw new Error(`${path} must be a boolean.`);
  return value;
}

function optionalBoolean(value: unknown, path: string): boolean | undefined {
  if (value === undefined || value === null) return undefined;
  return expectBoolean(value, path);
}

function optionalNumber(value: unknown, path: string): number | undefined {
  if (value === undefined || value === null) return undefined;
  return expectNumber(value, path);
}

function expectStringArray(value: unknown, path: string): string[] {
  return expectArray(value, path).map((item, index) => expectString(item, `${path}[${index}]`));
}

function optionalStringArray(value: unknown, path: string): string[] | undefined {
  if (value === undefined || value === null) return undefined;
  return expectStringArray(value, path);
}

function expectStringRecord(value: unknown, path: string): Record<string, string> {
  const record = expectRecord(value, path);
  return Object.fromEntries(Object.entries(record).map(([key, item]) => [key, expectString(item, `${path}.${key}`)]));
}

function rejectForbiddenKeys(record: Record<string, unknown>, path: string, keys: string[]) {
  for (const key of keys) {
    if (key in record) throw new Error(`${path} contains forbidden field ${key}.`);
  }
}

function nonEmpty(value: string, label: string): string {
  const trimmed = value.trim();
  if (!trimmed) throw new Error(`${label} is required.`);
  return trimmed;
}

function trimTrailingSlashes(value: string): string {
  return value.replace(/\/+$/, "");
}

function ensureTrailingSlash(value: string): string {
  return `${trimTrailingSlashes(value)}/`;
}

function trimLeadingSlashes(value: string): string {
  return value.replace(/^\/+/, "");
}

function jsonHeaders(): Record<string, string> {
  return { "content-type": "application/json" };
}

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) throw await responseError(response);
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

async function responseError(response: Response): Promise<AgentboxAPIError> {
  const payload = await parseErrorPayload(response);
  return new AgentboxAPIError(response.status, payload, `Request failed with HTTP ${response.status}`);
}

async function parseErrorPayload(response: Response): Promise<BackendErrorPayload | undefined> {
  const text = await response.text();
  if (text.trim() === "") return undefined;
  try {
    return JSON.parse(text) as BackendErrorPayload;
  } catch {
    return { error: text };
  }
}
