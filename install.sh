#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
MIN_GO_MAJOR=1
MIN_GO_MINOR=26

ROOT="$(cd "$(dirname "$0")" && pwd)"

# --- helpers -----------------------------------------------------------------

info()  { echo "[info]  $*"; }
warn()  { echo "[warn]  $*" >&2; }
error() { echo "[error] $*" >&2; exit 1; }

# --- check Go ----------------------------------------------------------------

if ! command -v go &>/dev/null; then
    error "Go is not installed. Please install Go ${MIN_GO_MAJOR}.${MIN_GO_MINOR} or later."
fi

GO_VERSION="$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')"
GO_MAJOR="${GO_VERSION%%.*}"
GO_MINOR="${GO_VERSION##*.}"

if (( GO_MAJOR < MIN_GO_MAJOR || (GO_MAJOR == MIN_GO_MAJOR && GO_MINOR < MIN_GO_MINOR) )); then
    error "Go ${MIN_GO_MAJOR}.${MIN_GO_MINOR} or later is required (found ${GO_VERSION})."
fi
info "Go ${GO_VERSION} found."

# --- check gvfs-backends -----------------------------------------------------

if command -v dpkg &>/dev/null; then
    if ! dpkg -s gvfs-backends &>/dev/null 2>&1; then
        warn "gvfs-backends is not installed. Run: sudo apt install gvfs-backends fuse3"
    else
        info "gvfs-backends found."
    fi
else
    warn "Cannot verify gvfs-backends (non-Debian system). Make sure it is installed."
fi

# --- build -------------------------------------------------------------------

info "Building tp7..."
"$ROOT/build.sh"

# --- install -----------------------------------------------------------------

mkdir -p "$INSTALL_DIR"
cp "$ROOT/tp7" "$INSTALL_DIR/tp7"
info "Installed tp7 to $INSTALL_DIR/tp7"

# --- PATH hint ---------------------------------------------------------------

if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    warn "$INSTALL_DIR is not in your PATH."
    warn "Add the following line to your shell profile (~/.bashrc, ~/.zshrc, …):"
    warn "  export PATH=\"\$PATH:$INSTALL_DIR\""
fi

info "Done. Run 'tp7 -h' to get started."
