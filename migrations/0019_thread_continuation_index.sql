-- Ordinary inbox and search traversal use a stable (updated_at, id) keyset.
-- Keep this deployment-global because effective access may come from ownership
-- or any current team membership rather than one owner-scoped index alone.

create index if not exists threads_updated_id_idx
  on threads (updated_at desc, id asc);
