create table if not exists api_keys (
  name text primary key,
  key_value text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
