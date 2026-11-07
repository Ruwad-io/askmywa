#!/bin/sh
# Installs askmywa on Linux (including Raspberry Pi) and macOS:
#
#   curl -fsSL https://github.com/abonur/askmywa/releases/latest/download/install.sh | sh
#
# It downloads the release for this machine, checks its SHA-256 against the
# release's checksums.txt, puts the binary in ~/.local/bin, installs the
# background service and offers to link WhatsApp. Running it again upgrades.
#
# Options, after `sh -s --` when piped:
#   --no-service      only install the binary
#   --no-link         do not offer to link WhatsApp at the end
#   --dir DIR         install the binary in DIR instead of ~/.local/bin
#   --version X.Y.Z   install that version instead of the latest
#
# On macOS the program is askmywa.app, installed in ~/Applications
# (ASKMYWA_APP_DIR), and the command is a link to the binary inside it.
#
# Environment: ASKMYWA_INSTALL_DIR, ASKMYWA_APP_DIR, ASKMYWA_VERSION, and
# ASKMYWA_RELEASES (base URL of the releases, for testing).
#
# Everything is inside main, so a download cut short runs nothing.

set -eu

main() {
  repo="abonur/askmywa"
  releases="${ASKMYWA_RELEASES:-https://github.com/$repo/releases}"
  dir="${ASKMYWA_INSTALL_DIR:-$HOME/.local/bin}"
  version="${ASKMYWA_VERSION:-}"
  service=1
  offer_link=1

  while [ $# -gt 0 ]; do
    case "$1" in
      --no-service) service=0 ;;
      --no-link) offer_link=0 ;;
      --dir) [ $# -ge 2 ] || die "--dir needs a folder"; dir="$2"; shift ;;
      --version) [ $# -ge 2 ] || die "--version needs a version"; version="$2"; shift ;;
      -h|--help) say "usage: install.sh [--no-service] [--no-link] [--dir DIR] [--version X.Y.Z]"; exit 0 ;;
      *) die "unknown option $1 (see --help)" ;;
    esac
    shift
  done

  os=$(detect_os)
  arch=$(detect_arch "$os")
  need tar
  if command -v curl >/dev/null 2>&1; then
    fetch=curl
  elif command -v wget >/dev/null 2>&1; then
    fetch=wget
  else
    die "curl or wget is needed"
  fi

  if [ -z "$version" ]; then
    version=$(latest_version) || die "could not find the latest release on $releases"
  fi
  version="${version#v}"
  pkg="askmywa-$version-$os-$arch"
  say "Installing askmywa $version for $os/$arch"

  tmp=$(mktemp -d 2>/dev/null || mktemp -d -t askmywa)
  trap 'rm -rf "$tmp"' EXIT INT TERM
  download "$releases/download/v$version/$pkg.tar.gz" "$tmp/$pkg.tar.gz"
  download "$releases/download/v$version/checksums.txt" "$tmp/checksums.txt"
  verify "$tmp" "$pkg.tar.gz"
  tar -xzf "$tmp/$pkg.tar.gz" -C "$tmp"

  mkdir -p "$dir"
  bin="$dir/askmywa"
  if [ -d "$tmp/$pkg/askmywa.app" ]; then
    # macOS: the app (which gives the background service its name and logo
    # in System Settings) goes to ~/Applications; the command links into it.
    apps="${ASKMYWA_APP_DIR:-$HOME/Applications}"
    mkdir -p "$apps"
    rm -rf "$apps/askmywa.app.new" "$apps/askmywa.app.old"
    cp -R "$tmp/$pkg/askmywa.app" "$apps/askmywa.app.new"
    # Swap through renames so a running daemon keeps its old files.
    if [ -d "$apps/askmywa.app" ]; then mv "$apps/askmywa.app" "$apps/askmywa.app.old"; fi
    mv "$apps/askmywa.app.new" "$apps/askmywa.app"
    rm -rf "$apps/askmywa.app.old"
    ln -sf "$apps/askmywa.app/Contents/MacOS/askmywa" "$bin.new"
    mv -f "$bin.new" "$bin"
    say "Installed $apps/askmywa.app"
    say "Linked $bin"
  else
    # Replace through a rename so a running daemon keeps its old file.
    cp "$tmp/$pkg/askmywa" "$bin.new"
    chmod 755 "$bin.new"
    mv -f "$bin.new" "$bin"
    say "Installed $bin"
  fi
  # Before the rename the command was whatsapp-mcp: point that name at the
  # new binary, so MCP client configs that use it keep working.
  if [ -f "$dir/whatsapp-mcp" ] && [ ! -L "$dir/whatsapp-mcp" ]; then
    ln -sf askmywa "$dir/whatsapp-mcp"
    say "Replaced $dir/whatsapp-mcp with a link to askmywa"
  fi
  "$bin" version >/dev/null || die "$bin does not run on this machine"

  if [ "$service" = 1 ]; then
    install_service "$bin"
  fi

  case ":$PATH:" in
    *":$dir:"*) ;;
    *) say ""
       say "Add $dir to your PATH to type askmywa without its full path, for example:"
       say "  echo 'export PATH=\"$dir:\$PATH\"' >> ~/.profile" ;;
  esac

  next_steps "$bin"
}

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is needed"; }

detect_os() {
  case "$(uname -s)" in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    MINGW*|MSYS*|CYGWIN*) die "on Windows, install the Claude Desktop extension or the windows zip from https://github.com/abonur/askmywa/releases" ;;
    *) die "unsupported system $(uname -s); builds exist for Linux, macOS and Windows" ;;
  esac
}

detect_arch() {
  m=$(uname -m)
  # A shell running under Rosetta reports x86_64 on Apple Silicon.
  if [ "$1" = darwin ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" = 1 ]; then
    m=arm64
  fi
  case "$m" in
    x86_64|amd64) echo amd64 ;;
    # A 64-bit kernel runs the arm64 build even with a 32-bit Raspberry Pi OS.
    aarch64|arm64) echo arm64 ;;
    armv7*|armv8l) echo armv7 ;;
    armv6*) die "this ARMv6 board (Raspberry Pi 1 or Zero) is not supported; a Pi Zero 2, 3, 4 or 5 works" ;;
    *) die "unsupported processor $m" ;;
  esac
}

latest_version() {
  # /releases/latest redirects to /releases/tag/vX.Y.Z.
  if [ "$fetch" = curl ]; then
    url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$releases/latest")
  else
    url=$(wget -q --max-redirect=5 -S --spider "$releases/latest" 2>&1 | sed -n 's/^ *Location: *//p' | tail -n 1 | tr -d '\r')
  fi
  tag=${url##*/}
  case "$tag" in
    v[0-9]*) echo "${tag#v}" ;;
    *) return 1 ;;
  esac
}

download() {
  if [ "$fetch" = curl ]; then
    curl -fsSL --retry 3 -o "$2" "$1" || die "download failed: $1"
  else
    wget -q --tries=3 -O "$2" "$1" || die "download failed: $1"
  fi
}

verify() {
  want=$(awk -v f="$2" '{ n = $2; sub(/^\.\//, "", n); sub(/^\*/, "", n); if (n == f) print $1 }' "$1/checksums.txt")
  [ -n "$want" ] || die "$2 is not listed in checksums.txt"
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$1/$2" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "$1/$2" | awk '{print $1}')
  else
    die "sha256sum or shasum is needed to check the download"
  fi
  [ "$got" = "$want" ] || die "checksum mismatch for $2: the download is corrupt or was tampered with"
  say "Checksum OK"
}

install_service() {
  if [ "$os" = linux ] && ! command -v systemctl >/dev/null 2>&1; then
    say "No systemd here: start the daemon yourself with \"$1 daemon\"."
    service=0
    return
  fi
  say "Installing the background service"
  if ! "$1" install-service; then
    say "The service could not be installed; see the message above. The binary is installed."
    service=0
  fi
}

linked() {
  "$1" status 2>/dev/null | grep -q '"logged_in": true'
}

next_steps() {
  bin="$1"
  if [ "$service" = 1 ] && ! linked "$bin"; then
    # Piped into sh, stdin is the script: ask on the terminal instead.
    if [ "$offer_link" = 1 ] && [ -t 1 ] && (: </dev/tty) 2>/dev/null; then
      say ""
      say "Now link your WhatsApp account."
      "$bin" link </dev/tty || say "Not linked yet. Run \"$bin link\" when you are ready."
    else
      say ""
      say "Link your WhatsApp account with: $bin link"
    fi
  fi
  say ""
  say "To use it from an MCP client, the command is: $bin serve"
  if [ "$os" = linux ]; then
    say "From Claude Desktop on another computer, over SSH (with an SSH key):"
    say "  \"command\": \"ssh\", \"args\": [\"$(id -un)@$(hostname)\", \"$bin\", \"serve\"]"
  fi
  say "Documentation: https://github.com/abonur/askmywa"
}

main "$@"
