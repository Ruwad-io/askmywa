#!/usr/bin/env bash
# Smoke test that needs no WhatsApp account: start an unpaired daemon on an
# empty data directory, check that it answers on its socket without trying
# to pair, then stop it. Runs on macOS, Linux and Windows (Git Bash).
#
# Usage: scripts/smoke.sh path/to/whatsapp-mcp
set -euo pipefail
bin=$1
work=$(mktemp -d smoke.XXXX)
trap 'kill "$pid" 2>/dev/null || true; rm -rf "$work"' EXIT

"$bin" version
"$bin" daemon --data-dir "$work/data" --pair wait >"$work/daemon.log" 2>&1 &
pid=$!

for _ in $(seq 1 30); do
  if "$bin" status --data-dir "$work/data" >"$work/status.txt" 2>&1; then
    break
  fi
  sleep 1
done
cat "$work/status.txt"
grep -q '"logged_in": false' "$work/status.txt" || { echo "daemon did not answer"; cat "$work/daemon.log"; exit 1; }
grep -q "waiting for a pairing request" "$work/daemon.log" || { echo "daemon did not wait for pairing"; cat "$work/daemon.log"; exit 1; }
if grep -qi "scan this QR" "$work/daemon.log"; then
  echo "an unpaired daemon in wait mode must not show a QR code"
  exit 1
fi
echo "smoke test passed"
