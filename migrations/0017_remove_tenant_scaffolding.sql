-- Point-of-no-return user/team cutover. A legacy deployment with content must
-- create the permanent owner before applying this migration so ownership is
-- explicit rather than inferred or invented.
do $$
begin
  if exists (select 1 from threads where owner_user_id is null) then
    raise exception 'permanent owner setup is required before migration 0017'
      using errcode = '23514';
  end if;
end $$;

alter table threads validate constraint threads_owner_user_id_required;
alter table threads alter column owner_user_id set not null;
alter table threads drop constraint if exists threads_owner_user_id_required;

drop trigger if exists users_assign_legacy_threads_to_owner on users;
drop function if exists assign_legacy_threads_to_deployment_owner();

drop index if exists threads_tenant_updated_idx;
drop index if exists messages_tenant_thread_created_idx;
drop index if exists assets_tenant_message_id_idx;
drop index if exists pending_uploads_tenant_thread_idx;
drop index if exists users_tenant_email_idx;
drop index if exists users_tenant_id_idx;

alter table threads drop column if exists tenant_id;
alter table messages drop column if exists tenant_id;
alter table assets drop column if exists tenant_id;
alter table pending_uploads drop column if exists tenant_id;
alter table users drop column if exists tenant_id;

-- R2 objects are private. Download access is always represented by a short-
-- lived signed URL, never a durable URL stored beside the object reference.
alter table assets drop column if exists public_url;
alter table pending_uploads drop column if exists public_url;

create or replace function protect_deployment_owner()
returns trigger
language plpgsql
as $$
begin
  if tg_op = 'DELETE' then
    if old.is_owner then
      raise exception 'deployment owner cannot be deleted' using errcode = '23514';
    end if;
    return old;
  end if;
  if old.is_owner then
    if not new.is_owner then
      raise exception 'deployment owner cannot be demoted' using errcode = '23514';
    end if;
    if new.disabled_at is not null then
      raise exception 'deployment owner cannot be disabled' using errcode = '23514';
    end if;
  end if;
  return new;
end;
$$;

alter table users drop column if exists role;
drop table if exists tenants;
