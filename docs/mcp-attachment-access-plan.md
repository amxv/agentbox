# MCP Attachment Read and Download Plan

Status: implemented on `main`; live ChatGPT text reads verified; download conversation handoff under compatibility validation
Date: 2026-08-07
Target: AgentBox Go MCP server and attachment storage path

## 1. Goal

AgentBox is intended to be a shared context inbox for humans and agents. Message
bodies are already directly readable through MCP, but attached Markdown, text,
source code, scripts, logs, and other files currently appear to an MCP agent only
as attachment metadata. The agent can see that a file exists but cannot inspect
its contents or ask the MCP server to hand the file back to the host.

Add two explicit read-only MCP operations so an agent can follow this flow:

1. call `get_thread`;
2. inspect the returned attachment metadata and stable `asset_id` values;
3. call `read_attachment` when it needs raw textual contents; or
4. call `download_attachment` when it needs the original file as a file/blob.

The design must preserve AgentBox's current lazy attachment behavior. Merely
opening a thread must **not** fetch attachment bytes, register every attachment
as an MCP resource, or cause attachment contents to be projected into the model
context.

## 2. Explicit product decisions

### 2.1 `get_thread` remains metadata-only

`get_thread` continues to return the thread, messages, and normal attachment
metadata including the stable asset ID, filename, MIME type, and byte size. It
does not perform an R2 `HEAD`, `GET`, or signed-URL operation and it does not
embed attachment contents.

Its tool description should tell the model what to do next, for example:

> Attachments are returned as metadata. Use `read_attachment` to inspect a
> text/code attachment and `download_attachment` to retrieve the original file.

This deliberately implements a discover-then-fetch workflow rather than an
eager "thread plus all files" response.

### 2.2 Add `read_attachment`

`read_attachment` is the model-facing path for raw Markdown, text, source code,
scripts, logs, JSON/YAML/TOML/XML, configuration files, and other attachments
whose stored bytes are textual.

Proposed input:

```json
{
  "asset_id": "asset_...",
  "offset_bytes": 0,
  "max_bytes": 65536
}
```

- `asset_id` is required.
- `offset_bytes` is optional and defaults to `0`.
- `max_bytes` is optional and defaults to 64 KiB.
- hard-cap one response at 128 KiB of raw file content.
- callers should normally use the returned `next_offset` rather than inventing
  an offset, which keeps UTF-8 chunk boundaries deterministic.

Proposed structured result:

```json
{
  "asset": {
    "id": "asset_...",
    "file_name": "review.md",
    "mime_type": "text/markdown",
    "size_bytes": 33434
  },
  "encoding": "utf-8",
  "text": "# ...",
  "range": {
    "start_byte": 0,
    "end_byte": 33434,
    "total_bytes": 33434,
    "has_more": false
  }
}
```

When more data remains, include `next_offset`. `end_byte` is exclusive. The
tool result should also contain a normal MCP `TextContent` representation so
older clients that ignore `structuredContent` still receive usable text.

The tool is annotated:

```text
readOnlyHint: true
destructiveHint: false
openWorldHint: false
```

### 2.3 Text detection is byte-based, not Markdown-only

Do not limit `read_attachment` to `.md` files or to MIME values beginning with
`text/`. Agents frequently attach source files as `application/octet-stream`,
and the purpose of this feature is to make any ordinary raw text/code artifact
usable as context.

Version 1 should support UTF-8 text, including an optional UTF-8 BOM at byte
zero. MIME type and filename extension are hints, not the final gate. For
unknown or generic MIME types, inspect a bounded leading sample and accept it
when it is valid UTF-8 and does not look binary (for example, embedded NULs or
an implausible density of disallowed control bytes). Common text-oriented
application MIME types such as JSON and XML should be treated as textual but
must still pass UTF-8 validation.

If the attachment is not safely readable as text, return a stable error such as
`ATTACHMENT_NOT_TEXT` with guidance to use `download_attachment`. Do not decode
arbitrary binary formats, archives, PDFs, Office documents, images, or compiled
files inside this tool.

When a requested chunk would split a multibyte UTF-8 code point, shorten the
chunk to the preceding valid boundary and return that exact byte position as
`next_offset`. This ensures concatenating successive chunks reproduces the
original textual bytes after the optional initial BOM handling.

### 2.4 Add `download_attachment`

`download_attachment` is the explicit path for retrieving the original stored
file without interpreting it.

Proposed input:

```json
{
  "asset_id": "asset_..."
}
```

After the same authorization and object-identity checks used by current signed
downloads, return safe structured metadata plus **one standard MCP
`ResourceLink` content item** for the requested attachment. The resource link
uses the short-lived, direct R2 HTTPS URL already produced by AgentBox's signed
download path and includes the filename, MIME type, and size.

Conceptually:

```json
{
  "type": "resource_link",
  "uri": "https://<signed-r2-object-url>",
  "name": "review.md",
  "mimeType": "text/markdown",
  "size": 33434
}
```

The signed URL must not also be copied into ordinary explanatory text. The
resource link itself is the file transfer result. Use the existing bounded URL
expiry (or a similarly short explicit expiry) and mint a fresh URL on each
`download_attachment` call.

This tool is also read-only and closed-world:

```text
readOnlyHint: true
destructiveHint: false
openWorldHint: false
```

### 2.5 Do not register the attachment inventory as MCP resources

Do **not** call `AddResource` for every attachment. Do **not** make
`resources/list` return the user's AgentBox attachment inventory. Do **not**
attach `EmbeddedResource` objects containing attachment bytes to `get_thread`.

Version 1 should not need an attachment `ResourceTemplate` either. The standard
MCP `ResourceLink` returned by `download_attachment` can use the signed HTTPS
URL as a directly fetchable resource. This keeps attachment discovery entirely
behind the explicit tool workflow and means the MCP server does not advertise a
separate resource catalog at all.

This is intentional. MCP resources are application-driven: the host is free to
offer explicit selection, search/filtering, or automatic context inclusion.
AgentBox cannot control another host's resource-selection policy. The strongest
way to satisfy the product requirement is therefore not to enumerate
attachments as resources in the first place.

The only file-like MCP content appears after the model has already selected one
known asset by calling `download_attachment`.

## 3. Why this matches MCP and OpenAI guidance

### 3.1 MCP resources are not guaranteed to stay out of context

The MCP resources specification says resources are application-driven and that
hosts may expose them for explicit selection, search/filtering, or automatic
context inclusion based on heuristics or model selection. The protocol does
not mandate one interaction model.

Therefore globally registering every AgentBox attachment would create exactly
the ambiguity this design is trying to avoid.

Source:

- https://modelcontextprotocol.io/specification/2025-06-18/server/resources

### 3.2 Tool-returned resource links are explicitly supported

The MCP tools specification permits a tool result to contain a `resource_link`
with a URI, name, description, MIME type, size, and annotations. It also states
that resource links returned by tools are not guaranteed to appear in
`resources/list`.

The resources specification recommends `https://` resources when the client
can fetch/load the resource directly from the web without calling
`resources/read`. That is a good fit for AgentBox's existing short-lived,
authorization-checked, direct-R2 download design.

Sources:

- https://modelcontextprotocol.io/specification/2025-06-18/server/tools
- https://modelcontextprotocol.io/specification/2025-06-18/server/resources

### 3.3 The desired chaining pattern matches OpenAI guidance

OpenAI's current MCP server guidance recommends concise `structuredContent` and
stable identifiers so later tool calls can refer to the same records. That maps
directly onto `get_thread -> asset_id -> read_attachment/download_attachment`.

Source:

- https://developers.openai.com/plugins/build/mcp-server

### 3.4 ChatGPT file inputs are documented; reverse file outputs are less explicit

AgentBox already implements the documented ChatGPT-to-MCP input direction:
`_meta["openai/fileParams"]` names a file parameter whose schema contains
`download_url`, `file_id`, `mime_type`, and `file_name`, with the first two
required.

OpenAI's current reference also says ChatGPT's file APIs can operate on files
"returned by tool file references." However, the public reference currently
does not specify a separate server-side file-output schema analogous to
`openai/fileParams`.

Do not invent an undocumented `_meta` output field. The portable MCP
`ResourceLink` remains the canonical download result for every host. Live
ChatGPT testing showed that ChatGPT renders that link as a file card but does
not automatically expose a generic non-image file to the model/sandbox. A
follow-up experiment with `window.openai.uploadFile(file, { library: true })`
returned a valid native file handle and rendered a successful Library-save
status in the widget, but a fresh uploaded fixture still did not appear through
the model's Files/Library surface. There is no documented generic `fileIds`
widget-state field analogous to `imageIds` for arbitrary files.

The production contract therefore keeps the standard `ResourceLink` and also
returns its short-lived signed R2 URL as model-visible `download_url` in the
explicit `download_attachment` result. A tiny companion view performs the
standard `ui/initialize` handshake. If the MCP Apps host advertises
`hostCapabilities.message.resourceLink`, the view sends a `ui/message` whose
content contains that same `ResourceLink`. Current ChatGPT testing showed the
host can render the app but does not advertise that message modality, so the
view falls back to a text `ui/message` containing the expiring `download_url`;
ChatGPT's `sendFollowUpMessage` is a final compatibility fallback. This keeps
the resource capability in an actual conversation turn so a guarded local or
sandbox downloader can consume it without proxying bytes through AgentBox. The
capability expires after `expires_in` seconds, storage keys remain hidden, and
the attachment bytes never transit the AgentBox/Vercel response path.

Source:

- https://developers.openai.com/plugins/reference

## 4. Vercel transport constraint

The Go MCP backend is deployed as a Vercel Function. Vercel currently limits a
normal Function request or response payload to 4.5 MB. AgentBox permits files
up to 25 MiB, and MCP binary `resources/read` payloads are base64-encoded, which
would make them larger still.

Consequences:

- `read_attachment` must remain bounded and comfortably below the function
  response ceiling.
- `download_attachment` must not proxy an arbitrary full attachment through a
  normal MCP JSON response.
- direct client-to-R2 transfer remains the correct large-file architecture.
- a future `resources/read` implementation must not become the sole general
  attachment-download mechanism unless the deployment transport changes or a
  verified streaming path avoids the response limit.

Sources:

- https://vercel.com/docs/functions/limitations
- https://vercel.com/kb/guide/how-to-bypass-vercel-body-size-limit-serverless-functions

## 5. Current AgentBox code fit

The present codebase already has most of the important boundaries.

### 5.1 MCP server

`internal/agentbox/mcpserver/mcpserver.go` currently exposes six tools and no
MCP resources. `get_thread` calls `Service.GetThread` and serializes the
result. It does not inspect or fetch R2 objects. Keep that property.

The Go MCP SDK already supports `mcp.ResourceLink` as tool-result content, so
`download_attachment` does not require a new MCP dependency or an attachment
resource catalog.

### 5.2 Authorization and race-safe signing

`Service.SignedAssetDownloadURL` already provides the correct security model:

1. require `assets:read`;
2. acquire an asset authorization lease and snapshot the asset;
3. close the database lease;
4. reject purged assets;
5. inspect the stored R2 object's size/checksum identity outside PostgreSQL;
6. reacquire authorization;
7. verify the asset identity did not change and it was not purged;
8. mint the signed URL while the second short authorization lease is held.

`download_attachment` should reuse this existing path rather than create a
parallel authorization model.

`read_attachment` needs the same two-phase authorization property around the
actual byte read: authorization must be rechecked after storage I/O and before
bytes are returned to the caller. A team-share removal, user disablement,
attachment purge, or other access change racing a slow R2 read must prevent the
read result from being released.

### 5.3 Scope model

Normal ChatGPT and Claude connector credentials already include `assets:read`,
so no new scope is necessary. A custom MCP credential that lacks `assets:read`
must continue to receive a permission error even if it has `threads:read` and
can see attachment metadata in a thread.

### 5.4 Storage layer

`AssetStore` currently supports upload, `HEAD`, copy, delete, presigned upload,
and signed download, but it has no bounded byte-read primitive. Add a direct R2
range-read operation rather than having the backend sign its own URL and fetch
that URL back over HTTP.

The primitive should support a byte start plus maximum byte count and return
only the requested bounded body. The R2 implementation can use S3 `GetObject`
with `Range`; the fake store should gain exact byte fixtures so tests can prove
chunking and content fidelity.

The range read is for `read_attachment`. General binary downloads continue to
use the existing signed-R2 URL path.

### 5.5 Existing CLI behavior validates the direct-download architecture

`agentbox download` already performs the desired separation: fetch thread
metadata, request a signed URL for one asset, then download bytes directly from
R2 to the local filesystem. `download_attachment` should expose the equivalent
idea through standard MCP result content rather than proxying bytes through the
Go function.

## 6. Proposed implementation slices

### Slice 1: storage read primitive

- Add a bounded/ranged asset-object read operation to `AssetStore` and
  `R2Store`.
- Extend `FakeStore` with deterministic stored bytes for read tests.
- Enforce start/limit validation and storage-error mapping.
- Do not change existing upload or signed-download behavior.

### Slice 2: authorized textual attachment reader

- Add a service method for authorized bounded attachment reads.
- Require `assets:read`.
- Use the existing asset authorization lease and object-identity checks.
- Read storage outside the DB lease and reauthorize before returning bytes.
- Add UTF-8/text sniffing and chunk-boundary logic.
- Map purged, unavailable, unauthorized, non-text, and invalid-range cases to
  stable coded errors.

### Slice 3: MCP `read_attachment`

- Register the new read-only tool with an exact input/output schema.
- Return safe asset metadata plus raw text and byte-range continuation data.
- Update `get_thread` description to point agents at the two attachment tools.
- Do not change `get_thread`'s output shape beyond any documentation/help text
  needed to make the stable asset ID discoverable.

### Slice 4: MCP `download_attachment`

- Register the second read-only tool.
- Resolve/sign exactly one authorized asset through the existing signed-download
  service path.
- Return safe structured metadata plus one `mcp.ResourceLink` whose URI is the
  short-lived direct R2 URL.
- Do not add `mcp.EmbeddedResource` content.
- Do not add per-attachment `AddResource` registrations.
- Do not add an attachment `ResourceTemplate` in v1.
- Do not return R2 storage keys.

### Slice 5: documentation, connector rediscovery, and compatibility smoke

- Document the recommended agent flow and error behavior in README/self-host
  docs.
- Add a credential-free MCP SDK test showing the resource-link result shape.
- After deployment, refresh/recreate the ChatGPT connector and run Scan Tools
  when available so the new tool descriptors are projected.
- Use the existing known Markdown attachment case as a live acceptance fixture.
- Verify ChatGPT can call `get_thread`, then `read_attachment`, and receive the
  exact Markdown source without another connector or file-search surface.
- Verify `download_attachment` produces a host-usable standard ResourceLink and
  the same short-lived URL in structured `download_url`.
- In ChatGPT, verify the companion MCP Apps view negotiates the host's
  `ui/message` modalities. Prefer ResourceLink content when available; otherwise
  verify the text-message fallback places the short-lived `download_url` in a
  user turn and that the next agent turn can fetch it into the sandbox. Compare
  byte count/hash or exact contents to the original fixture.
- Repeat the resource-link download smoke with a generic MCP client/inspector.

## 7. Required regression and acceptance coverage

At minimum, implementation is not complete until tests prove all of the
following.

1. `get_thread` with one or many attachments performs zero attachment storage
   reads and returns only attachment metadata.
2. The MCP server does not enumerate AgentBox attachments through
   `resources/list`; v1 does not register attachment resources/templates.
3. A small Markdown file is returned byte-for-byte as raw Markdown text in one
   `read_attachment` call.
4. Plain text and source code are readable independent of extension when the
   bytes are valid textual UTF-8.
5. A UTF-8 source file larger than the default chunk size can be reconstructed
   exactly by following `next_offset` values, including multibyte characters at
   chunk boundaries.
6. Binary/NUL-heavy or invalid-UTF-8 content receives `ATTACHMENT_NOT_TEXT` and
   does not leak binary bytes into model context.
7. Cross-user private assets cannot be read or downloaded.
8. Current team members can read/download shared-thread assets, and loss of
   membership immediately revokes the capability.
9. A credential without `assets:read` cannot use either attachment tool.
10. Owner-purged and storage-missing/mismatched assets retain the current coded
    unavailable/gone behavior and do not produce file bytes or signed links.
11. A simulated authorization loss while R2 is being read causes
    `read_attachment` to discard the fetched bytes and return no content.
12. `download_attachment` returns exactly one standard `ResourceLink` with the
    expected filename, MIME type, size, and short-lived authorized HTTPS URI,
    and structured `download_url` contains that same URI for agents that need
    same-turn filesystem/sandbox access.
13. `download_attachment` does not embed the file body into the MCP JSON result,
    keeping large files off the Vercel response path.
14. Repeated download calls mint fresh short-lived capabilities and do not
    expose R2 storage keys or credentials.
15. The existing `post_message.file` ChatGPT-to-AgentBox file-input contract and
    its security tests remain unchanged and passing.

## 8. Non-goals for the first implementation

- No automatic parsing or OCR of PDFs, Office files, images, archives, or other
  binary document formats.
- No automatic attachment ingestion into `get_thread`.
- No global attachment search/index in the MCP resource catalog.
- No eager `ResourceLink` values in thread responses.
- No persistent public attachment URLs.
- No arbitrary "fetch this URL" tool.
- No filesystem/sandbox path accepted from an MCP caller.
- No undocumented OpenAI `_meta` field invented for reverse file transfer.
- No change to current direct R2 upload/download ownership or purge semantics.

## 9. Expected end state

For the motivating thread, an agent should be able to do exactly this:

```text
get_thread(thr_...)
  -> sees asset_... / icloud-mail-cli-crucible-round-1.md / text/markdown

read_attachment(asset_...)
  -> receives the raw Markdown contents
```

If the agent instead needs the file itself:

```text
get_thread(thr_...)
  -> sees asset_...

download_attachment(asset_...)
  -> receives one explicit MCP resource/file link for that authorized asset
```

Nothing about merely fetching the thread causes any attachment body to enter
the model context. No global MCP attachment resource inventory exists. The
agent has to identify an attachment and explicitly ask AgentBox to read or
download that specific asset.
