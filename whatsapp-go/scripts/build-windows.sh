#!/usr/bin/env bash
# Builds the Windows programs: OUT_DIR/askmywa-windows-amd64.exe and
# OUT_DIR/askmywa-windows-arm64.exe, with the logo and file description from
# the committed rsrc_windows_*.syso files.
#
# Usage: scripts/build-windows.sh VERSION OUT_DIR
#
# build-release.sh calls it for unsigned builds. The release workflow calls
# it on a Windows runner, signs the two files with Azure Artifact Signing and
# hands them to build-release.sh through WINDOWS_BIN_DIR.
set -euo pipefail
version="${1:?version}"
version="${version#v}"
# OUT_DIR is relative to the caller's folder, not to whatsapp-go.
mkdir -p "${2:?output directory}"
out=$(cd "$2" && pwd)
cd "$(dirname "${BASH_SOURCE[0]}")/.."

for arch in amd64 arm64; do
  echo "building windows/${arch}"
  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=${version}" -o "$out/askmywa-windows-$arch.exe" .
done
