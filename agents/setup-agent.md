# Coherence Setup Agent

Sets up the `coherence` environment on a new machine: Go binaries, `.env`, systemd service, nginx config, and slash commands.

## Prerequisites
- coherence repo already cloned somewhere on disk
- Go 1.22+ installed (`go version`)
- nginx installed and running (optional — only needed for TLS termination)

---

## Steps

### Step 0 — Identify repo location and set COHERENCE_HOME

Find where the coherence repo lives and export `COHERENCE_HOME`:
```bash
# Set this to the actual path of the coherence repo on this machine
export COHERENCE_HOME="$(find ~ -name 'coherence-doc' -path '*/bin/*' 2>/dev/null | head -1 | xargs -I{} dirname {} | xargs -I{} dirname {})"
echo "COHERENCE_HOME=$COHERENCE_HOME"
```

Or set it explicitly if you already know the path:
```bash
export COHERENCE_HOME=/path/to/coherence   # replace with actual path on this machine
```

### Step 1 — Build Go binaries

```bash
cd "$COHERENCE_HOME"
make build
```

This produces `$COHERENCE_HOME/bin/`: `coherence-server`, `coherence-doc`, `coherence-bash-log`, `coherence-mcp-log`, `coherence-patterns`.

### Step 2 — Create $COHERENCE_HOME/.env

Copy `.env.example` and fill in credentials:
```bash
cp "$COHERENCE_HOME/.env.example" "$COHERENCE_HOME/.env"
chmod 600 "$COHERENCE_HOME/.env"
```

Required fields for full functionality:
| Key | Purpose |
|-----|---------|
| `DOC_HOST` | Public hostname (default: `localhost`) |
| `DOC_PORT` | Public port as seen by the browser (default: `8080`) |
| `DOC_SCHEME` | `http` or `https` (default: `http` for localhost, `https` otherwise) |
| `COHERENCE_PORT` | Port coherence-server binds to (default: `8080`; set to `8081` behind nginx) |
| `COHERENCE_BIND` | Bind address (default: `0.0.0.0`; set to `127.0.0.1` behind nginx) |
| `GITHUB_ORG` | e.g. `myorg` — enables pretty PR URL labels in docs (optional) |
| `JIRA_BASE_URL` | e.g. `https://myorg.atlassian.net` — enables ticket ID auto-linking in docs (optional) |
| `FOOTER_TEXT` | Text shown in all page footers (default: `Built with coherence`, optional) |

Auth (password hash + session secret) is stored in `~/.ssh/doc-auth.json`, not in `.env`.
Set a password with:
```bash
"$COHERENCE_HOME/bin/coherence-doc" set-password
```

### Step 3 — Install systemd service

**Standalone (no nginx):** coherence-server runs directly on the public port.
Set `COHERENCE_PORT=8080` (or whatever port you want) and `COHERENCE_BIND=0.0.0.0` in `.env`.

**Behind nginx:** set `COHERENCE_PORT=8081` and `COHERENCE_BIND=127.0.0.1` in `.env` so the server
only listens on localhost, and nginx forwards traffic to it.

Check if the service is already installed and act accordingly:
```bash
SERVICE=/etc/systemd/system/coherence-server.service

if systemctl list-unit-files coherence-server.service &>/dev/null && \
   [ "$(systemctl is-active coherence-server)" = "active" ]; then
  echo "coherence-server already running — reloading with updated service file"
  sed -e "s/YOUR_USERNAME/$(whoami)/g" \
      -e "s|COHERENCE_HOME_PLACEHOLDER|$COHERENCE_HOME|g" \
      "$COHERENCE_HOME/scripts/coherence-server.service" \
    | sudo tee "$SERVICE" > /dev/null
  sudo systemctl daemon-reload
  sudo systemctl restart coherence-server
else
  echo "Installing coherence-server service"
  sed -e "s/YOUR_USERNAME/$(whoami)/g" \
      -e "s|COHERENCE_HOME_PLACEHOLDER|$COHERENCE_HOME|g" \
      "$COHERENCE_HOME/scripts/coherence-server.service" \
    | sudo tee "$SERVICE" > /dev/null
  sudo systemctl daemon-reload
  sudo systemctl enable coherence-server
  sudo systemctl start coherence-server
fi

sudo systemctl status coherence-server
```

### Step 4 — Configure nginx (optional — only if using a TLS reverse proxy)

nginx is optional. coherence-server handles auth, static files, and routing itself.
If you have nginx for TLS termination, make it a simple forwarder — no auth_request,
no internal locations, no aliases. Everything is handled by coherence-server.

```nginx
# /etc/nginx/conf.d/docs.conf — device-specific, not part of the coherence repo
server {
    listen 443 ssl;   # or 8080 ssl — whatever external port you want
    server_name your.hostname.example.com;

    ssl_certificate     /path/to/fullchain.pem;
    ssl_certificate_key /path/to/privkey.pem;

    # Forward everything to coherence-server — no auth, no aliases
    location / {
        proxy_pass         http://127.0.0.1:8081;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_set_header   Cookie            $http_cookie;
        proxy_read_timeout 60s;
    }
}
```

```bash
sudo nginx -t && sudo systemctl reload nginx
```

### Step 5 — Configure ~/.claude/ (hooks, settings, CLAUDE.md)

#### 5a — Persist COHERENCE_HOME in the shell profile

`COHERENCE_HOME` must be exported in the user's shell profile so every terminal session
and Claude Code invocation inherits it automatically:

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

#### 5b — Configure Claude Code settings (hooks + COHERENCE_HOME env)

This step also sets `COHERENCE_HOME` in Claude Code's environment so all hooks resolve the
repo path dynamically — no symlink or hardcoded path needed.

```bash
# Merge coherence hooks + COHERENCE_HOME env into ~/.claude/settings.json using jq
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

Install CLAUDE.md (substitute the placeholder with the actual COHERENCE_HOME path):
```bash
# If ~/.claude/CLAUDE.md does not exist yet:
sed "s|COHERENCE_HOME|$COHERENCE_HOME|g" "$COHERENCE_HOME/dotclaude/CLAUDE.md" \
  > ~/.claude/CLAUDE.md

# If ~/.claude/CLAUDE.md already exists with custom content,
# ensure this line is present (with the actual path substituted):
echo "@$COHERENCE_HOME/agents/generate-doc-agent.md" >> ~/.claude/CLAUDE.md
```

### Step 6 — Install slash commands
```bash
mkdir -p ~/.claude/commands
cp "$COHERENCE_HOME/agents/commands/"*.md ~/.claude/commands/
```

Available commands after install:
- `/generate-doc` — generate HTML doc
- `/load-doc` — load doc + comments into context
- `/learn` — extract reusable skills from session
- `/tool-patterns` — analyze MCP tool failure patterns
- `/setup-coherence` — full VM setup walkthrough

### Step 7 — Test end-to-end
```bash
# Test doc generation
"$COHERENCE_HOME/bin/coherence-doc" generate \
  --folder "test" --title "Test" --content "hello"

# Verify server
curl -s http://localhost:8080/auth/check
```

---

## Directory Structure After Setup

```
$COHERENCE_HOME/          ← wherever the repo lives (set via COHERENCE_HOME)
  .env                    ← credentials (gitignored)
  bin/                    ← compiled Go binaries
    coherence-server
    coherence-doc
    coherence-bash-log
    coherence-mcp-log
    coherence-patterns
  scripts/                ← Node hooks + systemd service template
    hooks/
      suggest-compact.js
    coherence-server.service
  www/assets/             ← doc frontend CSS + JS
  agents/                 ← agent docs + slash command wrappers
    commands/             ← thin wrappers installed to ~/.claude/commands/
  dotclaude/
    CLAUDE.md             ← template for ~/.claude/CLAUDE.md (uses COHERENCE_HOME placeholder)
    settings.json         ← template for ~/.claude/settings.json (coherence hooks only)

~/.claude/
  settings.json           ← active hooks config (COHERENCE_HOME env var set here)
  CLAUDE.md               ← active global instructions (actual path substituted)
  commands/*.md           ← installed slash commands

~/.coherence/data/        ← generated HTML docs (served by coherence-server)

~/.ssh/
  doc-auth.json           ← password hash + session secret (written by coherence-doc set-password)
  doc-shares.json         ← share tokens
```
