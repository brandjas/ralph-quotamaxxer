#!/usr/bin/env bash
# install.sh — Copy scripts into ~/.claude/ralph-quotamaxxer/ and patch settings.json.
set -euo pipefail

REPO_DIR="$(dirname "$(readlink -f "$0")")"
DEST_DIR="$HOME/.claude/ralph-quotamaxxer"
SETTINGS="$HOME/.claude/settings.json"
STATUSLINE_CMD="~/.claude/ralph-quotamaxxer/statusline/statusline.sh"

# Build proxy binary.
echo "Building proxy..."
mkdir -p "$DEST_DIR/bin"
(cd "$REPO_DIR/proxy" && go build -o "$DEST_DIR/bin/ralph-quotamaxxer/proxy" .)
echo "Proxy built → $DEST_DIR/bin/ralph-quotamaxxer/proxy"

# Copy statusline scripts + wrapper.
mkdir -p "$DEST_DIR/statusline" "$DEST_DIR/data"
cp "$REPO_DIR/statusline/"*.sh "$DEST_DIR/statusline/"
chmod +x "$DEST_DIR/statusline/statusline.sh"
cp "$REPO_DIR/proxy/wrapper.sh" "$DEST_DIR/bin/quotamaxxer"
chmod +x "$DEST_DIR/bin/quotamaxxer"

echo "Statusline installed to $DEST_DIR/statusline/"
echo "Wrapper installed to $DEST_DIR/bin/quotamaxxer"

# Patch settings.json
if [[ ! -f "$SETTINGS" ]]; then
    echo '{}' > "$SETTINGS"
fi

jq --arg cmd "$STATUSLINE_CMD" \
   '.statusLine = { type: "command", command: $cmd }' \
   "$SETTINGS" > "$SETTINGS.tmp" && mv "$SETTINGS.tmp" "$SETTINGS"

echo "settings.json patched: statusLine → $STATUSLINE_CMD"
echo "Done."
