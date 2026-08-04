do $$
declare
  legacy_column_count integer;
begin
  if not exists (
    select 1 from schema_migrations where version = '0017'
  ) then
    raise exception 'migration 0017 is not recorded in schema_migrations';
  end if;
  if not exists (
    select 1 from schema_migrations where version = '0023'
  ) then
    raise exception 'migration 0023 is not recorded in schema_migrations';
  end if;

  if to_regclass('tenants') is not null then
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
  where table_schema = current_schema()
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
  if exists (select 1 from messages where position <= 0) then
    raise exception 'message with invalid ordinal remains';
  end if;
  if exists (
    select 1
    from messages
    group by thread_id, position
    having count(*) > 1
  ) then
    raise exception 'duplicate message ordinal remains';
  end if;
  if exists (
    select 1 from assets a
    left join messages m on m.id = a.message_id
    where m.id is null
  ) then
    raise exception 'asset message reference is orphaned';
  end if;
  if exists (select 1 from assets where position <= 0) then
    raise exception 'asset with invalid ordinal remains';
  end if;
  if exists (
    select 1
    from assets
    group by message_id, position
    having count(*) > 1
  ) then
    raise exception 'duplicate asset ordinal remains';
  end if;
  if exists (
    select 1 from pending_uploads p
    left join threads t on t.id = p.thread_id
    where t.id is null
  ) then
    raise exception 'pending upload thread reference is orphaned';
  end if;
  if to_regclass('upload_cleanup_objects') is null then
    raise exception 'upload cleanup inventory table is missing';
  end if;
  if exists (
    select 1
    from upload_cleanup_objects c
    left join pending_uploads p on p.id = c.upload_id
    where c.upload_id is not null and p.id is null
  ) then
    raise exception 'upload cleanup reference is orphaned';
  end if;
  if exists (
    select 1
    from pending_uploads p
    where not exists (
      select 1
      from upload_cleanup_objects c
      where c.upload_id = p.id
        and c.object_kind = 'staging'
        and c.storage_key = p.storage_key
    )
  ) then
    raise exception 'pending upload is missing its exact staging cleanup record';
  end if;
  if exists (
    select 1
    from pending_uploads
    where status not in ('pending', 'finalizing', 'finalized', 'rejected')
       or (status = 'pending' and (final_storage_key is not null or finalization_token is not null or finalization_started_at is not null))
       or (status = 'finalizing' and (final_storage_key is null or finalization_token is null or finalization_started_at is null))
       or (status = 'finalized' and (final_storage_key is null or consumed_at is null or finalization_token is not null))
       or (status = 'rejected' and rejected_at is null)
  ) then
    raise exception 'pending upload lifecycle state is inconsistent';
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
  'pending_upload_cleanup', (select count(*) from upload_cleanup_objects where cleaned_at is null),
  'thread_team_shares', (select count(*) from thread_team_shares),
  'active_public_links', (select count(*) from thread_public_links where revoked_at is null)
) as agentbox_cutover_postcheck;
