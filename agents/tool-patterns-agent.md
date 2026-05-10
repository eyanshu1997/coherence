# /tool-patterns - Cross-Session Tool Pattern Analysis

Analyze MCP tool use and bash command logs across all sessions. Present a summary
of failure patterns and repeated bash commands. User then asks for targeted fixes.

## Trigger

Run `/tool-patterns` when you want to review accumulated tool failures, identify
documentation gaps that cause repeated mistakes, or see which bash commands might
warrant an MCP tool.

---

## Step 1 — Refresh the Pattern Report

Run the analyzer to get fresh data:
```bash
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-patterns"
```

This reads `~/.claude/tool-uses.jsonl` (all MCP tool calls) and `~/.claude/bash-commands.jsonl`
(repeated bash commands) and writes `~/.claude/tool-patterns.md`.

Then read the report:
```bash
cat ~/.claude/tool-patterns.md
```

---

## Step 2 — Present Summary to User

Show the user a clean summary — do NOT dump the full report. Present:

### High Failure Rate Tools
> "These MCP tools have a ≥30% failure rate in the last 30 days:"
> - `get_github_pr_details` — 8/12 calls failed (67%) — top error: `404 Not Found`
>   Input pattern: `repo_name` empty in 8/8 failures

### Bash Command Candidates
> "These bash command patterns have been run 5+ times — consider making MCP tools:"
> - `git log --oneline -20` — used 12 times
> - `source scripts/gosetup.sh` — used 9 times

Then ask:
> "Which of these would you like me to investigate and fix? For tool failures I can
> update the agent doc. For bash commands I can create an MCP tool."

---

## Step 3 — Act on User Request

### Fixing a tool failure pattern
1. Read the agent doc that covers the failing tool (`~/coherence/agents/`)
2. Identify the root cause from the error + input pattern (e.g., always missing `repo_name`)
3. Add a prominent warning or fix the example call in the doc
4. Confirm the edit with the user

### Creating an MCP tool for a bash command
1. Add the tool implementation to your domain repo's MCP tools directory
2. Add to `allowedTools` in `~/.claude/settings.json`
3. Document in the relevant agent `.md`
4. Update `.env.example` if new env vars needed

---

## Manual Run

```bash
# Analyze last 30 days (default)
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-patterns"

# Analyze last 7 days only
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-patterns" --since-days 7

# Print to stdout without saving
"${COHERENCE_HOME:-$HOME/coherence}/bin/coherence-patterns" --no-save
```

Run `/tool-patterns` whenever you want a fresh analysis — it always re-runs the analyzer.
