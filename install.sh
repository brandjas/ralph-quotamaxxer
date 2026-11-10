#!/usr/bin/env bash
# install.sh — Build and install quotamaxxer into ~/.claude/ralph-quotamaxxer/.
set -euo pipefail

REPO_DIR="$(dirname "$(readlink -f "$0")")"
DEST_DIR="$HOME/.claude/ralph-quotamaxxer"

# Build binary.
echo "Building quotamaxxer..."
mkdir -p "$DEST_DIR/bin" "$DEST_DIR/data"
# CGO_ENABLED=0 gives a static binary that runs on musl (Alpine) and old glibc.
(cd "$REPO_DIR/cmd/quotamaxxer" && CGO_ENABLED=0 go build -o "$DEST_DIR/bin/quotamaxxer" .)
echo "Installed to $DEST_DIR/bin/quotamaxxer"
echo ""
echo "Interactive sessions and background agents are tracked via the statusline."
echo "Add this to ~/.claude/settings.json:"
echo '  "statusLine": { "type": "command", "command": "~/.claude/ralph-quotamaxxer/bin/quotamaxxer statusline" }'
echo "Or keep your own statusline and pipe its input to:"
echo '  ~/.claude/ralph-quotamaxxer/bin/quotamaxxer statusline --persist-only'
echo ""
echo "Done."
