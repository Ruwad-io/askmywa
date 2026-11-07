#!/usr/bin/env bash
# Tests the Homebrew formula end to end against a fake release served on
# localhost: generates it with scripts/homebrew-formula.sh, runs brew style,
# installs it from a throwaway local tap, runs its test block, and checks
# that install-service would point the service at the stable opt path.
# Everything is removed at the end; the real service is never touched.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

version=0.0.1
tap=askmywa-ci/test
work=$(mktemp -d)
pid=""
cleanup() {
  brew uninstall --formula --force "$tap/askmywa" >/dev/null 2>&1 || true
  brew untap "$tap" >/dev/null 2>&1 || true
  [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

if brew list --formula askmywa >/dev/null 2>&1; then
  echo "error: an askmywa formula is already installed; not touching it" >&2
  exit 1
fi

rel="$work/download/v$version"
mkdir -p "$rel"
for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  os=${target%/*} arch=${target#*/}
  pkg="askmywa-$version-$os-$arch"
  mkdir -p "$work/pkg/$pkg"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$work/pkg/$pkg/askmywa" .
  if [[ $os == darwin ]]; then
    bash scripts/macos-app.sh "$work/pkg/$pkg/askmywa" "$work/pkg/$pkg" "$version" >/dev/null
    codesign --force --sign - "$work/pkg/$pkg/askmywa.app" 2>/dev/null
    rm "$work/pkg/$pkg/askmywa"
    ln -s askmywa.app/Contents/MacOS/askmywa "$work/pkg/$pkg/askmywa"
  fi
  tar -C "$work/pkg" -czf "$rel/$pkg.tar.gz" "$pkg"
done
sum="sha256sum"
command -v sha256sum >/dev/null || sum="shasum -a 256"
(cd "$rel" && $sum ./*.tar.gz > checksums.txt)

port=$((20000 + RANDOM % 20000))
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$work" >/dev/null 2>&1 &
pid=$!
for _ in $(seq 50); do
  curl -fsS -o /dev/null "http://127.0.0.1:$port/download/v$version/checksums.txt" 2>/dev/null && break
  sleep 0.1
done

brew tap-new --no-git "$tap" >/dev/null
formula="$(brew --repository "$tap")/Formula/askmywa.rb"

echo "--- brew style (formula as released)"
scripts/homebrew-formula.sh "$version" "$rel/checksums.txt" > "$formula"
brew style --formula "$tap/askmywa"

echo "--- brew install from the fake release"
scripts/homebrew-formula.sh "$version" "$rel/checksums.txt" "http://127.0.0.1:$port/download/v$version" > "$formula"
HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 brew install --formula "$tap/askmywa"
brew test "$tap/askmywa"

echo "--- the service would run the opt path, which survives upgrades"
opt="$(brew --prefix)/opt/askmywa/"
[[ $(uname -s) == Darwin ]] && opt="$(brew --prefix)/opt/askmywa/libexec/askmywa.app/Contents/MacOS/askmywa"
def=$("$(brew --prefix)/bin/askmywa" install-service --dry-run 2>/dev/null)
grep -q "$opt" <<<"$def" || { echo "FAIL: service definition does not use $opt:"; echo "$def"; exit 1; } >&2
if grep -q "/Cellar/" <<<"$def"; then
  echo "FAIL: service definition points into the Cellar" >&2
  exit 1
fi

echo "homebrew test passed"
