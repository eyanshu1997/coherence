# coherence

Claude Code infrastructure for teams and developers who want structured, reviewable AI collaboration — built around the insight that **HTML is a dramatically better medium for communicating with Claude than plain text or Markdown**.

---

## The Problem with Text-First AI Workflows

Most Claude Code workflows look like this:

1. You describe a task in chat
2. Claude makes changes
3. You react, iterate in chat
4. Somewhere along the way the plan lived only in your head (or got lost in a long thread)

This works for small tasks. It breaks down for anything that requires:
- Reviewing Claude's reasoning before it acts
- Giving structured feedback across multiple sessions
- Keeping a record of what was decided and why
- Multiple people collaborating on the same task

---

## The HTML Insight

In May 2026, Thariq Shihipar (engineering lead for Claude Code at Anthropic) published ["The Unreasonable Effectiveness of HTML"](https://simonwillison.net/2026/May/8/unreasonable-effectiveness-of-html/) — arguing that HTML is a far superior medium for AI output than Markdown.

The key arguments:

- **Markdown was optimized for token scarcity** — it made sense with GPT-4's 8,192-token limit. With million-token context windows, that constraint is gone.
- **HTML carries structure that Markdown loses** — headings, tables, callouts, severity colors, inline annotations, diagrams (SVG/Mermaid), and interactive elements all render faithfully in HTML.
- **HTML is a better input medium too** — a richly structured HTML document that Claude reads as context is more navigable and semantically clear than a flat markdown wall of text.
- **Complex reasoning externalizes better in HTML** — when Claude writes a plan doc as a real HTML page, it's forced to structure the reasoning: problem → root cause → proposed fix → risks. That structure reduces mistakes and improves review quality.

**coherence was built around this insight, end to end.** Every plan, analysis, and fix summary is an HTML document — not a markdown file, not a chat message.

---

## What coherence gives you

### 1. Structured plan docs before any action

The core workflow: before Claude writes a single line of code, it generates an HTML plan doc and pauses for your review.

```
You: fix the login timeout bug
Claude: [generates plan.html] → "Plan doc ready: http://localhost:8080/auth-bug/plan.html — review it and reply to proceed"
You: [reads plan in browser, adds annotation: "don't touch the session store, we're migrating it next sprint"]
You: /load-doc auth-bug  →  Claude reads the doc + your comment
Claude: [updates plan, re-gates]  →  "Updated plan ready..."
You: looks good, proceed
Claude: [now writes code]
```

This is the plan doc gate — a hard stop between reasoning and action that uses the browser as the review interface.

**Why this matters**: the plan doc externalizes Claude's intent into a reviewable artifact. You're not approving a wall of chat text — you're reviewing a structured document that lays out root cause, proposed changes, files to modify, and risks in labeled sections.

### 2. Browser comment feedback loop

Every doc has an annotation layer. While Claude is working or waiting:

- Click any paragraph in the browser to add a comment
- Comments are persisted to `~/.coherence/data/<folder>/<doc>.comments.json`
- Run `/load-doc <folder>` to pull the doc + all comments into Claude's context

This closes the loop between asynchronous human review and Claude's in-session context — without copy-pasting anything.

**Example use case**: share a plan doc with a colleague (via a share token link), they annotate "this approach won't work because X", you load their comment back into the session. Claude gets structured feedback as context, not a forwarded chat.

### 3. Consistent, styled HTML document generation

```bash
coherence-doc generate \
  --folder "auth-bug" \
  --title "Plan: Fix login timeout" \
  --filename "plan.html" \
  --content-file /tmp/plan.md
```

Every generated doc gets:
- Consistent styling (dark theme, typography, callout blocks)
- Auto-generated folder index pages and a home page
- Mermaid diagram rendering (sequence diagrams, flowcharts, state machines)
- Jira and GitHub PR auto-linking when credentials are configured
- Share token support for external links

Docs live in `~/.coherence/data/` and are served by `coherence-server` with session auth.

### 4. Session analytics

PostToolUse hooks log every MCP tool call and bash command pattern:

```bash
/tool-patterns   # surfaces: which MCP tools fail repeatedly, which bash commands you run most
```

Useful for identifying where to add `allowedTools` entries, which operations to script, and where Claude spends most of its time.

---

## How HTML context compares to Markdown context

When Claude reads a plan doc loaded by `/load-doc`, it gets:

**With Markdown:**
```
## Root Cause
The session store checks expiry on read but not on write...

## Fix
Change validateSession() in auth/session.go...
```

**With HTML (what coherence generates):**
- Labeled `<section>` blocks with semantic `id` attributes Claude can reference
- `<blockquote class="comment">` blocks from browser annotations, attributed to reviewer
- Mermaid diagrams rendered as SVG embedded in the document
- Color-coded risk callouts (`<div class="risk-high">`)
- Cross-links to related docs

The HTML document is not just prettier — it's more navigable, more precise, and it carries the comment annotations in-band as first-class context.

---

## The feedback loop in full

```
                   ┌──────────────────────────────────────────────────┐
                   │               coherence workflow                  │
                   │                                                    │
  You describe ───►│ Claude generates plan.html                        │
  a task           │         │                                          │
                   │         ▼                                          │
                   │  coherence-server serves it at localhost:8080     │
                   │         │                                          │
                   │         ▼                                          │
                   │  You review in browser, add comments              │◄── colleague can
                   │         │                                          │    review via
                   │         ▼                                          │    share token
                   │  /load-doc <folder> pulls doc + comments          │
                   │  back into Claude's context                        │
                   │         │                                          │
                   │         ▼                                          │
                   │  Claude revises plan, re-gates if needed          │
                   │         │                                          │
                   │         ▼                                          │
                   │  You approve → Claude acts                         │
                   │         │                                          │
                   │         ▼                                          │
                   │  Claude generates fix-summary.html                │
                   └──────────────────────────────────────────────────┘
```

---

## Quick Start

### 1. Clone and build

```bash
git clone <repo-url> ~/coherence
cd ~/coherence
make build
cp .env.example .env
# edit .env — DOC_BASE_URL is required; everything else is optional
```

### 2. Test doc generation (no credentials needed)

```bash
~/coherence/bin/coherence-doc generate \
  --folder "test" --title "Hello" --content "# Hello\nThis works."
```

Docs are written to `~/.coherence/data/test/`.

### 3. Full setup (systemd + nginx + hooks)

```bash
/setup-coherence    # in Claude Code — interactive guided setup
```

Or run the automated script: `./setup.sh`

---

## What's Inside

```
coherence/
  bin/                           ← compiled Go binaries (make build)
    coherence-server             ← auth + comment server + doc serving
    coherence-doc                ← doc generator CLI (markdown → HTML)
    coherence-bash-log           ← PostToolUse(Bash) hook
    coherence-mcp-log            ← PostToolUse(mcp__*) hook
    coherence-patterns           ← tool use + bash pattern analyzer
  agents/                        ← agent instruction files
    generate-doc-agent.md        ← plan gate, doc generation rules, mermaid guide
    load-doc-agent.md            ← /load-doc workflow
    learn-agent.md               ← /learn (extract reusable skills from session)
    tool-patterns-agent.md       ← /tool-patterns
    setup-agent.md               ← full VM setup procedure
    commands/                    ← install to ~/.claude/commands/ for slash commands
  dotclaude/
    CLAUDE.md                    ← minimal template: just loads generate-doc-agent.md
    settings.json                ← hooks template: mcp-log, bash-log, suggest-compact
  scripts/
    coherence-server.service     ← systemd unit template
    hooks/suggest-compact.js     ← PreToolUse hook: compact context hint
  www/assets/                    ← CSS + JS for the doc UI
  internal/                      ← Go packages: auth, docgen, server, analytics
  cmd/                           ← Go entry points for each binary
```

---

## Feature Flags

| Feature | Required | Without |
|---------|----------|---------|
| Doc generation | _(none)_ | Always works |
| Doc auth + comments | `coherence-doc set-password` | Auth disabled, comments still work locally |
| Jira auto-linking | `JIRA_BASE_URL` in `.env` | Skipped |
| GitHub PR auto-linking | `GITHUB_ORG` in `.env` | Skipped |

---

## Slash Commands

Install: `cp ~/coherence/agents/commands/*.md ~/.claude/commands/`

| Command | What it does |
|---------|-------------|
| `/generate-doc` | Generate a styled HTML doc with plan gate |
| `/load-doc <folder>` | Load a doc + browser comments into context |
| `/learn` | Extract reusable skills from the current session |
| `/tool-patterns` | Analyze MCP tool failure + bash command patterns |
| `/setup-coherence` | Full interactive VM setup walkthrough |

---

## dotclaude Templates

`dotclaude/CLAUDE.md` is intentionally minimal — it only loads `generate-doc-agent.md`. Add your own project-specific preferences on top when setting up a new machine or repo.

If you already have a `~/.claude/settings.json`, merge the hooks block from `dotclaude/settings.json` rather than overwriting it.

---

## Design Principles

- **Gate before acting** — every non-trivial change gets a plan doc. The plan doc gate is the central habit this system enforces.
- **HTML as the collaboration medium** — not markdown, not chat history. Structured, styled, annotatable HTML documents that both humans and Claude can read richly.
- **Feedback stays in-band** — browser comments flow back into Claude's context via `/load-doc`, not via copy-paste or re-explanation.
- **Optional by default** — missing credentials degrade gracefully. Doc generation requires nothing. Auth and integrations layer on top.
