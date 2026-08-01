create table if not exists signup_invitations (
  id text primary key,
  token_hash text not null unique,
  created_by_user_id text not null references users(id) on delete restrict,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  consumed_at timestamptz,
  consumed_by_user_id text references users(id) on delete set null,
  revoked_at timestamptz
);

create index if not exists signup_invitations_created_at_idx
  on signup_invitations (created_at desc);
create index if not exists signup_invitations_expires_idx
  on signup_invitations (expires_at)
  where consumed_at is null and revoked_at is null;
