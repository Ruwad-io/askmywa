#!/usr/bin/env bash
# Tests scripts/install.sh against a fake release served on localhost:
# latest-version lookup, download, checksum check, install, upgrade, and
# refusal of a tampered archive. Never touches the real service.
#
# Usage: scripts/test-install.sh [curl|wget]   (default: curl)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

fetcher="${1:-curl}"
work=$(mktemp -d)
pid=""
cleanup() {
  [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

os=$(go env GOOS)
arch=$(go env GOARCH)
[[ $arch == arm ]] && arch=armv7

# fake_release VERSION: a release laid out like GitHub's download URLs.
fake_release() {
  local v=$1 pkg="whatsapp-mcp-$1-$os-$arch"
  mkdir -p "$work/pkg/$pkg" "$work/rel/download/v$v"
  CGO_ENABLED=0 go build -ldflags "-X main.version=$v" -o "$work/pkg/$pkg/whatsapp-mcp" .
  tar -C "$work/pkg" -czf "$work/rel/download/v$v/$pkg.tar.gz" "$pkg"
  local sum="sha256sum"
  command -v sha256sum >/dev/null || sum="shasum -a 256"
  (cd "$work/rel/download/v$v" && $sum ./*.tar.gz > checksums.txt)
}

cat > "$work/server.py" <<'PY'
import http.server, os, sys
root, port = sys.argv[1], int(sys.argv[2])
class H(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **k): super().__init__(*a, directory=root, **k)
    def log_message(self, *a): pass
    def do_HEAD(self): self.route() or super().do_HEAD()
    def do_GET(self): self.route() or super().do_GET()
    def route(self):
        if self.path.rstrip("/").endswith("/latest"):
            latest = open(os.path.join(root, "LATEST")).read().strip()
            self.send_response(302); self.send_header("Location", "/tag/v" + latest); self.end_headers(); return True
        if "/tag/" in self.path:
            self.send_response(200); self.send_header("Content-Length", "0"); self.end_headers(); return True
http.server.ThreadingHTTPServer(("127.0.0.1", port), H).serve_forever()
PY

port=$((20000 + RANDOM % 20000))
fake_release 1.0.0
echo 1.0.0 > "$work/rel/LATEST"
python3 "$work/server.py" "$work/rel" "$port" &
pid=$!
for _ in $(seq 50); do
  curl -fsS -o /dev/null "http://127.0.0.1:$port/LATEST" 2>/dev/null && break
  sleep 0.1
done

# Hide curl to exercise the wget path.
path="$PATH"
if [[ $fetcher == wget ]]; then
  mkdir -p "$work/nocurl"
  for d in ${PATH//:/ }; do
    for f in "$d"/*; do
      n=$(basename "$f")
      [[ $n == curl || -e "$work/nocurl/$n" ]] || ln -s "$f" "$work/nocurl/$n" 2>/dev/null || true
    done
  done
  path="$work/nocurl"
fi

run() {
  PATH="$path" WHATSAPP_MCP_RELEASES="http://127.0.0.1:$port" sh -s -- --no-service --dir "$work/bin" "$@" < scripts/install.sh
}

echo "--- install the latest release ($fetcher)"
run
"$work/bin/whatsapp-mcp" version | grep -qx "whatsapp-mcp 1.0.0"

echo "--- upgrade"
fake_release 1.1.0
echo 1.1.0 > "$work/rel/LATEST"
run
"$work/bin/whatsapp-mcp" version | grep -qx "whatsapp-mcp 1.1.0"

echo "--- tampered archive is refused"
sums="$work/rel/download/v1.1.0/checksums.txt"
awk '{ $1 = "0000000000000000000000000000000000000000000000000000000000000000"; print $1 "  " $2 }' "$sums" > "$sums.bad" && mv "$sums.bad" "$sums"
if out=$(run --dir "$work/bin-bad" 2>&1); then
  echo "FAIL: tampered archive was installed" >&2
  exit 1
fi
grep -q "checksum mismatch" <<<"$out"
[[ ! -e "$work/bin-bad/whatsapp-mcp" ]]

echo "install test passed ($fetcher)"
