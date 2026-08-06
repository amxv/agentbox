#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo 'Checking the ChatGPT file-parameter source boundary...'
legacy_pattern='RawString|File was received as a plain string|Pass the ChatGPT uploaded file ID|uploaded conversation file ID|local filesystem path or plain filename'
if rg -n "$legacy_pattern" \
  internal/agentbox/mcpserver \
  internal/agentbox/assets \
  README.md \
  public/setup-self-host.md \
  --glob '!**/*_test.go'; then
  echo 'Legacy manual file-ID, path, string, or URL compatibility guidance remains.' >&2
  exit 1
fi

if rg -n 'json:"file"|ChatGPTFileReference|ChatGPTFileInput' internal/agentbox/httpapi/server.go; then
  echo 'The ordinary HTTP message adapter still exposes the ChatGPT host file object.' >&2
  exit 1
fi

echo 'Checking the exact OpenAI file-object descriptor...'
go test ./internal/agentbox/mcpserver \
  -run '^(TestPostMessageFileDescriptorMatchesOpenAIContract|TestParseFileInputRequiresClosedStructuredObject|TestPostMessageAcceptsStructuredChatGPTArtifact)$' \
  -count=1

echo 'Checking secure download, R2, and compensation behavior...'
go test ./internal/agentbox/assets \
  -run '^(TestNormalizeChatGPTFileInput|TestSecureRemoteFileFetcher.*|TestR2StoreChatGPT.*)$' \
  -count=1
go test ./internal/agentbox/service \
  -run '^TestChatGPTFileFailureAndAccessLossLeaveNoPartialState$' \
  -count=1
go test ./internal/agentbox/httpapi \
  -run '^TestThreadRoutesAndMultipartAsset$' \
  -count=1

echo 'ChatGPT file-attachment readiness passed.'
