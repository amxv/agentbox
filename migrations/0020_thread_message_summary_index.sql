-- Ordinary inbox pages expose message counts and the latest message preview
-- without issuing per-thread detail requests. Keep the latest-message lookup
-- deterministic when multiple messages share a timestamp.

create index if not exists messages_thread_latest_idx
  on messages (thread_id, created_at desc, id desc);
