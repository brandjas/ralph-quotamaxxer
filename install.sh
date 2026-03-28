#!/usr/bin/env bash
# install.sh — Build and install quotamaxxer into ~/.claude/ralph-quotamaxxer/.
set -euo pipefail

REPO_DIR="$(dirname "$(readlink -f "$0")")"
DEST_DIR="$HOME/.claude/ralph-quotamaxxer"

# Build binary.
echo "Building quotamaxxer..."
mkdir -p "$DEST_DIR/bin"
(cd "$REPO_DIR/proxy" && go build -o "$DEST_DIR/bin/quotamaxxer" .)
echo "Binary built -> $DEST_DIR/bin/quotamaxxer"

# Copy statusline scripts.
mkdir -p "$DEST_DIR/statusline" "$DEST_DIR/data"
cp "$REPO_DIR/statusline/statusline.sh" "$DEST_DIR/statusline/"
cp "$REPO_DIR/statusline/parse.sh" "$DEST_DIR/statusline/"
chmod +x "$DEST_DIR/statusline/statusline.sh"

echo "Statusline installed to $DEST_DIR/statusline/"
echo ""
echo "To enable the statusline, add this to ~/.claude/settings.json:"
echo '  "statusLine": { "type": "command", "command": "~/.claude/ralph-quotamaxxer/statusline/statusline.sh" }'
echo ""
echo "Done."
