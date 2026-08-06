-- Phase 8 stores only hashed public-link tokens. One row per thread gives each
-- thread at most one live URL; rotation replaces the hash and revocation marks
-- the row inactive without exposing historical token material.

create table if not exists thread_public_links (
  thread_id text primary key references threads(id) on delete cascade,
  token_hash text not null,
  token_prefix text not null,
  created_by_user_id text references users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  revoked_at timestamptz,
  constraint thread_public_links_token_prefix_not_blank check (btrim(token_prefix) <> '')
);

create unique index if not exists thread_public_links_token_hash_unique
  on thread_public_links (token_hash);

create index if not exists thread_public_links_active_token_idx
  on thread_public_links (token_hash, thread_id)
  where revoked_at is null;
