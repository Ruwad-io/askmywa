#!/usr/bin/env bash
# Builds the release artifacts into dist/:
#   whatsapp-mcp-<version>-darwin-arm64.tar.gz
#   whatsapp-mcp-<version>-darwin-amd64.tar.gz
#   whatsapp-mcp-<version>.mcpb          (Claude Desktop extension, universal binary)
#   checksums.txt
#
# Usage: scripts/build-release.sh [version]   (default: git describe, without the leading v)
# Needs macOS (lipo) and Node.js (npx) for the extension.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

version="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
version="${version#v}"
mcpb_cli="@anthropic-ai/mcpb@2.1.2"
dist=dist
rm -rf "$dist"
mkdir -p "$dist/bin" "$dist/mcpb/server"

ldflags="-s -w -X main.version=${version}"
for arch in arm64 amd64; do
  echo "building darwin/${arch}"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$dist/bin/whatsapp-mcp-$arch" .
  pkg="whatsapp-mcp-${version}-darwin-${arch}"
  mkdir -p "$dist/$pkg"
  cp "$dist/bin/whatsapp-mcp-$arch" "$dist/$pkg/whatsapp-mcp"
  cp README.md ../LICENSE "$dist/$pkg/"
  tar -C "$dist" -czf "$dist/$pkg.tar.gz" "$pkg"
  rm -rf "${dist:?}/$pkg"
done

echo "building the universal binary"
lipo -create -output "$dist/mcpb/server/whatsapp-mcp" "$dist/bin/whatsapp-mcp-arm64" "$dist/bin/whatsapp-mcp-amd64"
chmod +x "$dist/mcpb/server/whatsapp-mcp"
# Go signs arm64 binaries ad hoc; lipo drops that signature, so sign the result again.
codesign --force --sign - "$dist/mcpb/server/whatsapp-mcp"

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
(cd "$dist" && shasum -a 256 ./*.tar.gz ./*.mcpb > checksums.txt)
echo "done:"
ls -l "$dist"
