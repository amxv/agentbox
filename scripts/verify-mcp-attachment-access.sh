#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo 'Checking that MCP attachments remain explicit tool calls...'
if rg -n 'AddResource\(|AddResourceTemplate\(|EmbeddedResource' internal/agentbox/mcpserver --glob '!**/*_test.go'; then
  echo 'Agentbox MCP must not globally register or eagerly embed attachment resources.' >&2
  exit 1
fi

echo 'Checking bounded direct-R2 attachment reads...'
go test ./internal/agentbox/assets \
  -run '^(TestFakeStoreUploadAndSignedURL|TestR2RangeReadUsesDirectBoundedGetAndIfMatch)$' \
  -count=1

echo 'Checking attachment text classification, chunking, authorization, and direct download preparation...'
go test ./internal/agentbox/service \
  -run '^Test(ReadAttachment|AttachmentReadAndDownload|PrepareAttachmentDownload)' \
  -count=1

echo 'Checking MCP descriptors and explicit read/download flow...'
go test ./internal/agentbox/mcpserver \
  -run '^(TestToolsExposeMetadataAndAnnotations|TestAttachmentToolsUseExplicitReadThenDirectDownloadFlow)$' \
  -count=1

echo 'MCP attachment access readiness passed.'
