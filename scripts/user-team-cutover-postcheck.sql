do $$
declare
  legacy_column_count integer;
begin
  if not exists (
    select 1 from schema_migrations where version = '0017'
  ) then
    raise exception 'migration 0017 is not recorded in schema_migrations';
  end if;

  if to_regclass('public.tenants') is not null then
    raise exception 'legacy tenants table still exists';
  end if;
  if exists (
    select 1 from pg_trigger
    where tgname = 'users_assign_legacy_threads_to_owner' and not tgisinternal
  ) or exists (
    select 1 from pg_proc where proname = 'assign_legacy_threads_to_deployment_owner'
  ) then
    raise exception 'temporary owner-backfill trigger or function still exists';
  end if;

  select count(*)::integer
  into legacy_column_count
  from information_schema.columns
  where table_schema = 'public'
    and (
      column_name = 'tenant_id'
      or (table_name = 'users' and column_name = 'role')
      or (table_name in ('assets', 'pending_uploads') and column_name = 'public_url')
    );
  if legacy_column_count <> 0 then
    raise exception 'legacy schema columns remain: %', legacy_column_count;
  end if;

  if (select count(*) from users where is_owner) <> 1 then
    raise exception 'expected exactly one permanent owner';
  end if;
  if exists (select 1 from users where is_owner and disabled_at is not null) then
    raise exception 'permanent owner is disabled';
  end if;
  if exists (select 1 from threads where owner_user_id is null) then
    raise exception 'thread without owner remains';
  end if;
  if exists (
    select 1 from threads t
    left join users u on u.id = t.owner_user_id
    where u.id is null
  ) then
    raise exception 'thread owner reference is orphaned';
  end if;
  if exists (
    select 1 from messages m
    left join threads t on t.id = m.thread_id
    where t.id is null
  ) then
    raise exception 'message thread reference is orphaned';
  end if;
  if exists (
    select 1 from assets a
    left join messages m on m.id = a.message_id
    where m.id is null
  ) then
    raise exception 'asset message reference is orphaned';
  end if;
  if exists (
    select 1 from pending_uploads p
    left join threads t on t.id = p.thread_id
    where t.id is null
  ) then
    raise exception 'pending upload thread reference is orphaned';
  end if;
end $$;

select json_build_object(
  'schema_version', (select max(version) from schema_migrations),
  'owners', (select count(*) from users where is_owner),
  'users', (select count(*) from users),
  'teams', (select count(*) from teams),
  'team_memberships', (select count(*) from team_memberships),
  'threads', (select count(*) from threads),
  'messages', (select count(*) from messages),
  'assets', (select count(*) from assets),
  'pending_uploads', (select count(*) from pending_uploads),
  'thread_team_shares', (select count(*) from thread_team_shares),
  'active_public_links', (select count(*) from thread_public_links where revoked_at is null)
) as agentbox_cutover_postcheck;
