#!/usr/bin/env bash
# Builds the release artifacts into dist/:
#   askmywa-<version>-darwin-{arm64,amd64}.tar.gz   macOS askmywa.app, signed, notarized and stapled
#   askmywa-<version>-linux-{amd64,arm64,armv7}.tar.gz  Linux and Raspberry Pi
#   askmywa-<version>-windows-{amd64,arm64}.zip
#   askmywa-<version>.mcpb   Claude Desktop extension: macOS universal askmywa.app + Windows x64
#   askmywa.mcpb             the same, under a stable name for direct download links
#   checksums.txt
#   install.sh        one-line installer for Linux and macOS
#   askmywa.rb   Homebrew formula
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
#   WINDOWS_BIN_DIR        a folder holding askmywa-windows-{amd64,arm64}.exe already built
#                          for this version by scripts/build-windows.sh and code-signed
#                          (the release workflow signs them on a Windows runner). Without it
#                          the Windows programs are built here, unsigned.
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

# sign APP: signs askmywa.app with Developer ID, the hardened runtime and a
# secure timestamp (all required by notarization), or ad hoc when no
# identity is configured. The bundle identifier is com.askmywa.app.
sign() {
  if [[ -n "${SIGN_IDENTITY:-}" ]]; then
    codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$1"
  else
    codesign --force --sign - "$1"
  fi
  codesign --verify --deep --strict "$1"
}

# On macOS every build ships inside askmywa.app (scripts/macos-app.sh), so
# that System Settings shows the logo for the background service.
ldflags="-s -w -X main.version=${version}"
for arch in arm64 amd64; do
  echo "building darwin/${arch}"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$dist/bin/askmywa-$arch" .
  bash scripts/macos-app.sh "$dist/bin/askmywa-$arch" "$dist/app-$arch" "$version" >/dev/null
  sign "$dist/app-$arch/askmywa.app"
done

echo "building the universal app for the extension"
lipo -create -output "$dist/bin/askmywa-universal" "$dist/bin/askmywa-arm64" "$dist/bin/askmywa-amd64"
bash scripts/macos-app.sh "$dist/bin/askmywa-universal" "$dist/mcpb/server" "$version" >/dev/null
sign "$dist/mcpb/server/askmywa.app"
apps=("$dist/app-arm64/askmywa.app" "$dist/app-amd64/askmywa.app" "$dist/mcpb/server/askmywa.app")

if [[ $notarize == 1 ]]; then
  echo "notarizing (usually a few minutes; a team's first submissions can take an hour or more)"
  submission="$dist/notarize.zip"
  for a in arm64 amd64 universal; do mkdir -p "$dist/notarize/$a"; done
  ditto "${apps[0]}" "$dist/notarize/arm64/askmywa.app"
  ditto "${apps[1]}" "$dist/notarize/amd64/askmywa.app"
  ditto "${apps[2]}" "$dist/notarize/universal/askmywa.app"
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
  # Apps can carry their ticket, so macOS does not need to go online the
  # first time they run.
  for app in "${apps[@]}"; do xcrun stapler staple "$app"; done
  rm -rf "${dist:?}/notarize" "$submission" "$dist/notarize.json"
else
  echo "warning: the apps are not notarized; Gatekeeper will block them when downloaded" >&2
fi

for arch in arm64 amd64; do
  pkg="askmywa-${version}-darwin-${arch}"
  mkdir -p "$dist/$pkg"
  ditto "$dist/app-$arch/askmywa.app" "$dist/$pkg/askmywa.app"
  # The command line keeps working from the archive: askmywa -> the app's binary.
  ln -s askmywa.app/Contents/MacOS/askmywa "$dist/$pkg/askmywa"
  cp README.md ../LICENSE "$dist/$pkg/"
  tar -C "$dist" -czf "$dist/$pkg.tar.gz" "$pkg"
  rm -rf "${dist:?}/$pkg" "${dist:?}/app-$arch"
done

if [[ -n "${WINDOWS_BIN_DIR:-}" ]]; then
  windows_bin="$WINDOWS_BIN_DIR"
  for arch in amd64 arm64; do
    if [[ ! -f "$windows_bin/askmywa-windows-$arch.exe" ]]; then
      echo "error: WINDOWS_BIN_DIR has no askmywa-windows-$arch.exe" >&2
      exit 1
    fi
  done
  echo "using the signed Windows programs from $windows_bin"
else
  windows_bin="$dist/bin/windows"
  bash scripts/build-windows.sh "$version" "$windows_bin"
  echo "warning: the Windows programs are not code-signed; SmartScreen may warn when they are downloaded" >&2
fi

echo "building Linux and packing Windows"
for target in linux/amd64 linux/arm64 linux/arm windows/amd64 windows/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name=$arch
  [[ $arch == arm ]] && name=armv7
  pkg="askmywa-${version}-${os}-${name}"
  mkdir -p "$dist/$pkg"
  if [[ $os == windows ]]; then
    cp "$windows_bin/askmywa-windows-$arch.exe" "$dist/$pkg/askmywa.exe"
  else
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch GOARM=7 go build -trimpath -ldflags "$ldflags" -o "$dist/$pkg/askmywa" .
  fi
  cp README.md ../LICENSE "$dist/$pkg/"
  if [[ $os == windows ]]; then
    # The extension runs the x64 build on every Windows PC (ARM PCs emulate it).
    [[ $arch == amd64 ]] && cp "$dist/$pkg/askmywa.exe" "$dist/mcpb/server/"
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
npx -y "$mcpb_cli" pack "$dist/mcpb" "$dist/askmywa-${version}.mcpb"
# The same file without the version, so /releases/latest/download/askmywa.mcpb
# always serves the newest extension (the website's download button).
cp "$dist/askmywa-${version}.mcpb" "$dist/askmywa.mcpb"

rm -rf "${dist:?}/bin" "${dist:?}/mcpb"
(cd "$dist" && shasum -a 256 ./*.tar.gz ./*.zip ./*.mcpb > checksums.txt)
# The one-line installer and the Homebrew formula (copied by abonur/homebrew-tap).
cp scripts/install.sh "$dist/install.sh"
bash scripts/homebrew-formula.sh "$version" "$dist/checksums.txt" > "$dist/askmywa.rb"
echo "done:"
ls -l "$dist"
