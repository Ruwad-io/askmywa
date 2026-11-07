#!/usr/bin/env bash
# Regenerates rsrc_windows_amd64.syso and rsrc_windows_arm64.syso, the
# Windows resources (logo and file description) that `go build` links into
# askmywa.exe automatically. Run it after changing assets/icon-256.png; the
# outputs are committed, so builds do not need the tool.
#
# Uses go-winres (https://github.com/tc-hib/go-winres, MIT), pinned below.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
go run github.com/tc-hib/go-winres@v0.3.3 simply \
  --icon assets/icon-256.png \
  --arch amd64,arm64 \
  --product-name askmywa \
  --file-description "askmywa, WhatsApp MCP server" \
  --original-filename askmywa.exe \
  --copyright "MIT License" \
  --out rsrc
ls -l rsrc_windows_*.syso
