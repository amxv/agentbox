create table if not exists owner_setup_tokens (
  id text primary key,
  token_hash text not null unique,
  purpose text not null check (purpose in ('bootstrap', 'recovery')),
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  consumed_at timestamptz,
  revoked_at timestamptz
);

create unique index if not exists owner_setup_tokens_one_active_idx
  on owner_setup_tokens ((true))
  where consumed_at is null and revoked_at is null;
create index if not exists owner_setup_tokens_expires_idx
  on owner_setup_tokens (expires_at);
