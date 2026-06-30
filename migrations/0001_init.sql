create table if not exists threads (
  id text primary key,
  title text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  created_by text not null
);

create table if not exists messages (
  id text primary key,
  thread_id text not null references threads(id) on delete cascade,
  author text not null,
  body text not null,
  body_content_type text,
  created_at timestamptz not null default now()
);

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
);

create table if not exists api_keys (
  name text primary key,
  key_value text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists threads_updated_at_idx on threads(updated_at desc);
create index if not exists messages_thread_created_idx on messages(thread_id, created_at asc);
create index if not exists assets_message_id_idx on assets(message_id);
