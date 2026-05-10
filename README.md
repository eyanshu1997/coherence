# coherence

Generic Claude Code infrastructure: doc generation, comment server, session analytics, and context hooks.

Clone this to get:
- **HTML doc generation** — generate styled HTML docs from markdown, served with auth and a comment/annotation system
- **Session analytics** — PostToolUse hooks log MCP tool calls and bash patterns for pattern analysis

All features are **optional** — missing credentials disable sections gracefully. Doc generation works with zero credentials.

---

## Quick Start

### 1. Clone and build

```bash
git clone <repo-url> ~/coherence
cd ~/coherence
make build
cp .env.example .env
# edit .env and fill in credentials
```

### 2. Test doc generation (no credentials required)

```bash
~/coherence/bin/coherence-doc generate \
  --folder "test" --title "My First Doc" --content "# Hello\nThis works."
```

Docs are written to `~/.coherence/data/<folder>/`.

### 3. Full setup (systemd, nginx, hooks)

Run the setup agent: `/setup-coherence` in Claude Code, or follow `agents/setup-agent.md` manually.

Alternatively, run the automated setup script:
```bash
./setup.sh
```

---

## What's Inside

```
coherence/
  bin/                           ← compiled Go binaries (built by make build)
    coherence-server             ← auth/comment/doc server
    coherence-doc                ← doc generator CLI (markdown → HTML)
    coherence-bash-log           ← PostToolUse(Bash) hook: log bash patterns
    coherence-mcp-log            ← PostToolUse(mcp__*) hook: log MCP tool calls
    coherence-patterns           ← analyze tool use + bash patterns
  scripts/
    coherence-server.service     ← systemd unit template
    hooks/
      suggest-compact.js         ← PreToolUse(Edit|Write) hook: compact context hint
  www/assets/                    ← frontend CSS + JS for docs
  agents/                        ← agent docs for slash commands
    setup-agent.md               ← full setup procedure
    generate-doc-agent.md        ← doc generation rules + plan gate + mermaid guide
    load-doc-agent.md            ← /load-doc workflow
    learn-agent.md               ← /learn (extract skills from session)
    tool-patterns-agent.md       ← /tool-patterns
    commands/                    ← slash command wrappers (install to ~/.claude/commands/)
  dotclaude/
    CLAUDE.md                    ← template for ~/.claude/CLAUDE.md (coherence-only setup)
    settings.json                ← template for ~/.claude/settings.json (coherence hooks only)
  cmd/                           ← Go source for each binary
  internal/                      ← shared Go packages (auth, config, docgen, server, analytics)
  Makefile
  go.mod
  setup.sh                       ← automated setup script
```

---

## Feature Flags

Each feature requires credentials in `~/coherence/.env`:

| Feature | Required keys | Without |
|---------|--------------|---------|
| Doc generation | _(none)_ | Always works |
| Doc auth + comments | run `coherence-doc set-password` | Auth disabled |
| Jira auto-linking in docs | `JIRA_BASE_URL` | Auto-linking skipped |
| GitHub PR auto-linking | `GITHUB_ORG` | Auto-linking skipped |

---

## Slash Commands

Install: `cp ~/coherence/agents/commands/*.md ~/.claude/commands/`

| Command | What it does |
|---------|-------------|
| `/generate-doc` | Generate a styled HTML doc |
| `/load-doc` | Load a doc + browser comments into context |
| `/learn` | Extract reusable skills from the session |
| `/tool-patterns` | Analyze MCP tool failure + bash patterns |
| `/setup-coherence` | Full VM setup walkthrough |

---

## dotclaude Templates

`dotclaude/` contains Claude Code configuration templates for standalone coherence use:

| File | Purpose |
|------|---------|
| `dotclaude/CLAUDE.md` | Minimal CLAUDE.md pointing at coherence's generate-doc-agent.md |
| `dotclaude/settings.json` | Hooks: coherence-mcp-log, coherence-bash-log, suggest-compact; statusLine |

If adding coherence hooks into an existing `~/.claude/settings.json` (e.g. from a domain repo),
merge the hooks block from `dotclaude/settings.json` rather than overwriting.
