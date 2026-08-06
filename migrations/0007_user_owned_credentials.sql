-- Migration 0006 resets all disposable identity and credential rows, so these
-- structural changes do not need a compatibility backfill. Tenant columns
-- remain on users and content tables only as temporary Phase 2/3 scaffolding.

drop index if exists api_keys_tenant_active_name_idx;
drop index if exists api_keys_tenant_id_idx;

alter table api_keys add column if not exists purpose text not null default 'custom';
alter table api_keys alter column user_id set not null;

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conrelid = 'api_keys'::regclass
      and conname = 'api_keys_user_id_fkey'
  ) then
    alter table api_keys
      add constraint api_keys_user_id_fkey
      foreign key (user_id) references users(id) on delete cascade;
  end if;
end $$;

alter table api_keys drop column if exists tenant_id;
alter table api_keys drop column if exists key_value;

create unique index if not exists api_keys_user_active_name_idx
  on api_keys (user_id, lower(name))
  where revoked_at is null;
create index if not exists api_keys_user_id_idx on api_keys (user_id);

drop index if exists user_sessions_tenant_user_idx;
alter table user_sessions drop column if exists tenant_id;
create index if not exists user_sessions_user_id_idx on user_sessions (user_id);

drop index if exists cli_login_codes_tenant_user_idx;
alter table cli_login_codes drop column if exists tenant_id;
create index if not exists cli_login_codes_user_id_idx on cli_login_codes (user_id);
