-- Owner-issued, single-use password reset links for existing non-owner users.
-- Secrets are stored as SHA-256 hashes; redemption changes only the web password
-- and browser sessions, never the user's agent/API credentials or memberships.
create table if not exists user_password_resets (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  token_hash text not null unique,
  created_by_user_id text not null references users(id) on delete restrict,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  consumed_at timestamptz,
  revoked_at timestamptz
);

create unique index if not exists user_password_resets_one_active_idx
  on user_password_resets (user_id)
  where consumed_at is null and revoked_at is null;
create index if not exists user_password_resets_expiry_idx
  on user_password_resets (expires_at)
  where consumed_at is null and revoked_at is null;
