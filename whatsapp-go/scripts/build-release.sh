#!/usr/bin/env bash
# Builds the release artifacts into dist/:
#   whatsapp-mcp-<version>-darwin-{arm64,amd64}.tar.gz   macOS, signed and notarized
#   whatsapp-mcp-<version>-linux-{amd64,arm64,armv7}.tar.gz  Linux and Raspberry Pi
#   whatsapp-mcp-<version>-windows-{amd64,arm64}.zip
#   whatsapp-mcp-<version>.mcpb   Claude Desktop extension: macOS universal binary + Windows x64
#   checksums.txt
#   install.sh        one-line installer for Linux and macOS
#   whatsapp-mcp.rb   Homebrew formula
#
# Usage: scripts/build-release.sh [version]   (default: git describe, without the leading v)
# Needs macOS (lipo, codesign) and Node.js (npx) for the extension.
#
# Signing and notarization are optional and driven by the environment:
#   SIGN_IDENTITY          a "Developer ID Application: ..." identity in the keychain.
#                          Without it the binaries are signed ad hoc (fine for local use,
#                          blocked by Gatekeeper when downloaded).
#   NOTARY_KEY_PATH        App Store Connect API key (.p8) used by notarytool,
#   NOTARY_KEY_ID          with its key ID
#   NOTARY_ISSUER_ID       and issuer ID. All three are needed to notarize; notarization
#                          requires SIGN_IDENTITY.
#   REQUIRE_NOTARIZATION=1 fail instead of shipping unnotarized binaries (release CI).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

version="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
version="${version#v}"
mcpb_cli="@anthropic-ai/mcpb@2.1.2"
dist=dist
rm -rf "$dist"
mkdir -p "$dist/bin" "$dist/mcpb/server"

notarize=0
if [[ -n "${NOTARY_KEY_PATH:-}" && -n "${NOTARY_KEY_ID:-}" && -n "${NOTARY_ISSUER_ID:-}" ]]; then
  if [[ -z "${SIGN_IDENTITY:-}" ]]; then
    echo "error: notarization needs SIGN_IDENTITY (a Developer ID Application identity)" >&2
    exit 1
  fi
  notarize=1
fi
if [[ "${REQUIRE_NOTARIZATION:-0}" == 1 && $notarize == 0 ]]; then
  echo "error: REQUIRE_NOTARIZATION=1 but SIGN_IDENTITY / NOTARY_KEY_PATH / NOTARY_KEY_ID / NOTARY_ISSUER_ID are not all set" >&2
  exit 1
fi

# sign BINARY: Developer ID with the hardened runtime and a secure timestamp
# (both required by notarization), or ad hoc when no identity is configured.
sign() {
  if [[ -n "${SIGN_IDENTITY:-}" ]]; then
    codesign --force --options runtime --timestamp --identifier com.whatsapp-mcp.cli --sign "$SIGN_IDENTITY" "$1"
  else
    codesign --force --sign - "$1"
  fi
  codesign --verify --strict "$1"
}

ldflags="-s -w -X main.version=${version}"
for arch in arm64 amd64; do
  echo "building darwin/${arch}"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$dist/bin/whatsapp-mcp-$arch" .
  sign "$dist/bin/whatsapp-mcp-$arch"
done

echo "building the universal binary"
# lipo drops the per-architecture signatures, so the result is signed again.
lipo -create -output "$dist/mcpb/server/whatsapp-mcp" "$dist/bin/whatsapp-mcp-arm64" "$dist/bin/whatsapp-mcp-amd64"
chmod +x "$dist/mcpb/server/whatsapp-mcp"
sign "$dist/mcpb/server/whatsapp-mcp"

if [[ $notarize == 1 ]]; then
  echo "notarizing (usually a few minutes; a team's first submissions can take an hour or more)"
  submission="$dist/notarize.zip"
  mkdir -p "$dist/notarize"
  cp "$dist/bin/whatsapp-mcp-arm64" "$dist/bin/whatsapp-mcp-amd64" "$dist/notarize/"
  cp "$dist/mcpb/server/whatsapp-mcp" "$dist/notarize/whatsapp-mcp-universal"
  ditto -c -k --keepParent "$dist/notarize" "$submission"
  xcrun notarytool submit "$submission" --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID" \
    --wait --timeout 2h --output-format json | tee "$dist/notarize.json"
  status=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("status",""))' "$dist/notarize.json")
  if [[ "$status" != "Accepted" ]]; then
    id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("id",""))' "$dist/notarize.json")
    echo "error: notarization status is '$status'; log follows" >&2
    [[ -n "$id" ]] && xcrun notarytool log "$id" --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID" >&2 || true
    exit 1
  fi
  # Bare executables cannot be stapled: Gatekeeper fetches the ticket online
  # the first time the binary runs.
  rm -rf "${dist:?}/notarize" "$submission" "$dist/notarize.json"
else
  echo "warning: binaries are not notarized; Gatekeeper will block them when downloaded" >&2
fi

for arch in arm64 amd64; do
  pkg="whatsapp-mcp-${version}-darwin-${arch}"
  mkdir -p "$dist/$pkg"
  cp "$dist/bin/whatsapp-mcp-$arch" "$dist/$pkg/whatsapp-mcp"
  cp README.md ../LICENSE "$dist/$pkg/"
  tar -C "$dist" -czf "$dist/$pkg.tar.gz" "$pkg"
  rm -rf "${dist:?}/$pkg"
done

echo "building Linux and Windows"
for target in linux/amd64 linux/arm64 linux/arm windows/amd64 windows/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name=$arch
  [[ $arch == arm ]] && name=armv7
  ext=""
  [[ $os == windows ]] && ext=".exe"
  pkg="whatsapp-mcp-${version}-${os}-${name}"
  mkdir -p "$dist/$pkg"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch GOARM=7 go build -trimpath -ldflags "$ldflags" -o "$dist/$pkg/whatsapp-mcp$ext" .
  cp README.md ../LICENSE "$dist/$pkg/"
  if [[ $os == windows ]]; then
    # The extension runs the x64 build on every Windows PC (ARM PCs emulate it).
    [[ $arch == amd64 ]] && cp "$dist/$pkg/whatsapp-mcp.exe" "$dist/mcpb/server/"
    (cd "$dist" && zip -qr "$pkg.zip" "$pkg")
  else
    tar -C "$dist" -czf "$dist/$pkg.tar.gz" "$pkg"
  fi
  rm -rf "${dist:?}/$pkg"
done

echo "packing the Claude Desktop extension"
python3 - "$version" extension/manifest.json "$dist/mcpb/manifest.json" <<'PY'
import json, sys
version, src, dst = sys.argv[1:]
m = json.load(open(src))
# Manifest versions must be semver; fall back for untagged builds.
parts = version.split("-")[0].split(".")
m["version"] = version if len(parts) == 3 and all(p.isdigit() for p in parts) else "0.0.0-dev"
json.dump(m, open(dst, "w"), indent=2, ensure_ascii=False)
PY
cp extension/icon.png "$dist/mcpb/"
cp ../LICENSE "$dist/mcpb/"
npx -y "$mcpb_cli" validate "$dist/mcpb/manifest.json"
npx -y "$mcpb_cli" pack "$dist/mcpb" "$dist/whatsapp-mcp-${version}.mcpb"

rm -rf "${dist:?}/bin" "${dist:?}/mcpb"
(cd "$dist" && shasum -a 256 ./*.tar.gz ./*.zip ./*.mcpb > checksums.txt)
# The one-line installer and the Homebrew formula (copied by abonur/homebrew-tap).
cp scripts/install.sh "$dist/install.sh"
bash scripts/homebrew-formula.sh "$version" "$dist/checksums.txt" > "$dist/whatsapp-mcp.rb"
echo "done:"
ls -l "$dist"
