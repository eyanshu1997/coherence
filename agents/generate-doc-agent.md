# /generate-doc — Generate a Styled HTML Summary Document

Generate a styled HTML document in `~/.coherence/data/<folder>/` and serve it at
`<DOC_BASE_URL>/<folder>/<slug>.html` (where `DOC_BASE_URL` is set in `~/coherence/.env`).

The home page lists all folders with clickable links.
Each folder page lists the documents inside it.

---

## Plan Doc Gate — Required Before Any Implementation

**Any time you are about to make code changes, apply a fix, or start a non-trivial investigation**, you must first:

1. **Generate a plan/analysis doc** in the issue folder using the `coherence-doc` CLI
2. **Pause and direct the user to the browser** — do not proceed until they confirm

This applies to:
- Any investigation where you've identified a root cause and proposed fix
- Any fix or implementation task — before touching any code
- Any multi-step task where approach decisions need review

---

## Doc Folder Routing

Every plan, analysis, fix-summary, and log file belongs in a specific `~/.coherence/data/<folder>/`. Use this table to pick the right folder:

| Session type | Folder |
|---|---|
| Jira bug fix or feature (repo A) | `repo-a/PROJ-XXXXX` |
| Jira bug fix or feature (repo B) | `repo-b/PROJ-XXXXX` |
| AI tooling / infrastructure improvement | `ai-system` — Claude config, hooks, scripts, MCP tools, doc infra, agent files, skill updates, settings.json |
| Investigation / topic work (no Jira) | Descriptive topic slug at root (e.g. `perf-analysis`, `auth-refactor`) |

**Rules:**
- **Never use a synthetic placeholder as a `--folder` argument** — always use a real ticket ID or a descriptive slug.
- **No synthetic catch-alls** — if there is no Jira, use `ai-system` for tooling work or a descriptive topic slug for investigations.
- **Jira pages should show the Jira description** — always include the ticket title and URL in any doc generated for a Jira folder.
- **Always use the comment server move/rename API** (`POST /move-folder`, `POST /rename-folder`) to move or rename doc folders — never use `cp -r` or `mv` directly. The server automatically rewrites internal cross-doc links and `window.DOC_FOLDER` inside every HTML file after the move, then reindexes.
- **Never use `cp -r` to duplicate a folder** — copied files inherit `600` permissions and will 404 in the browser (nginx cannot read them), and internal links will be stale. Use the API instead.

Examples:
```bash
# Jira fix in repo-a
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-doc" generate --folder "repo-a/PROJ-123" --filename "plan.html" ...

# AI tooling work (no Jira)
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-doc" generate --folder "ai-system" --filename "plan-docs-consolidation.html" ...

# Investigation / topic work with a descriptive slug
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-doc" generate --folder "perf-analysis" --filename "analysis.html" ...
```

---

## Plan Doc Content

A plan doc must contain at minimum:

```markdown
## Jira
PROJ-XXXXX — <ticket summary>
<jira_url>

## Root Cause / Problem
<what is wrong and why>

## Proposed Fix / Approach
<numbered steps of what will be changed and why>

## Files to Modify
- `path/to/file.go` — what change and why

## Risks / Open Questions
<anything uncertain, side effects, edge cases to watch>
```

---

## Plan Status Block (required in every plan.html)

Every plan doc must include a `## Plan Status` section at the **top** of the content, maintained by Claude:

```markdown
## Plan Status
- **Generated**: YYYY-MM-DD
- **Executed**: No  ← update to "Yes — YYYY-MM-DD" after first commit or significant action
- **Actions taken**:
  - [ ] <step 1>   ← check off each item as it is completed
  - [ ] <step 2>
- **Pending**:
  - [ ] <open item>
```

**After each significant action** (commit, PR creation, build trigger, build pass, merge), regenerate `plan.html` with the same filename (overwrites) with the status block updated. This keeps the plan doc as the live source of truth for what's done and what's pending.

---

## Gate Message (required — exact wording)

After generating a plan doc, output this and **stop**:

> "Plan doc ready: <DOC_BASE_URL>/PROJ-XXXXX/plan.html
> Review it in the browser, add comments on anything you want changed, then reply here to proceed."

Do **not** continue to implementation until the user replies. The user may:
- Reply "looks good" / "proceed" → continue
- Add browser comments and reply → run `/load-doc PROJ-XXXXX`, incorporate feedback, regenerate plan, gate again
- Reply with inline feedback → incorporate and regenerate before continuing

Always use `--filename "plan.html"` (not a dated name) — overwritten on each revision so the URL stays stable.

**Rule:** any time you report a doc URL to the user (gate message, fix summary, investigation link), generate a shareable link and provide both:
- The shareable link (for sharing externally)
- The direct link (for the user's own browser, already logged in)

---

## Re-generating the Plan

If the user gives feedback (inline or via browser comments):
1. Load browser comments: read `~/.coherence/data/PROJ-XXXXX/plan.comments.json` if it exists
2. Update the plan content to address all feedback
3. Regenerate `plan.html` with the same filename (overwrites previous)
4. Output the gate message again and stop

---

## After Approval — Fix Summary

After implementation is done, generate a fix summary in the same folder:

```bash
cat > /tmp/fix-summary.md << 'DOCEOF'
<content here>
DOCEOF

"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-doc" generate \
  --folder "PROJ-XXXXX" \
  --title "Fix Summary: <ticket summary>" \
  --filename "fix-summary.html" \
  --content-file /tmp/fix-summary.md
```

Fix summary must include: Root Cause, What Was Changed (files + diffs), PR link, Build status.
Report the URL but **do not gate on it** — it's informational, not approval-required.

---

## Investigation Protocol

When investigating issues (no code change needed yet), **stop after identifying the root cause**:

1. Generate an analysis doc (`--filename "analysis.html"`) in the relevant folder (use Jira ID if known, else a topic slug)
2. Output the gate message and wait for confirmation before proposing or applying any fix
3. If the user confirms the fix → generate `plan.html` and gate again before touching code

> "Analysis doc ready: <DOC_BASE_URL>/<folder>/analysis.html
> Review it, add comments on anything that looks wrong, then reply here to proceed."

---

## /generate-doc Command — Steps

### Step 1 — Determine folder name

If the user provides a folder name as the argument (e.g. `/generate-doc PROJ-123`), use it directly.

Otherwise, prompt:
> "What folder should this document go in? (e.g. a Jira ID like PROJ-123, a topic name, or a session label)"

### Step 2 — Ask for document title

Prompt:
> "What should the document be titled?"

### Step 3 — Gather content

Ask the user what the document should contain, OR if this is being called mid-session to summarize work done, use context from the current conversation to write the content.

### Step 4 — Generate the document

For multi-line content, write to a temp file first:

```bash
cat > /tmp/doc-content.md << 'DOCEOF'
<content here>
DOCEOF

"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-doc" generate \
  --folder "PROJ-123" \
  --title "Fix Summary: <title>" \
  --filename "fix-summary.html" \
  --content-file /tmp/doc-content.md
```

The CLI:
1. Creates `~/.coherence/data/<folder>/<slug>.html` with the document
2. Regenerates `~/.coherence/data/<folder>/index.html` (folder listing)
3. Regenerates `~/.coherence/data/index.html` (home page)
4. Prints the URL

### Step 5 — Report the URL

After the CLI runs, report:
> "Document created at: <DOC_BASE_URL>/<folder>/<filename>.html"

---

## Reindex Only

To rebuild all index pages without creating a new document (e.g. after manual edits):

```bash
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-doc" reindex
```

---

## File Layout

```
~/.coherence/data/
  index.html                    ← home page (auto-generated)
  repo-a/
    PROJ-123/
      index.html                ← folder listing (auto-generated)
      plan.html
      fix-summary.html
  ai-system/
    index.html
    plan-some-improvement.html
  perf-analysis/
    index.html
    analysis.html
```

---

## Content Tips

Claude should write rich, structured content. Good examples:

**For a fix summary:**
```markdown
## Root Cause
The issue was caused by X in `file.go:123`.

## Fix Applied
Updated `validatePort()` to accept `ANY` as a valid port value when both source and destination are set.

## Files Changed
- `gosrc/src/controller/api/gateway.go`
- `gosrc/src/controller/apiserver/validate.go`

## How to Test
1. Create a NAT rule with source port ANY and destination port ANY
2. Verify the rule saves without error
```

**For a traffic test result:**
```markdown
## Test Summary
| Test | Result | Notes |
|------|--------|-------|
| HTTP ingress | PASS | 200 OK |
| HTTPS ingress | PASS | TLS handshake OK |
| East-West | FAIL | Packet drop at GW |

## Gateway Status
Gateway `gw-01` reached ACTIVE at 14:32 UTC.
```

---

## Diagrams (Mermaid)

The doc renderer supports Mermaid natively. Use fenced code blocks with the `mermaid` language tag.

Supported types: `sequenceDiagram`, `graph TD`, `graph LR`, `stateDiagram-v2`.

**Rule**: any doc that explains a request path, routing decision, or multi-service flow **must** include a Mermaid diagram. Do not describe flows in prose alone.

### Validate before generating

Always validate a diagram with `mmdc` before generating the doc:

```bash
cat > /tmp/test.mmd << 'EOF'
<paste diagram here>
EOF
npx @mermaid-js/mermaid-cli mmdc -i /tmp/test.mmd -o /tmp/test.svg 2>&1 | grep -i "error\|parse"
```

A clean run prints only `Generating single mermaid chart.`. Fix any parse error before generating.

### sequenceDiagram — Mermaid v11 syntax rules

1. **No non-ASCII characters in labels** — `→`, `—`, `✅`, `❌` all cause lex errors. Use plain ASCII only in message labels and `Note over` text.
2. **No commas in `Note over` text** — `Note over Foo: If not accepted, VM fails` — the comma is parsed as a participant separator. Use a semicolon or rephrase.
3. **No reserved keywords as participant names** — `create`, `destroy`, `box`, `loop`, `alt`, `opt`, `par`, `critical`, `break`, `rect`, `autonumber` are reserved. Even capitalized (e.g. `Create`) causes a parse error. Use a non-reserved alias (e.g. `GWCreate`, `InstanceCreate`).
4. **`Note over` only accepts exactly two participants** — `Note over A,B,C: text` is invalid; use `Note over A,C` to span a range or split into separate notes.
5. **`graph LR/TD` with `subgraph` and no edges** — a subgraph with only disconnected nodes may look unexpected. Use chained edges (`A --> B --> C`) instead.

---

## Shareable Links — Always Required for External URLs

**Never use a bare `<DOC_BASE_URL>/...` URL in any context where someone else might click it** — PR descriptions, Jira comments, team messages, or links from one doc to another. These require browser authentication and will 404 for anyone not logged in.

**This applies to cross-doc links too.** Any `[text](url)` link inside a doc that points to another doc on this server must use a share token.

Generate share tokens with the `coherence-doc share` command:

```bash
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-doc" share --days 365 /repo-a/PROJ-XXXXX/plan.html
```

Run one call per destination path. Use `--days 365` for long-lived documents, `--days 30` for short-term shares.

The share token is appended as a query parameter: `?share=<token>`. The server validates the token and serves the page without requiring login.

- coherence-server and nginx are managed separately — do not check or restart them as part of doc generation.
- If a URL is not reachable, that is an infra issue for the user to address independently — just report the expected URL and move on.
