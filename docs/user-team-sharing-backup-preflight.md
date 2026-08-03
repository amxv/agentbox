# User/team cutover backup preflight

This command is the required content-preservation gate before the user/team
authorization cutover. It is read-only for PostgreSQL and for existing R2
objects. Its only storage mutation is copying referenced objects into the
configured recovery bucket/prefix.

Do not run the schema cutover unless the generated manifest contains:

```json
{
  "ready": true
}
```

## Requirements

- Production `DATABASE_URL`.
- `pg_dump` installed and compatible with the production PostgreSQL server.
- `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, and `R2_SECRET_ACCESS_KEY`.
- `R2_BUCKET`, or an explicit `--source-bucket`.
- A secure local or mounted `--output-dir` outside the application deployment.
- Permission to list and read the source R2 bucket and to write the recovery
  bucket/prefix.

The output directory contains a database dump and a manifest. Treat both as
sensitive backup material. Store a copy outside the deployment being migrated.

## Run the preflight

Use a stable run ID so an interrupted operation can safely resume without
copying objects that already match the source:

```bash
export AGENTBOX_BACKUP_OUTPUT_DIR=/secure/off-host/agentbox-backups

bun run backup:preflight -- \
  --run-id user-team-cutover-2026-08-01 \
  --source-prefix agentbox/ \
  --backup-prefix agentbox-recovery/user-team-cutover-2026-08-01
```

By default the command copies recovery objects into `R2_BUCKET` under
`agentbox-recovery/<run-id>/`. A separate recovery bucket is preferable when
available:

```bash
bun run backup:preflight -- \
  --output-dir /secure/off-host/agentbox-backups \
  --run-id user-team-cutover-2026-08-01 \
  --source-bucket agentbox-production \
  --backup-bucket agentbox-recovery \
  --backup-prefix snapshots/user-team-cutover-2026-08-01
```

Useful environment equivalents are:

```text
AGENTBOX_BACKUP_OUTPUT_DIR
AGENTBOX_BACKUP_SOURCE_PREFIX
AGENTBOX_BACKUP_BUCKET
AGENTBOX_BACKUP_PREFIX
AGENTBOX_PG_DUMP_BINARY
```

The database URL is passed to `pg_dump` through `PGDATABASE`, not printed or
placed in the command line. Do not enable shell tracing while production secrets
are loaded.

## What the command verifies

The command holds one read-only, repeatable-read PostgreSQL transaction open,
exports its snapshot to `pg_dump`, and records counts from that same snapshot for:

- threads;
- messages;
- assets;
- pending uploads.

It also reports relational orphan counts and enumerates every exact
`storage_key` referenced by assets and pending uploads. For each distinct key it:

1. verifies that the source object exists;
2. compares the database size with R2 metadata;
3. copies the exact source key beneath the recovery prefix;
4. verifies the copied object size and ETag when available.

The original opaque object key is preserved as the suffix of the recovery key.
No production object is renamed or deleted.

An existing recovery object with matching size and ETag is accepted, making a
rerun with the same run ID idempotent. A missing referenced object, size mismatch,
copy failure, dump failure, or relational orphan makes `ready` false and exits
non-zero. Objects present in the source inventory but absent from PostgreSQL are
recorded as warnings for investigation; they do not by themselves make the
content backup incomplete.

## Output

Each run writes:

```text
<output-dir>/<run-id>/database.dump
<output-dir>/<run-id>/manifest.json
```

The manifest includes:

- PostgreSQL row and orphan counts;
- dump size and SHA-256;
- every database object reference;
- source object metadata;
- recovery object key and metadata;
- missing and unreferenced objects;
- blocking errors and warnings;
- the exact `pg_restore` command template.

The manifest is written even when content verification fails, so failures can be
investigated and the same run resumed.

## Recovery verification

Before the maintenance window, validate the dump outside production:

```bash
pg_restore --list /secure/off-host/agentbox-backups/<run-id>/database.dump >/dev/null

createdb agentbox_recovery_test
pg_restore \
  --clean \
  --if-exists \
  --no-owner \
  --no-acl \
  --dbname agentbox_recovery_test \
  /secure/off-host/agentbox-backups/<run-id>/database.dump
```

Compare restored thread, message, asset, and pending-upload counts with
`manifest.json`. Sample recovery objects by reading their recorded recovery keys
and verify that their metadata matches the manifest.

## Production boundary

The shared Zodex implementation environment does not have production PostgreSQL,
R2, Vercel, or application credentials. It can build and test this command but
must not claim a production backup. A credentialed local agent must run this
preflight, preserve the output off deployment, and record the real manifest path
and counts before the Phase 19 maintenance window. Continue only with the exact
sequence in [`user-team-sharing-production-cutover.md`](user-team-sharing-production-cutover.md).
