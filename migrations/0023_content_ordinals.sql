-- Preserve user-authored message and attachment order independently of wall
-- clock precision. Legacy rows have no recoverable submission sequence when
-- timestamps tie, so the deterministic backfill uses the strongest available
-- historical key: created_at followed by stable row ID.

alter table messages add column position bigint;
alter table assets add column position bigint;

with ranked_messages as (
  select id,
         row_number() over (
           partition by thread_id
           order by created_at, id
         )::bigint as position
  from messages
)
update messages m
set position = ranked_messages.position
from ranked_messages
where ranked_messages.id = m.id;

with ranked_assets as (
  select id,
         row_number() over (
           partition by message_id
           order by created_at, id
         )::bigint as position
  from assets
)
update assets a
set position = ranked_assets.position
from ranked_assets
where ranked_assets.id = a.id;

alter table messages alter column position set not null;
alter table assets alter column position set not null;

alter table messages
  add constraint messages_position_positive check (position > 0);

alter table assets
  add constraint assets_position_positive check (position > 0);

create unique index messages_thread_position_unique
  on messages (thread_id, position);

create unique index assets_message_position_unique
  on assets (message_id, position);
