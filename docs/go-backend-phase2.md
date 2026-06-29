# Go Backend Phase 2 Notes

Phase 2 adds the Go module and backend core while the TypeScript API remains the parity oracle.

## Migration Entrypoint Strategy

The Go repository ports the current idempotent `ensureSchema` behavior as `Repository.EnsureSchema(ctx)`. During this phase there is no public Go CLI migration command, because the final CLI command surface is scheduled for a later phase. The Go API can run schema creation at startup only when `AGENTBOX_AUTO_MIGRATE=true`; otherwise callers can invoke `EnsureSchema` from tests or rollout tooling.

This preserves the current lazy-schema compatibility path without claiming a finalized migration UX before the Go CLI is ported.

## Vercel Upload Behavior

The existing TypeScript implementation defaults to `AGENTBOX_MAX_FILE_SIZE_BYTES=26214400` (25 MiB), but Vercel Functions currently cap request and response payloads at 4.5 MB. The Go config therefore exposes:

- `MaxFileSizeBytes`: the backend object/file limit, defaulting to 25 MiB for non-Vercel/self-hosted operation.
- `MultipartLimitBytes`: capped to 4,500,000 bytes whenever `VERCEL=1` or `VERCEL_ENV` is set.

Until a direct-upload flow is implemented in a later phase, the Go Vercel deployment must not advertise 25 MiB multipart uploads through a function request body.

