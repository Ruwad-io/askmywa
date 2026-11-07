#!/usr/bin/env bash
# Wraps a macOS askmywa binary in askmywa.app, so that macOS shows the logo
# and the name "askmywa" for the background service (System Settings >
# General > Login Items) instead of a bare "exec" item. The app has no
# window and no Dock icon (LSUIElement); the binary works as before from
# askmywa.app/Contents/MacOS/askmywa.
#
# Usage: scripts/macos-app.sh BINARY OUT_DIR VERSION
#   Creates OUT_DIR/askmywa.app. Signing is left to the caller.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

binary="${1:?binary}"
out="${2:?output directory}"
version="${3:?version}"
version="${version#v}"
# CFBundleVersion must be numbers and dots.
build=$(printf '%s' "${version%%-*}" | tr -cd '0-9.')
[[ -n "$build" ]] || build=0

app="$out/askmywa.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$binary" "$app/Contents/MacOS/askmywa"
chmod 755 "$app/Contents/MacOS/askmywa"
cp assets/askmywa.icns "$app/Contents/Resources/askmywa.icns"
cat > "$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>askmywa</string>
    <key>CFBundleDisplayName</key>
    <string>askmywa</string>
    <key>CFBundleIdentifier</key>
    <string>com.askmywa.app</string>
    <key>CFBundleExecutable</key>
    <string>askmywa</string>
    <key>CFBundleIconFile</key>
    <string>askmywa</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${version}</string>
    <key>CFBundleVersion</key>
    <string>${build}</string>
    <key>LSMinimumSystemVersion</key>
    <string>12.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHumanReadableCopyright</key>
    <string>askmywa, WhatsApp MCP server. MIT License. https://askmywa.com</string>
</dict>
</plist>
EOF
plutil -lint "$app/Contents/Info.plist" >/dev/null
echo "$app"
