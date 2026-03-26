#!/usr/bin/env bash
# install.sh — Copy scripts into ~/.claude/ralph-quotamaxxer/ and patch settings.json.
set -euo pipefail

REPO_DIR="$(dirname "$(readlink -f "$0")")"
DEST_DIR="$HOME/.claude/ralph-quotamaxxer"
# Build proxy binary.
echo "Building proxy..."
mkdir -p "$DEST_DIR/bin"
(cd "$REPO_DIR/proxy" && go build -o "$DEST_DIR/bin/quotamaxxer-proxy" .)
echo "Proxy built -> $DEST_DIR/bin/quotamaxxer-proxy"

# Copy wrapper + statusline scripts.
mkdir -p "$DEST_DIR/statusline" "$DEST_DIR/data"
cp "$REPO_DIR/proxy/wrapper.sh" "$DEST_DIR/bin/quotamaxxer"
chmod +x "$DEST_DIR/bin/quotamaxxer"
cp "$REPO_DIR/statusline/"*.sh "$DEST_DIR/statusline/"
chmod +x "$DEST_DIR/statusline/statusline.sh"

echo "Wrapper installed to $DEST_DIR/bin/quotamaxxer"
echo "Statusline installed to $DEST_DIR/statusline/"
echo ""
echo "To enable the statusline, add this to ~/.claude/settings.json:"
echo '  "statusLine": { "type": "command", "command": "~/.claude/ralph-quotamaxxer/statusline/statusline.sh" }'
echo ""
echo "Done."
