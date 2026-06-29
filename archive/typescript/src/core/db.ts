import postgres from "postgres";
import type { Asset, Message, Thread, ThreadWithMessages } from "./types";

let sqlClient: postgres.Sql | null = null;
let schemaReady: Promise<void> | null = null;

const memoryDb = {
  threads: [] as Thread[],
  messages: [] as Message[],
  assets: [] as Asset[]
};

function isMemoryDbEnabled(): boolean {
  return process.env.AGENTBOX_TEST_DB === "memory";
}

function nowIso(): string {
  return new Date().toISOString();
}

function getSql() {
  if (!process.env.DATABASE_URL) {
    throw new Error("DATABASE_URL is required.");
  }

  sqlClient ??= postgres(process.env.DATABASE_URL, {
    max: Number(process.env.AGENTBOX_DB_POOL_SIZE ?? "3"),
    idle_timeout: 20,
    connect_timeout: 10
  });

  return sqlClient;
}

export async function ensureSchema(): Promise<void> {
  if (isMemoryDbEnabled()) return;

  schemaReady ??= (async () => {
    const sql = getSql();
    await sql`
      create table if not exists threads (
        id text primary key,
        title text not null,
        created_at timestamptz not null default now(),
        updated_at timestamptz not null default now(),
        created_by text not null
      )
    `;

    await sql`
      create table if not exists messages (
        id text primary key,
        thread_id text not null references threads(id) on delete cascade,
        author text not null,
        body text not null,
        created_at timestamptz not null default now()
      )
    `;

    await sql`
      create table if not exists assets (
        id text primary key,
        message_id text not null references messages(id) on delete cascade,
        storage_key text not null,
        file_name text not null,
        mime_type text,
        size_bytes integer not null,
        public_url text,
        created_at timestamptz not null default now(),
        created_by text not null
      )
    `;

    await sql`create index if not exists threads_updated_at_idx on threads(updated_at desc)`;
    await sql`create index if not exists messages_thread_created_idx on messages(thread_id, created_at asc)`;
    await sql`create index if not exists assets_message_id_idx on assets(message_id)`;
  })();

  return schemaReady;
}

function normalizeThread(row: Record<string, unknown>): Thread {
  return {
    id: String(row.id),
    title: String(row.title),
    created_at: new Date(row.created_at as string).toISOString(),
    updated_at: new Date(row.updated_at as string).toISOString(),
    created_by: String(row.created_by)
  };
}

function normalizeAsset(row: Record<string, unknown>): Asset {
  return {
    id: String(row.id),
    message_id: String(row.message_id),
    storage_key: String(row.storage_key),
    file_name: String(row.file_name),
    mime_type: row.mime_type ? String(row.mime_type) : null,
    size_bytes: Number(row.size_bytes),
    public_url: row.public_url ? String(row.public_url) : null,
    created_at: new Date(row.created_at as string).toISOString(),
    created_by: String(row.created_by)
  };
}

function normalizeMessage(row: Record<string, unknown>, assets: Asset[] = []): Message {
  return {
    id: String(row.id),
    thread_id: String(row.thread_id),
    author: String(row.author),
    body: String(row.body),
    created_at: new Date(row.created_at as string).toISOString(),
    assets
  };
}

export async function listThreads(limit = 50): Promise<Thread[]> {
  if (isMemoryDbEnabled()) {
    return [...memoryDb.threads]
      .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
      .slice(0, limit);
  }

  await ensureSchema();
  const sql = getSql();
  const rows = await sql`
    select id, title, created_at, updated_at, created_by
    from threads
    order by updated_at desc
    limit ${limit}
  `;
  return rows.map(normalizeThread);
}

export async function createThread(title: string, author: string): Promise<Thread> {
  if (isMemoryDbEnabled()) {
    const createdAt = nowIso();
    const thread: Thread = {
      id: `thr_${crypto.randomUUID()}`,
      title,
      created_at: createdAt,
      updated_at: createdAt,
      created_by: author
    };
    memoryDb.threads.push(thread);
    return thread;
  }

  await ensureSchema();
  const sql = getSql();
  const id = `thr_${crypto.randomUUID()}`;
  const rows = await sql`
    insert into threads (id, title, created_by)
    values (${id}, ${title}, ${author})
    returning id, title, created_at, updated_at, created_by
  `;
  return normalizeThread(rows[0]);
}

export async function getThread(threadId: string): Promise<ThreadWithMessages | null> {
  if (isMemoryDbEnabled()) {
    const thread = memoryDb.threads.find((entry) => entry.id === threadId);
    if (!thread) return null;

    return {
      ...thread,
      messages: memoryDb.messages
        .filter((message) => message.thread_id === threadId)
        .sort((a, b) => a.created_at.localeCompare(b.created_at))
        .map((message) => ({
          ...message,
          assets: memoryDb.assets
            .filter((asset) => asset.message_id === message.id)
            .sort((a, b) => a.created_at.localeCompare(b.created_at))
        }))
    };
  }

  await ensureSchema();
  const sql = getSql();
  const threadRows = await sql`
    select id, title, created_at, updated_at, created_by
    from threads
    where id = ${threadId}
  `;

  if (threadRows.length === 0) return null;

  const messageRows = await sql`
    select id, thread_id, author, body, created_at
    from messages
    where thread_id = ${threadId}
    order by created_at asc
  `;

  const messageIds = messageRows.map((row) => String(row.id));
  const assetRows = messageIds.length > 0
    ? await sql`
      select id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by
      from assets
      where message_id in ${sql(messageIds)}
      order by created_at asc
    `
    : [];

  const assetsByMessage = new Map<string, Asset[]>();
  for (const row of assetRows) {
    const asset = normalizeAsset(row);
    const existing = assetsByMessage.get(asset.message_id) ?? [];
    existing.push(asset);
    assetsByMessage.set(asset.message_id, existing);
  }

  return {
    ...normalizeThread(threadRows[0]),
    messages: messageRows.map((row) => normalizeMessage(row, assetsByMessage.get(String(row.id)) ?? []))
  };
}

export async function getAsset(assetId: string): Promise<Asset | null> {
  if (isMemoryDbEnabled()) {
    return memoryDb.assets.find((asset) => asset.id === assetId) ?? null;
  }

  await ensureSchema();
  const sql = getSql();
  const rows = await sql`
    select id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by
    from assets
    where id = ${assetId}
  `;
  return rows.length ? normalizeAsset(rows[0]) : null;
}

export type NewAsset = {
  storageKey: string;
  fileName: string;
  mimeType?: string | null;
  sizeBytes: number;
  publicUrl?: string | null;
};

export async function postMessage(params: {
  threadId: string;
  author: string;
  body: string;
  asset?: NewAsset | null;
}): Promise<Message> {
  if (isMemoryDbEnabled()) {
    const thread = memoryDb.threads.find((entry) => entry.id === params.threadId);
    if (!thread) {
      throw new Error("insert or update on table \"messages\" violates foreign key constraint \"messages_thread_id_fkey\"");
    }

    const message: Message = {
      id: `msg_${crypto.randomUUID()}`,
      thread_id: params.threadId,
      author: params.author,
      body: params.body,
      created_at: nowIso(),
      assets: []
    };
    memoryDb.messages.push(message);
    thread.updated_at = nowIso();

    if (!params.asset) return message;

    const asset: Asset = {
      id: `asset_${crypto.randomUUID()}`,
      message_id: message.id,
      storage_key: params.asset.storageKey,
      file_name: params.asset.fileName,
      mime_type: params.asset.mimeType ?? null,
      size_bytes: params.asset.sizeBytes,
      public_url: params.asset.publicUrl ?? null,
      created_at: nowIso(),
      created_by: params.author
    };
    memoryDb.assets.push(asset);
    return { ...message, assets: [asset] };
  }

  await ensureSchema();
  const sql = getSql();
  const messageId = `msg_${crypto.randomUUID()}`;

  const rows = await sql.begin(async (tx) => {
    const [message] = await tx`
      insert into messages (id, thread_id, author, body)
      values (${messageId}, ${params.threadId}, ${params.author}, ${params.body})
      returning id, thread_id, author, body, created_at
    `;

    await tx`update threads set updated_at = now() where id = ${params.threadId}`;

    if (!params.asset) return { message, assets: [] as Asset[] };

    const assetId = `asset_${crypto.randomUUID()}`;
    const [assetRow] = await tx`
      insert into assets (id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_by)
      values (
        ${assetId},
        ${messageId},
        ${params.asset.storageKey},
        ${params.asset.fileName},
        ${params.asset.mimeType ?? null},
        ${params.asset.sizeBytes},
        ${params.asset.publicUrl ?? null},
        ${params.author}
      )
      returning id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by
    `;

    return { message, assets: [normalizeAsset(assetRow)] };
  });

  return normalizeMessage(rows.message, rows.assets);
}


export async function closeDb(): Promise<void> {
  if (isMemoryDbEnabled()) {
    memoryDb.threads = [];
    memoryDb.messages = [];
    memoryDb.assets = [];
    return;
  }

  if (!sqlClient) return;
  const client = sqlClient;
  sqlClient = null;
  schemaReady = null;
  await client.end({ timeout: 5 });
}
