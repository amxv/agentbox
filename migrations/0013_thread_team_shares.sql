-- Phase 7 widens the single normal-user thread access predicate from owner-only
-- to owner-or-current-team-membership. A thread is never copied when shared.

create table if not exists thread_team_shares (
  thread_id text not null references threads(id) on delete cascade,
  team_id text not null references teams(id) on delete restrict,
  created_by_user_id text references users(id) on delete set null,
  created_at timestamptz not null default now(),
  primary key (thread_id, team_id)
);

create index if not exists thread_team_shares_team_thread_idx
  on thread_team_shares (team_id, thread_id);

create index if not exists thread_team_shares_creator_idx
  on thread_team_shares (created_by_user_id)
  where created_by_user_id is not null;
