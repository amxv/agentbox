-- Phase 3 establishes deployment-global, user-owned private content. Legacy
-- tenant columns remain only as temporary cutover scaffolding until Phase 14.

alter table threads add column if not exists owner_user_id text;

alter table threads add column if not exists created_by_user_display_name text;
alter table threads add column if not exists created_by_actor_name text;
alter table messages add column if not exists created_by_user_display_name text;
alter table messages add column if not exists created_by_actor_name text;
alter table assets add column if not exists created_by_user_display_name text;
alter table assets add column if not exists created_by_actor_name text;
alter table pending_uploads add column if not exists created_by_user_display_name text;
alter table pending_uploads add column if not exists created_by_actor_name text;

-- Authentication rows were intentionally reset in migration 0006. Any
-- historical attribution IDs that no longer resolve are legacy snapshots, not
-- authorization identities, and must become null before foreign keys are added.
update threads set created_by_user_id = null
where created_by_user_id is not null
  and not exists (select 1 from users u where u.id = threads.created_by_user_id);
update messages set created_by_user_id = null
where created_by_user_id is not null
  and not exists (select 1 from users u where u.id = messages.created_by_user_id);
update assets set created_by_user_id = null
where created_by_user_id is not null
  and not exists (select 1 from users u where u.id = assets.created_by_user_id);
update pending_uploads set created_by_user_id = null
where created_by_user_id is not null
  and not exists (select 1 from users u where u.id = pending_uploads.created_by_user_id);

update threads set created_by_key_id = null
where created_by_key_id is not null
  and not exists (select 1 from api_keys k where k.id = threads.created_by_key_id);
update messages set created_by_key_id = null
where created_by_key_id is not null
  and not exists (select 1 from api_keys k where k.id = messages.created_by_key_id);
update assets set created_by_key_id = null
where created_by_key_id is not null
  and not exists (select 1 from api_keys k where k.id = assets.created_by_key_id);
update pending_uploads set created_by_key_id = null
where created_by_key_id is not null
  and not exists (select 1 from api_keys k where k.id = pending_uploads.created_by_key_id);

do $$
begin
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'threads'::regclass and conname = 'threads_owner_user_id_fkey'
  ) then
    alter table threads
      add constraint threads_owner_user_id_fkey
      foreign key (owner_user_id) references users(id) on delete restrict;
  end if;

  if not exists (
    select 1 from pg_constraint
    where conrelid = 'threads'::regclass and conname = 'threads_created_by_user_id_fkey'
  ) then
    alter table threads
      add constraint threads_created_by_user_id_fkey
      foreign key (created_by_user_id) references users(id) on delete set null;
  end if;
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'threads'::regclass and conname = 'threads_created_by_key_id_fkey'
  ) then
    alter table threads
      add constraint threads_created_by_key_id_fkey
      foreign key (created_by_key_id) references api_keys(id) on delete set null;
  end if;

  if not exists (
    select 1 from pg_constraint
    where conrelid = 'messages'::regclass and conname = 'messages_created_by_user_id_fkey'
  ) then
    alter table messages
      add constraint messages_created_by_user_id_fkey
      foreign key (created_by_user_id) references users(id) on delete set null;
  end if;
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'messages'::regclass and conname = 'messages_created_by_key_id_fkey'
  ) then
    alter table messages
      add constraint messages_created_by_key_id_fkey
      foreign key (created_by_key_id) references api_keys(id) on delete set null;
  end if;

  if not exists (
    select 1 from pg_constraint
    where conrelid = 'assets'::regclass and conname = 'assets_created_by_user_id_fkey'
  ) then
    alter table assets
      add constraint assets_created_by_user_id_fkey
      foreign key (created_by_user_id) references users(id) on delete set null;
  end if;
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'assets'::regclass and conname = 'assets_created_by_key_id_fkey'
  ) then
    alter table assets
      add constraint assets_created_by_key_id_fkey
      foreign key (created_by_key_id) references api_keys(id) on delete set null;
  end if;

  if not exists (
    select 1 from pg_constraint
    where conrelid = 'pending_uploads'::regclass and conname = 'pending_uploads_created_by_user_id_fkey'
  ) then
    alter table pending_uploads
      add constraint pending_uploads_created_by_user_id_fkey
      foreign key (created_by_user_id) references users(id) on delete set null;
  end if;
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'pending_uploads'::regclass and conname = 'pending_uploads_created_by_key_id_fkey'
  ) then
    alter table pending_uploads
      add constraint pending_uploads_created_by_key_id_fkey
      foreign key (created_by_key_id) references api_keys(id) on delete set null;
  end if;
end $$;

-- Populate immutable snapshots only where a stable post-reset user reference is
-- known. Legacy author/created_by strings remain untouched and continue to be
-- the fallback for rows whose original actor cannot be mapped.
update threads t
set created_by_user_display_name = coalesce(t.created_by_user_display_name, u.display_name),
    created_by_actor_name = coalesce(t.created_by_actor_name, t.created_by)
from users u
where t.created_by_user_id = u.id;
update messages m
set created_by_user_display_name = coalesce(m.created_by_user_display_name, u.display_name),
    created_by_actor_name = coalesce(m.created_by_actor_name, m.author)
from users u
where m.created_by_user_id = u.id;
update assets a
set created_by_user_display_name = coalesce(a.created_by_user_display_name, u.display_name),
    created_by_actor_name = coalesce(a.created_by_actor_name, a.created_by)
from users u
where a.created_by_user_id = u.id;
update pending_uploads p
set created_by_user_display_name = coalesce(p.created_by_user_display_name, u.display_name),
    created_by_actor_name = coalesce(p.created_by_actor_name, p.created_by)
from users u
where p.created_by_user_id = u.id;

-- A deployment upgraded after owner setup can backfill immediately. Fresh
-- cutovers have no user yet, so the owner insert trigger below performs the same
-- backfill atomically when the one-time owner setup token is consumed.
update threads
set owner_user_id = (select id from users where is_owner limit 1)
where owner_user_id is null
  and exists (select 1 from users where is_owner);

create or replace function assign_legacy_threads_to_deployment_owner()
returns trigger
language plpgsql
as $$
begin
  if new.is_owner then
    update threads set owner_user_id = new.id where owner_user_id is null;
  end if;
  return new;
end;
$$;

drop trigger if exists users_assign_legacy_threads_to_owner on users;
create trigger users_assign_legacy_threads_to_owner
after insert or update of is_owner on users
for each row execute function assign_legacy_threads_to_deployment_owner();

-- NOT VALID permits a pre-owner legacy database to migrate without inventing
-- an identity. PostgreSQL still enforces the constraint for all new/updated
-- rows. Phase 14 validates it after the owner backfill and removes scaffolding.
do $$
begin
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'threads'::regclass and conname = 'threads_owner_user_id_required'
  ) then
    alter table threads
      add constraint threads_owner_user_id_required
      check (owner_user_id is not null) not valid;
  end if;
end $$;

create index if not exists threads_owner_updated_idx
  on threads (owner_user_id, updated_at desc);
create index if not exists messages_thread_created_user_access_idx
  on messages (thread_id, created_at asc);
create index if not exists pending_uploads_user_thread_idx
  on pending_uploads (created_by_user_id, thread_id, created_at desc);
