-- Direct uploads are written only to temporary staging keys. Finalization
-- claims a row, copies the verified object to a content-addressed immutable
-- final key, and publishes that final key transactionally with the message.

alter table pending_uploads add column if not exists expected_sha256 text;
alter table pending_uploads add column if not exists status text not null default 'pending';
alter table pending_uploads add column if not exists final_storage_key text;
alter table pending_uploads add column if not exists finalization_token text;
alter table pending_uploads add column if not exists finalization_started_at timestamptz;
alter table pending_uploads add column if not exists rejected_at timestamptz;
alter table pending_uploads add column if not exists rejection_reason text;

alter table pending_uploads drop constraint if exists pending_uploads_status_check;
alter table pending_uploads
  add constraint pending_uploads_status_check
  check (status in ('pending', 'finalizing', 'finalized', 'rejected'));

alter table pending_uploads drop constraint if exists pending_uploads_expected_sha256_check;
alter table pending_uploads
  add constraint pending_uploads_expected_sha256_check
  check (expected_sha256 is null or expected_sha256 ~ '^[0-9a-f]{64}$');

create unique index if not exists pending_uploads_final_storage_key_unique
  on pending_uploads (final_storage_key)
  where final_storage_key is not null;

create index if not exists pending_uploads_cleanup_idx
  on pending_uploads (status, expires_at, created_at);

create table if not exists upload_cleanup_objects (
  id text primary key,
  upload_id text references pending_uploads(id) on delete cascade,
  storage_key text not null unique,
  object_kind text not null check (object_kind in ('staging', 'final_candidate')),
  not_before timestamptz not null,
  created_at timestamptz not null default now(),
  attempt_count integer not null default 0,
  last_attempt_at timestamptz,
  last_error text,
  cleaned_at timestamptz
);

create index if not exists upload_cleanup_objects_due_idx
  on upload_cleanup_objects (not_before, created_at, id)
  where cleaned_at is null;

-- Existing active rows become cleanup-tracked staging rows. They lack a
-- checksum and therefore cannot be finalized by the new contract, but their
-- exact keys remain visible to backup/preflight and bounded cleanup.
insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before)
select 'ucl_' || md5('staging:' || p.id || ':' || p.storage_key),
       p.id,
       p.storage_key,
       'staging',
       p.expires_at
from pending_uploads p
on conflict (storage_key) do nothing;
