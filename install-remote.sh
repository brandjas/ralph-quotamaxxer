#!/usr/bin/env bash
# install-remote.sh — Install ralph-quotamaxxer without cloning the repo or Go.
# Usage: curl -fsSL https://raw.githubusercontent.com/brandjas/ralph-quotamaxxer/main/install-remote.sh | bash
set -euo pipefail

REPO="brandjas/ralph-quotamaxxer"
DEST_DIR="$HOME/.claude/ralph-quotamaxxer"
RAW_BASE="https://raw.githubusercontent.com/$REPO/main"

# --- Detect platform ---
detect_platform() {
    local os arch
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)

    case "$os" in
        linux)  ;;
        darwin) ;;
        *)      echo "Unsupported OS: $os" >&2; exit 1 ;;
    esac

    case "$arch" in
        x86_64)  arch="amd64" ;;
        amd64)   ;;
        aarch64) arch="arm64" ;;
        arm64)   ;;
        *)       echo "Unsupported architecture: $arch" >&2; exit 1 ;;
    esac

    echo "${os}-${arch}"
}

# --- Download helper (curl with wget fallback) ---
# Usage: download <url> <dest>   — save to file
#        download <url>          — write to stdout
download() {
    local url="$1" dest="${2:-}"
    if command -v curl &>/dev/null; then
        if [[ -n "$dest" ]]; then curl -fsSL -o "$dest" "$url"; else curl -fsSL "$url"; fi
    elif command -v wget &>/dev/null; then
        if [[ -n "$dest" ]]; then wget -qO "$dest" "$url"; else wget -qO- "$url"; fi
    else
        echo "Neither curl nor wget found." >&2
        exit 1
    fi
}

# --- Main ---
PLATFORM=$(detect_platform)
echo "Platform: $PLATFORM"

# Resolve version.
VERSION="${QUOTAMAXXER_VERSION:-}"
if [[ -z "$VERSION" ]]; then
    echo "Fetching latest release..."
    RELEASE_JSON=$(download "https://api.github.com/repos/$REPO/releases/latest")
    if command -v jq &>/dev/null; then
        VERSION=$(echo "$RELEASE_JSON" | jq -r '.tag_name')
    else
        VERSION=$(echo "$RELEASE_JSON" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    fi
    if [[ -z "$VERSION" ]]; then
        echo "Could not determine latest release. Set QUOTAMAXXER_VERSION to install a specific version." >&2
        exit 1
    fi
fi
echo "Version: $VERSION"

# Create directories.
mkdir -p "$DEST_DIR/bin" "$DEST_DIR/data"

# Download binary.
BINARY_URL="https://github.com/$REPO/releases/download/$VERSION/quotamaxxer-proxy-$PLATFORM"
echo "Downloading binary..."
download "$BINARY_URL" "$DEST_DIR/bin/quotamaxxer-proxy"
chmod +x "$DEST_DIR/bin/quotamaxxer-proxy"

# Download wrapper + statusline scripts.
echo "Downloading scripts..."
download "$RAW_BASE/proxy/wrapper.sh" "$DEST_DIR/bin/quotamaxxer"
chmod +x "$DEST_DIR/bin/quotamaxxer"

mkdir -p "$DEST_DIR/statusline"
download "$RAW_BASE/statusline/statusline.sh"   "$DEST_DIR/statusline/statusline.sh"
download "$RAW_BASE/statusline/parse.sh"        "$DEST_DIR/statusline/parse.sh"
download "$RAW_BASE/statusline/persist.sh"      "$DEST_DIR/statusline/persist.sh"
chmod +x "$DEST_DIR/statusline/statusline.sh"

echo ""
echo "Installed to $DEST_DIR"
echo ""
echo "Usage:"
echo "  $DEST_DIR/bin/quotamaxxer          # interactive"
echo "  $DEST_DIR/bin/quotamaxxer -p '...' # headless"
echo ""
echo "Or alias it:"
echo "  alias claude=$DEST_DIR/bin/quotamaxxer"
echo ""
echo "To enable the statusline, add this to ~/.claude/settings.json:"
echo '  "statusLine": { "type": "command", "command": "~/.claude/ralph-quotamaxxer/statusline/statusline.sh" }'
