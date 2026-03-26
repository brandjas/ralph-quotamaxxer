#!/usr/bin/env bash
# install.sh — Copy scripts into ~/.claude/ralph-quotamaxxer/ and patch settings.json.
set -euo pipefail

REPO_DIR="$(dirname "$(readlink -f "$0")")"
DEST_DIR="$HOME/.claude/ralph-quotamaxxer"
SETTINGS="$HOME/.claude/settings.json"
STATUSLINE_CMD="~/.claude/ralph-quotamaxxer/scripts/statusline.sh"

# Copy scripts
mkdir -p "$DEST_DIR/scripts" "$DEST_DIR/data"
cp "$REPO_DIR/scripts/"*.sh "$DEST_DIR/scripts/"
chmod +x "$DEST_DIR/scripts/statusline.sh"

echo "Scripts installed to $DEST_DIR/scripts/"

# Patch settings.json
if [[ ! -f "$SETTINGS" ]]; then
    echo '{}' > "$SETTINGS"
fi

jq --arg cmd "$STATUSLINE_CMD" \
   '.statusLine = { type: "command", command: $cmd }' \
   "$SETTINGS" > "$SETTINGS.tmp" && mv "$SETTINGS.tmp" "$SETTINGS"

echo "settings.json patched: statusLine → $STATUSLINE_CMD"
echo "Done."
