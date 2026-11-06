#!/usr/bin/env bash
# whatsmeow API parity check (after sealjay/mcp-whatsapp's mdtest-parity.sh).
#
# Verifies that every whatsmeow client method the daemon calls is still
# defined as "func (cli *Client) Method(" in the pinned module, so an
# upstream rename or removal fails CI before it reaches a user.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

mod_version=$(go list -m -f '{{.Version}}' go.mau.fi/whatsmeow)
mod_dir=$(go env GOMODCACHE)/go.mau.fi/whatsmeow@${mod_version}
if [[ ! -d "$mod_dir" ]]; then
  echo "error: whatsmeow module not found at ${mod_dir}; run 'go mod download' first" >&2
  exit 1
fi

methods=(
  AddEventHandler
  Connect
  Disconnect
  Download
  GetGroupInfo
  GetQRChannel
  PairPhone
  IsConnected
  ParseWebMessage
  SendMessage
  Upload
)

missing=()
for m in "${methods[@]}"; do
  if ! grep -rq "^func (cli \*Client) ${m}(" "$mod_dir"; then
    missing+=("$m")
  fi
done

if (( ${#missing[@]} > 0 )); then
  echo "error: whatsmeow API parity check failed at ${mod_version}." >&2
  echo "These methods are no longer defined on *whatsmeow.Client:" >&2
  printf '  - %s\n' "${missing[@]}" >&2
  echo "Fix the call sites in internal/daemon/." >&2
  exit 1
fi
echo "whatsmeow API parity: all ${#methods[@]} tracked methods present at ${mod_version}."
