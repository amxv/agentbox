create table if not exists pending_uploads (
  id text primary key,
  thread_id text not null references threads(id) on delete cascade,
  storage_key text not null unique,
  file_name text not null,
  mime_type text,
  size_bytes integer not null,
  public_url text,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  created_by text not null,
  consumed_at timestamptz
);

create index if not exists pending_uploads_thread_idx on pending_uploads(thread_id, created_at desc);
