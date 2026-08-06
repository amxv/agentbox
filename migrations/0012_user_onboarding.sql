-- Phase 6 keeps onboarding resumable without pre-creating credentials. Each
-- completed connector step points at the credential that was created by the
-- user's explicit action; revoked credentials remain auditable while the live
-- onboarding view treats the step as disconnected.

create table if not exists user_onboarding (
  user_id text primary key references users(id) on delete cascade,
  dismissed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists user_onboarding_steps (
  user_id text not null references users(id) on delete cascade,
  connector text not null,
  credential_id text references api_keys(id) on delete set null,
  completed_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (user_id, connector),
  constraint user_onboarding_steps_connector_check
    check (connector in ('chatgpt', 'claude', 'local'))
);

create index if not exists user_onboarding_steps_credential_idx
  on user_onboarding_steps (credential_id)
  where credential_id is not null;
