# Go Backend Phase 2 Notes

Phase 2 adds the Go module and backend core while the TypeScript API remains the parity oracle.

## Migration Entrypoint Strategy

The Go repository ports the current idempotent `ensureSchema` behavior as `Repository.EnsureSchema(ctx)`. The migration entrypoint is now:

```bash
bun run db:migrate
```

which runs:

```bash
go run ./cmd/migrate
```

The Go API can still run schema creation at startup when `AGENTBOX_AUTO_MIGRATE=true`, but production rollout should prefer the explicit migration command.

This preserves the current lazy-schema compatibility path while giving rollout a non-server migration command.

## Vercel Upload Behavior

The existing TypeScript implementation defaults to `AGENTBOX_MAX_FILE_SIZE_BYTES=26214400` (25 MiB), but Vercel Functions currently cap request and response payloads at 4.5 MB. The Go config therefore exposes:

- `MaxFileSizeBytes`: the backend object/file limit, defaulting to 25 MiB for non-Vercel/self-hosted operation.
- `MultipartLimitBytes`: capped to 4,500,000 bytes whenever `VERCEL=1` or `VERCEL_ENV` is set.

Until a direct-upload flow is implemented in a later phase, the Go Vercel deployment must not advertise 25 MiB multipart uploads through a function request body.
