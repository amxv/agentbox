create extension if not exists pgcrypto;

create table if not exists tenants (
  id text primary key,
  slug text not null unique,
  name text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

insert into tenants (id, slug, name)
values ('ten_default', 'default', 'Default')
on conflict (id) do nothing;

create table if not exists users (
  id text primary key,
  tenant_id text not null references tenants(id) on delete cascade,
  email text not null,
  display_name text not null,
  password_hash text,
  role text not null default 'member',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  disabled_at timestamptz
);

create unique index if not exists users_tenant_email_idx on users (tenant_id, lower(email));
create index if not exists users_tenant_id_idx on users (tenant_id);

create table if not exists user_sessions (
  id text primary key,
  tenant_id text not null references tenants(id) on delete cascade,
  user_id text not null references users(id) on delete cascade,
  secret_hash text not null unique,
  created_at timestamptz not null default now(),
  last_used_at timestamptz,
  expires_at timestamptz not null,
  revoked_at timestamptz
);

create index if not exists user_sessions_tenant_user_idx on user_sessions (tenant_id, user_id);
create index if not exists user_sessions_expires_idx on user_sessions (expires_at);

create table if not exists cli_login_codes (
  id text primary key,
  tenant_id text not null references tenants(id) on delete cascade,
  user_id text not null references users(id) on delete cascade,
  code_hash text not null unique,
  state_hash text not null,
  redirect_uri text not null,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  consumed_at timestamptz
);

create index if not exists cli_login_codes_tenant_user_idx on cli_login_codes (tenant_id, user_id);
create index if not exists cli_login_codes_expires_idx on cli_login_codes (expires_at);

alter table threads add column if not exists tenant_id text;
alter table threads add column if not exists created_by_user_id text;
alter table threads add column if not exists created_by_key_id text;
update threads set tenant_id = 'ten_default' where tenant_id is null;
alter table threads alter column tenant_id set not null;

alter table messages add column if not exists tenant_id text;
alter table messages add column if not exists created_by_user_id text;
alter table messages add column if not exists created_by_key_id text;
update messages m
set tenant_id = coalesce(t.tenant_id, 'ten_default')
from threads t
where m.thread_id = t.id and m.tenant_id is null;
update messages set tenant_id = 'ten_default' where tenant_id is null;
alter table messages alter column tenant_id set not null;

alter table assets add column if not exists tenant_id text;
alter table assets add column if not exists created_by_user_id text;
alter table assets add column if not exists created_by_key_id text;
update assets a
set tenant_id = coalesce(m.tenant_id, 'ten_default')
from messages m
where a.message_id = m.id and a.tenant_id is null;
update assets set tenant_id = 'ten_default' where tenant_id is null;
alter table assets alter column tenant_id set not null;

alter table pending_uploads add column if not exists tenant_id text;
alter table pending_uploads add column if not exists created_by_user_id text;
alter table pending_uploads add column if not exists created_by_key_id text;
update pending_uploads p
set tenant_id = coalesce(t.tenant_id, 'ten_default')
from threads t
where p.thread_id = t.id and p.tenant_id is null;
update pending_uploads set tenant_id = 'ten_default' where tenant_id is null;
alter table pending_uploads alter column tenant_id set not null;

alter table api_keys add column if not exists id text;
alter table api_keys add column if not exists tenant_id text;
alter table api_keys add column if not exists user_id text;
alter table api_keys add column if not exists token_prefix text;
alter table api_keys add column if not exists token_hash text;
alter table api_keys add column if not exists scopes text[] not null default array['threads:read','threads:write','assets:read','assets:write','mcp:use'];
alter table api_keys add column if not exists last_used_at timestamptz;
alter table api_keys add column if not exists revoked_at timestamptz;

update api_keys
set
  id = coalesce(id, 'key_' || replace(gen_random_uuid()::text, '-', '')),
  tenant_id = coalesce(tenant_id, 'ten_default'),
  token_prefix = coalesce(token_prefix, left(key_value, 8)),
  token_hash = coalesce(token_hash, encode(digest(key_value, 'sha256'), 'hex'))
where id is null or tenant_id is null or token_prefix is null or token_hash is null;

alter table api_keys alter column id set not null;
alter table api_keys alter column tenant_id set not null;
alter table api_keys alter column token_prefix set not null;
alter table api_keys alter column token_hash set not null;
alter table api_keys alter column key_value drop not null;

do $$
declare
  pk_name text;
begin
  select conname into pk_name
  from pg_constraint
  where conrelid = 'api_keys'::regclass and contype = 'p';

  if pk_name is not null and pk_name <> 'api_keys_pkey' then
    execute format('alter table api_keys drop constraint %I', pk_name);
  elsif pk_name = 'api_keys_pkey' then
    if not exists (
      select 1
      from pg_attribute a
      join pg_constraint c on c.conrelid = a.attrelid and a.attnum = any(c.conkey)
      where c.conrelid = 'api_keys'::regclass
        and c.contype = 'p'
        and a.attname = 'id'
    ) then
      alter table api_keys drop constraint api_keys_pkey;
    end if;
  end if;

  if not exists (
    select 1 from pg_constraint
    where conrelid = 'api_keys'::regclass and contype = 'p'
  ) then
    alter table api_keys add constraint api_keys_pkey primary key (id);
  end if;
end $$;

create unique index if not exists api_keys_token_hash_idx on api_keys (token_hash);
create unique index if not exists api_keys_tenant_active_name_idx on api_keys (tenant_id, lower(name)) where revoked_at is null;
create index if not exists api_keys_tenant_id_idx on api_keys (tenant_id);
create index if not exists api_keys_user_id_idx on api_keys (user_id);

create index if not exists threads_tenant_updated_idx on threads (tenant_id, updated_at desc);
create index if not exists messages_tenant_thread_created_idx on messages (tenant_id, thread_id, created_at asc);
create index if not exists assets_tenant_message_id_idx on assets (tenant_id, message_id);
create index if not exists pending_uploads_tenant_thread_idx on pending_uploads (tenant_id, thread_id, created_at desc);
