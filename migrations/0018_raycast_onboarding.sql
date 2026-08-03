-- Add Raycast as the fourth resumable onboarding connector without rewriting
-- or deleting any existing ChatGPT, Claude, or local connector state.

alter table user_onboarding_steps
  drop constraint if exists user_onboarding_steps_connector_check;

alter table user_onboarding_steps
  add constraint user_onboarding_steps_connector_check
  check (connector in ('chatgpt', 'claude', 'local', 'raycast'));
