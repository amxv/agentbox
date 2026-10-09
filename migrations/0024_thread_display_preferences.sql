-- Reader-specific presentation preferences. Never changes the underlying
-- message body or content type, and never grants access to a thread.
create table thread_display_preferences (
  user_id text not null references users(id) on delete cascade,
  thread_id text not null references threads(id) on delete cascade,
  all_plain_as_markdown boolean not null default false,
  message_preferences jsonb not null default '{}'::jsonb,
  updated_at timestamptz not null default now(),
  primary key (user_id, thread_id),
  constraint thread_display_message_preferences_object
    check (jsonb_typeof(message_preferences) = 'object')
);
