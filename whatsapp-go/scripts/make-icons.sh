#!/usr/bin/env bash
# Regenerates every icon from assets/logo.svg and assets/logo-macos.svg.
# Needs macOS (iconutil) and Google Chrome (to render the SVGs). The outputs
# are committed, so builds and CI need neither.
#
#   assets/askmywa.icns            macOS app icon (askmywa.app)
#   assets/icon-256.png            source of the Windows .exe icon
#   extension/icon.png             Claude Desktop extension, 512x512
#   ../site/assets/favicon.svg     site favicon (copy of logo.svg)
#   ../site/assets/icon-*.png      site and MCP server icons
#   ../site/assets/apple-touch-icon.png
#
# The Windows resources (rsrc_windows_*.syso) are made from icon-256.png by
# scripts/make-windows-resources.sh.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# render SVG SIZE OUT: rasterizes an SVG with a transparent background.
render() {
  printf '<!doctype html><html><body style="margin:0;background:transparent"><img src="file://%s" width="%s" height="%s"></body></html>' "$PWD/$1" "$2" "$2" > "$work/r.html"
  "$chrome" --headless=new --disable-gpu --hide-scrollbars --default-background-color=00000000 \
    --force-device-scale-factor=1 --window-size="$2,$2" --screenshot="$3" "file://$work/r.html" >/dev/null 2>&1
}

set_dir="$work/askmywa.iconset"
mkdir -p "$set_dir"
for s in 16 32 128 256 512; do
  render assets/logo-macos.svg "$s" "$set_dir/icon_${s}x${s}.png"
  render assets/logo-macos.svg "$((s * 2))" "$set_dir/icon_${s}x${s}@2x.png"
done
iconutil -c icns -o assets/askmywa.icns "$set_dir"

render assets/logo.svg 256 assets/icon-256.png
render assets/logo.svg 512 extension/icon.png
cp assets/logo.svg ../site/assets/favicon.svg
for s in 48 192 512; do render assets/logo.svg "$s" "../site/assets/icon-$s.png"; done
render assets/logo.svg 180 ../site/assets/apple-touch-icon.png
ls -l assets/askmywa.icns assets/icon-256.png extension/icon.png ../site/assets/icon-*.png ../site/assets/apple-touch-icon.png
