-- Crucible follow-up: retain non-secret setup context for independently
-- labeled Raycast installations. The credential secret remains represented
-- only by its hash and one-time creation/rotation response.

alter table api_keys add column if not exists setup_base_url text;

create index if not exists api_keys_user_created_inventory_idx
  on api_keys (user_id, created_at desc, id desc);

create index if not exists api_keys_user_purpose_inventory_idx
  on api_keys (user_id, purpose, created_at desc, id desc);
