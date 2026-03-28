#!/usr/bin/env bash
# install.sh — Build and install quotamaxxer into ~/.claude/ralph-quotamaxxer/.
set -euo pipefail

REPO_DIR="$(dirname "$(readlink -f "$0")")"
DEST_DIR="$HOME/.claude/ralph-quotamaxxer"

# Build binary.
echo "Building quotamaxxer..."
mkdir -p "$DEST_DIR/bin" "$DEST_DIR/data"
(cd "$REPO_DIR/cmd/quotamaxxer" && go build -o "$DEST_DIR/bin/quotamaxxer" .)
echo "Installed to $DEST_DIR/bin/quotamaxxer"
echo ""
echo "To enable the statusline, add this to ~/.claude/settings.json:"
echo '  "statusLine": { "type": "command", "command": "~/.claude/ralph-quotamaxxer/bin/quotamaxxer statusline" }'
echo ""
echo "Done."
