-- The production runbook creates the permanent owner after migrations through
-- 0016 and before tenant scaffolding is removed by 0017. Authentication rows
-- were reset by 0006, so allowing the transitional owner row to omit tenant_id
-- is safe and is required to make that documented sequence executable.
alter table users alter column tenant_id drop not null;

alter table assets
  add column if not exists purged_at timestamptz,
  add column if not exists purged_by_user_id text references users(id) on delete set null,
  add column if not exists purge_last_attempt_at timestamptz,
  add column if not exists purge_error text;

create index if not exists assets_uploader_unpurged_idx
  on assets (created_by_user_id, created_at, id)
  where purged_at is null and created_by_user_id is not null;
