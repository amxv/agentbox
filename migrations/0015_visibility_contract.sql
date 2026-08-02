-- Public thread URLs are live bearer links that authenticated participants must
-- be able to copy again from the visibility API. Keep the hash for anonymous
-- lookup and retain the token value only for authenticated redisplay. It is
-- never emitted through public DTOs or logs.

alter table thread_public_links
  add column if not exists token_value text;

alter table thread_public_links
  drop constraint if exists thread_public_links_token_value_not_blank;

alter table thread_public_links
  add constraint thread_public_links_token_value_not_blank
  check (token_value is null or btrim(token_value) <> '');
