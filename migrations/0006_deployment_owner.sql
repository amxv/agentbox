-- Authentication and authorization records are explicitly disposable for the
-- user/team cutover. Content rows and their historical attribution snapshots are
-- preserved; only live accounts, sessions, login codes, and credentials reset.
delete from cli_login_codes;
delete from user_sessions;
delete from api_keys;
delete from users;

alter table users add column if not exists is_owner boolean not null default false;

create unique index if not exists users_email_idx on users (lower(email));
create unique index if not exists users_single_owner_idx on users ((is_owner)) where is_owner;

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
    if new.role <> 'admin' then
      raise exception 'deployment owner role cannot be changed' using errcode = '23514';
    end if;
    if new.disabled_at is not null then
      raise exception 'deployment owner cannot be disabled' using errcode = '23514';
    end if;
  end if;
  return new;
end;
$$;

drop trigger if exists users_protect_deployment_owner on users;
create trigger users_protect_deployment_owner
before update or delete on users
for each row execute function protect_deployment_owner();
