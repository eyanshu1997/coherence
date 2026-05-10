#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COHERENCE_HOME="$SCRIPT_DIR"
CLAUDE_DIR="$HOME/.claude"
DATA_DIR="${DOC_DATA_DIR:-$HOME/.coherence/data}"
BIN_DIR="$COHERENCE_HOME/bin"
SERVICE_NAME="coherence-server"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
SETTINGS_FILE="$CLAUDE_DIR/settings.json"

log() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarn:\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# ── 1. Build Go binaries ───────────────────────────────────────────────────

log "Building Go binaries..."
if ! command -v go &>/dev/null; then
  die "Go not found in PATH. Install Go 1.22+ from https://go.dev/dl/"
fi
cd "$COHERENCE_HOME"
make build
log "Binaries built in $BIN_DIR/"

# ── 2. Create data directory ───────────────────────────────────────────────

log "Ensuring data directory: $DATA_DIR"
mkdir -p "$DATA_DIR"
chmod 755 "$DATA_DIR"

# ── 3. Install systemd service (Linux only) ────────────────────────────────

if command -v systemctl &>/dev/null && [ -d /etc/systemd/system ]; then
  log "Installing systemd service..."
  CUR_USER="$(id -un)"
  sed -e "s|YOUR_USERNAME|$CUR_USER|g" \
      -e "s|COHERENCE_HOME_PLACEHOLDER|$COHERENCE_HOME|g" \
      "$COHERENCE_HOME/scripts/coherence-server.service" \
      | sudo tee "$SERVICE_FILE" > /dev/null
  sudo systemctl daemon-reload
  sudo systemctl enable "$SERVICE_NAME"
  if sudo systemctl is-active --quiet "$SERVICE_NAME"; then
    sudo systemctl restart "$SERVICE_NAME"
    log "Service restarted."
  else
    sudo systemctl start "$SERVICE_NAME"
    log "Service started."
  fi
else
  warn "systemd not available — start the server manually:"
  warn "  COHERENCE_HOME=$COHERENCE_HOME $BIN_DIR/coherence-server"
fi

# ── 4. Copy slash commands to ~/.claude/commands/ ─────────────────────────

COMMANDS_DIR="$CLAUDE_DIR/commands"
mkdir -p "$COMMANDS_DIR"
if [ -d "$COHERENCE_HOME/dotclaude/commands" ]; then
  log "Installing slash commands to $COMMANDS_DIR/"
  cp -r "$COHERENCE_HOME/dotclaude/commands/." "$COMMANDS_DIR/"
fi

# ── 5. Merge hook settings into ~/.claude/settings.json ───────────────────

log "Merging hook settings into $SETTINGS_FILE..."
mkdir -p "$CLAUDE_DIR"

MCP_CMD="$BIN_DIR/coherence-mcp-log"
BASH_CMD="$BIN_DIR/coherence-bash-log"
COMPACT_CMD="node \"$COHERENCE_HOME/scripts/hooks/suggest-compact.js\""

if ! command -v jq &>/dev/null; then
  warn "jq not found — skipping automatic settings.json merge."
  warn "Add these hooks manually to $SETTINGS_FILE:"
  cat "$COHERENCE_HOME/dotclaude/settings.json"
else
  # Create settings.json if it doesn't exist
  if [ ! -f "$SETTINGS_FILE" ]; then
    echo '{}' > "$SETTINGS_FILE"
  fi

  # Merge: add/replace hooks for mcp__ (PostToolUse), Bash (PostToolUse), Edit|Write (PreToolUse)
  TMP="$(mktemp)"
  jq --arg mcp "$MCP_CMD" \
     --arg bash_cmd "$BASH_CMD" \
     --arg compact "$COMPACT_CMD" '
    # Ensure top-level keys exist
    .hooks //= {}
    | .hooks.PostToolUse //= []
    | .hooks.PreToolUse //= []

    # Remove existing coherence hooks by matcher, then append fresh ones
    | .hooks.PostToolUse = (
        [.hooks.PostToolUse[] | select(.matcher != "mcp__" and .matcher != "Bash")]
        + [
            {"matcher":"mcp__","hooks":[{"type":"command","command":$mcp}]},
            {"matcher":"Bash","hooks":[{"type":"command","command":$bash_cmd}]}
          ]
      )
    | .hooks.PreToolUse = (
        [.hooks.PreToolUse[] | select(.matcher != "Edit|Write")]
        + [{"matcher":"Edit|Write","hooks":[{"type":"command","command":$compact}]}]
      )
  ' "$SETTINGS_FILE" > "$TMP" && mv "$TMP" "$SETTINGS_FILE"
  log "settings.json updated."
fi

# ── 6. Done ────────────────────────────────────────────────────────────────

log "Setup complete!"
echo ""
echo "  Server:   $(grep -E '^(DOC_HOST|DOC_PORT|DOC_SCHEME)' "$COHERENCE_HOME/.env" 2>/dev/null | head -3 | tr '\n' ' ')"
echo "  Data dir: $DATA_DIR"
echo ""
echo "Next steps:"
echo "  1. Edit $COHERENCE_HOME/.env to set your DOC_HOST, DOC_PORT, etc."
echo "  2. Set a password:  $BIN_DIR/coherence-doc set-password"
echo "  3. Restart Claude Code to pick up the new hook settings."
echo ""
