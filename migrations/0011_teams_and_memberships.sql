-- Phase 5 adds deployment-global teams and overlapping memberships. Threads
-- remain private-only until Phase 7 introduces thread_team_shares.

create table if not exists teams (
  id text primary key,
  slug text not null,
  name text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint teams_slug_not_blank check (btrim(slug) <> ''),
  constraint teams_name_not_blank check (btrim(name) <> '')
);

create unique index if not exists teams_slug_lower_unique
  on teams (lower(slug));

create table if not exists team_memberships (
  team_id text not null references teams(id) on delete restrict,
  user_id text not null references users(id) on delete restrict,
  created_at timestamptz not null default now(),
  primary key (team_id, user_id)
);

create index if not exists team_memberships_user_team_idx
  on team_memberships (user_id, team_id);

create table if not exists signup_invitation_teams (
  invitation_id text not null references signup_invitations(id) on delete cascade,
  team_id text not null references teams(id) on delete restrict,
  created_at timestamptz not null default now(),
  primary key (invitation_id, team_id)
);

create index if not exists signup_invitation_teams_team_idx
  on signup_invitation_teams (team_id, invitation_id);
