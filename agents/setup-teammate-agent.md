# Coherence Teammate Setup

Sets up `coherence-doc` on a teammate's machine to generate docs against a **shared remote server**.
No local server, no nginx, no systemd — just the CLI binary, env config, and Claude Code hooks.

## Prerequisites
- coherence repo already cloned somewhere on disk
- Go 1.22+ installed (`go version`)
- The server owner has shared:
  - `COHERENCE_API_KEY` — the shared secret
  - The server URL (e.g. `https://docs.example.com`)

---

## Steps

### Step 0 — Identify repo location and set COHERENCE_HOME

```bash
export COHERENCE_HOME="$(find ~ -name 'coherence-doc' -path '*/bin/*' 2>/dev/null | head -1 | xargs -I{} dirname {} | xargs -I{} dirname {})"
echo "COHERENCE_HOME=$COHERENCE_HOME"
```

Or set it explicitly:
```bash
export COHERENCE_HOME=/path/to/coherence
```

### Step 1 — Build Go binaries

```bash
cd "$COHERENCE_HOME"
make build
```

This produces `$COHERENCE_HOME/bin/`: `coherence-doc`, `coherence-bash-log`, `coherence-mcp-log`, `coherence-patterns`.
(`coherence-server` is also built by `make build` but not used in teammate mode.)

### Step 2 — Create $COHERENCE_HOME/.env (remote mode)

```bash
cp "$COHERENCE_HOME/.env.example" "$COHERENCE_HOME/.env"
chmod 600 "$COHERENCE_HOME/.env"
```

Then edit `$COHERENCE_HOME/.env` and set these fields (remove or comment out the server-only fields):

```bash
# Remote server URL — where the owner's coherence-server is running
COHERENCE_REMOTE_URL=https://docs.example.com   # replace with actual URL

# Shared API key — get this from the server owner
COHERENCE_API_KEY=<shared-secret>               # replace with actual key

# Public-facing URL config — must match the remote server so printed URLs are correct
DOC_HOST=docs.example.com    # replace with actual hostname
DOC_PORT=443
DOC_SCHEME=https
```

Fields **not** needed in teammate mode (leave commented out or remove):
- `COHERENCE_PORT` / `COHERENCE_BIND` — no local server
- `JIRA_BASE_URL`, `GITHUB_ORG`, `FOOTER_TEXT` — optional; can be set for doc formatting

### Step 3 — Persist COHERENCE_HOME in the shell profile

```bash
SHELL_RC=""
if [ -f ~/.zshrc ]; then
  SHELL_RC=~/.zshrc
elif [ -f ~/.bashrc ]; then
  SHELL_RC=~/.bashrc
fi

if [ -n "$SHELL_RC" ]; then
  if grep -q "COHERENCE_HOME" "$SHELL_RC"; then
    echo "COHERENCE_HOME already in $SHELL_RC — skipping"
  else
    echo "" >> "$SHELL_RC"
    echo "export COHERENCE_HOME=\"$COHERENCE_HOME\"" >> "$SHELL_RC"
    echo "Added COHERENCE_HOME to $SHELL_RC"
  fi
else
  echo "WARNING: could not find ~/.zshrc or ~/.bashrc — add manually:"
  echo "  export COHERENCE_HOME=\"$COHERENCE_HOME\""
fi
```

### Step 4 — Configure Claude Code settings (hooks + COHERENCE_HOME env)

```bash
SETTINGS=~/.claude/settings.json
[ -f "$SETTINGS" ] || echo '{}' > "$SETTINGS"

jq --arg home "$COHERENCE_HOME" '
  .env //= {}
  | .env.COHERENCE_HOME = $home
  | .hooks //= {}
  | .hooks.PostToolUse = (
      [(.hooks.PostToolUse // [])[] | select(.matcher != "mcp__" and .matcher != "Bash")]
      + [
          {"matcher":"mcp__","hooks":[{"type":"command","command":"\($home)/bin/coherence-mcp-log"}]},
          {"matcher":"Bash","hooks":[{"type":"command","command":"\($home)/bin/coherence-bash-log"}]}
        ]
    )
  | .hooks.PreToolUse = (
      [(.hooks.PreToolUse // [])[] | select(.matcher != "Edit|Write")]
      + [{"matcher":"Edit|Write","hooks":[{"type":"command","command":"node \"\($home)/scripts/hooks/suggest-compact.js\""}]}]
    )
  | .statusLine = {
      "type": "command",
      "command": "input=$(cat); cwd=$(echo \"$input\" | jq -r \".cwd\"); branch=$(git -C \"$cwd\" --no-optional-locks rev-parse --abbrev-ref HEAD 2>/dev/null); [ -n \"$branch\" ] && printf \" %s\" \"$branch\""
    }
' "$SETTINGS" > /tmp/settings-merged.json && mv /tmp/settings-merged.json "$SETTINGS"
echo "settings.json updated"
```

Install CLAUDE.md:
```bash
if [ ! -f ~/.claude/CLAUDE.md ]; then
  sed "s|COHERENCE_HOME|$COHERENCE_HOME|g" "$COHERENCE_HOME/dotclaude/CLAUDE.md" \
    > ~/.claude/CLAUDE.md
else
  echo "@$COHERENCE_HOME/agents/generate-doc-agent.md" >> ~/.claude/CLAUDE.md
fi
```

### Step 5 — Install slash commands

```bash
mkdir -p ~/.claude/commands
cp "$COHERENCE_HOME/agents/commands/"*.md ~/.claude/commands/
```

### Step 6 — Test

```bash
# Should print a URL on the remote server
"$COHERENCE_HOME/bin/coherence-doc" generate \
  --folder "test" \
  --title "Teammate Test" \
  --content "hello from teammate setup"
```

A successful run prints something like:
```
https://docs.example.com/test/teammate-test.html
```

If you get `HTTP 401: not authenticated`, the `COHERENCE_API_KEY` in your `.env` doesn't match the server's key — ask the owner to confirm.

---

## Checklist

- [ ] `COHERENCE_HOME` exported and in shell profile
- [ ] `$COHERENCE_HOME/.env` has `COHERENCE_REMOTE_URL`, `COHERENCE_API_KEY`, `DOC_HOST`, `DOC_PORT`, `DOC_SCHEME`
- [ ] `coherence-doc generate` produces a URL on the remote server
- [ ] Claude Code hooks installed (`settings.json` updated)
- [ ] Slash commands installed (`~/.claude/commands/*.md`)
