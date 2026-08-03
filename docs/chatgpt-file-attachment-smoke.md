# ChatGPT host file-attachment smoke

Run this only from the credentialed local Phase 20 machine after the exact
Phase 19 commit is deployed to the AgentBox backend and dashboard. It requires
ChatGPT developer-mode connector access, a dedicated ordinary-user ChatGPT
credential, production PostgreSQL/R2 access for verification, and a private
test thread owned by or shared with that user.

This procedure verifies the real ChatGPT host projection. The credential-free
schema, SSRF, size, timeout, R2, and compensation tests must already pass on the
same commit:

```bash
git switch feat/user-team-sharing
git pull --ff-only origin feat/user-team-sharing
git status --short --branch
git log -1 --oneline
bun install --frozen-lockfile
bun run verify:chatgpt-files
```

Stop if the checkout is dirty, the commit differs from the deployed backend,
or the readiness command fails. Never record the MCP URL, API key, temporary
`download_url`, connector request payload, cookies, database URL, or R2 secret
in screenshots or logs.

## 1. Refresh the connector and its tool projection

1. Sign in to AgentBox as the ordinary test user and create or rotate only that
   user's dedicated **ChatGPT** connection from Onboarding or Credentials.
2. In ChatGPT developer settings, remove/recreate or refresh the AgentBox
   connector using the complete one-time MCP URL.
3. Run **Scan Tools** when the developer surface exposes that action.
4. Confirm the connector exposes `post_message` and text-only posting still has
   no required file field.
5. When the developer surface displays JSON Schema, confirm `file` is an
   optional object with exactly these properties:

```text
download_url  string, required
file_id       string, required
mime_type     string, optional
file_name     string, optional
additionalProperties: false
```

It must not appear as a free-form string, path, URL input, or manual file-ID
field. If ChatGPT still projects the previous string shape, stop: the connector
has not rediscovered the deployed Phase 19 descriptor.

## 2. Prepare a private target and unique run marker

Create a private AgentBox thread owned by the test user. Record only its thread
ID and a non-secret run marker:

```bash
export CHATGPT_FILE_SMOKE_RUN_ID="chatgpt-file-$(date -u +%Y%m%dT%H%M%SZ)"
export CHATGPT_FILE_SMOKE_THREAD_ID="thr_REPLACE_ME"
mkdir -p ./tmp/chatgpt-file-smoke
```

Before the test, record the thread's message/asset counts from PostgreSQL or the
ordinary API so failed attempts can be proven not to create partial state.

## 3. Create a real ChatGPT artifact

In the same ChatGPT conversation that has the refreshed AgentBox connector,
ask ChatGPT:

```text
Create a Markdown file artifact named agentbox-chatgpt-artifact-smoke.md with
exactly the following UTF-8 content and a final newline. Give me the file as an
artifact; do not paste it only as chat text.

# AgentBox ChatGPT artifact smoke
run-id: CHATGPT_FILE_SMOKE_RUN_ID
transport: host-expanded-file-object
expected-attribution: User · ChatGPT
```

Replace `CHATGPT_FILE_SMOKE_RUN_ID` in the prompt with the actual marker. Download
the original artifact from ChatGPT to the trusted local directory and hash it:

```bash
mv ~/Downloads/agentbox-chatgpt-artifact-smoke.md \
  ./tmp/chatgpt-file-smoke/original.md
shasum -a 256 ./tmp/chatgpt-file-smoke/original.md
wc -c ./tmp/chatgpt-file-smoke/original.md
```

The local copy is the byte-for-byte oracle. Do not infer expected bytes from the
rendered chat preview.

## 4. Attach it using only natural language

Send this prompt in ChatGPT, replacing the two placeholders:

```text
Post the message "CHATGPT_FILE_SMOKE_RUN_ID native artifact smoke" to AgentBox
thread CHATGPT_FILE_SMOKE_THREAD_ID and attach the file I just created. Do not
paste the file contents into the message.
```

Do not mention or supply a `file_...` identifier, sandbox path, local path,
temporary download URL, base64 payload, or JSON conversion. ChatGPT must select
the conversation artifact conceptually and expand it for the MCP call.

The call must create exactly one message and one attached asset. A request for a
manual ID/path or a text-only fallback is a failed host acceptance test.

## 5. Verify AgentBox state and attribution

Read the thread through the dashboard, CLI, or ordinary authenticated API and
verify:

- the message body contains the unique run marker exactly once;
- the message is attributed to the expected deployment-global user and actor
  `ChatGPT` (`User · ChatGPT` in the UI);
- the attached asset has the same user/actor attribution snapshot;
- the safe filename is `agentbox-chatgpt-artifact-smoke.md`;
- MIME is `text/markdown` when ChatGPT supplied it, otherwise AgentBox safely
  infers a Markdown-compatible MIME from the filename;
- size matches the original local artifact;
- the thread remained private or retained only its pre-existing team shares;
- no temporary host URL appears in the API response, database row, timeline,
  or logs.

Download the AgentBox attachment into a separate directory and compare exact
bytes:

```bash
agentbox --profile CHATGPT_TEST_USER download \
  "$CHATGPT_FILE_SMOKE_THREAD_ID" \
  --output ./tmp/chatgpt-file-smoke/from-agentbox \
  --json

cmp \
  ./tmp/chatgpt-file-smoke/original.md \
  ./tmp/chatgpt-file-smoke/from-agentbox/agentbox-chatgpt-artifact-smoke.md

shasum -a 256 \
  ./tmp/chatgpt-file-smoke/original.md \
  ./tmp/chatgpt-file-smoke/from-agentbox/agentbox-chatgpt-artifact-smoke.md
```

`cmp` must exit zero and both hashes/sizes must match.

## 6. Verify private R2 persistence

Using the trusted PostgreSQL session, locate the single new asset by the run
marker and record its asset ID, storage key, filename, MIME, size, message user,
message key, and actor snapshots. Do not print unrelated message bodies:

```bash
export CHATGPT_FILE_SMOKE_EXPECTED_BODY="$CHATGPT_FILE_SMOKE_RUN_ID native artifact smoke"
psql "$DATABASE_URL" \
  -v ON_ERROR_STOP=1 \
  -v thread_id="$CHATGPT_FILE_SMOKE_THREAD_ID" \
  -v expected_body="$CHATGPT_FILE_SMOKE_EXPECTED_BODY" <<'SQL'
select
  m.id as message_id,
  m.created_by_user_id,
  m.created_by_key_id,
  m.created_by_user_display_name,
  m.created_by_actor_name,
  a.id as asset_id,
  a.storage_key,
  a.file_name,
  a.mime_type,
  a.size_bytes,
  a.created_by_user_id as asset_created_by_user_id,
  a.created_by_key_id as asset_created_by_key_id,
  a.created_by_user_display_name as asset_created_by_user_display_name,
  a.created_by_actor_name as asset_created_by_actor_name
from messages m
join assets a on a.message_id = m.id
where m.thread_id = :'thread_id'
  and m.body = :'expected_body';
SQL
```

Require exactly one row. Inspect the object at that private `storage_key` with
the existing R2-compatible tooling used by the backup preflight. Its size and
content type must match the row and its downloaded bytes must match the local
oracle. The database must not store the temporary ChatGPT `download_url` or
expose it as a reusable URL. The opaque `file_id` may be used only as a private
storage-key hint; it is not a caller-supplied workflow or downloadable location.

## 7. Prove text-only posting still works

Ask ChatGPT:

```text
Post the message "CHATGPT_FILE_SMOKE_RUN_ID text-only regression" to AgentBox
thread CHATGPT_FILE_SMOKE_THREAD_ID with no attachment.
```

Verify exactly one text-only message is created with `User · ChatGPT`
attribution and zero assets.

## 8. Prove paths and manual identifiers are not a workflow

In a fresh turn, ask ChatGPT:

```text
Post a message to AgentBox thread CHATGPT_FILE_SMOKE_THREAD_ID and attach the
literal path sandbox:/mnt/data/not-a-real-agentbox-artifact.md. Do not create a
file artifact first.
```

The connector must not successfully create a message/asset by passing that
literal string. Acceptable outcomes are that ChatGPT declines, asks for an
actual artifact, or the tool call is rejected before network/storage work.
Compare the thread message/asset counts with the pre-attempt values and require
no partial row/object. Do not test production with loopback, metadata-service,
private-network, or oversized destinations; those adversarial cases are covered
deterministically by `bun run verify:chatgpt-files` on the exact deployed code.

## 9. Record evidence and clean up

Record only:

- exact Git/deployment commit;
- ChatGPT connector name and scan/refresh result, without its URL;
- thread, message, and asset IDs;
- sanitized schema shape;
- original and downloaded SHA-256/byte counts;
- filename, MIME, attribution, visibility, and private R2-head results;
- text-only and literal-path outcomes;
- any defect with exact reproduction steps, excluding secrets/temporary URLs.

Delete local artifact copies after evidence is captured, revoke the disposable
ChatGPT credential, and remove the disposable thread if product policy allows.
If any check fails, keep production maintenance mode enabled where applicable,
fix the narrowest issue on `feat/user-team-sharing`, update the blueprint, push
normally, wait for exact-head CI, redeploy, refresh/scan the connector again, and
repeat the blocked checks.
