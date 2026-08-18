#!/usr/bin/env bash
set -euo pipefail

if command -v bun >/dev/null 2>&1; then
  (cd apps/dashboard && bun install --frozen-lockfile --linker=isolated)
fi
