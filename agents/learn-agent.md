# /learn - Extract Skills and Improve Existing Docs from Current Session

Analyze the **current conversation** for reusable patterns worth saving as skills,
AND for improvements to existing agent docs, commands, and skills that the session revealed.
Asks the user to confirm before writing anything.

## Trigger

Run `/learn` after solving a non-trivial problem in the current session — a tricky bug,
a useful debugging technique, a non-obvious workaround, a project convention discovered.
Do NOT run this for trivial fixes (typos, simple syntax errors, one-time API outages).

---

## Step 1 — Review the Session for New Skills

Look for:

1. **Error Resolution Patterns** — what error occurred, root cause, what fixed it
2. **Debugging Techniques** — non-obvious steps, useful tool combinations, diagnostic sequences
3. **Workarounds** — library quirks, API limitations, version-specific fixes
4. **Project-Specific Patterns** — codebase conventions discovered, architecture decisions, integration patterns

---

## Step 2 — Review Existing Docs, Commands, and Skills for Improvements

Read the following files and compare against what happened in the session:

- Any agent `.md` files that were consulted or produced errors during the session
- Any `~/.claude/commands/` files used during the session
- Any `~/.claude/skills/learned/` files that overlap with Step 1 findings

For each file, look for:

1. **Stale/wrong instructions** — steps that failed, tools used incorrectly, rules that needed to be overridden
2. **Missing steps** — things that worked but aren't documented (flags, workarounds, sequences)
3. **Gaps** — scenarios that occurred with no guidance in the doc, requiring ad-hoc decisions
4. **Redundant/obsolete content** — instructions for workflows that no longer apply
5. **Better approaches** — if the session found a better way than what's in an existing skill

---

## Step 3 — Ask the User to Confirm

Before writing anything, summarize ALL findings from Steps 1 and 2 grouped by type:

> "From this session I found:
>
> **New skills to create:**
> 1. [description] → `~/.claude/skills/learned/foo.md`
>
> **Improvements to existing docs:**
> 1. `agents/fix.md` — [what's wrong/missing and proposed fix]
> 2. `docs/instructions.md` — [what to change]
>
> **Improvements to existing commands/skills:**
> 1. `agents/commands/bar.md` + `~/.claude/commands/bar.md` — [what to change]
>
> Which would you like me to apply? Or all of them?"

Wait for the user to confirm or narrow down before proceeding.

---

## Step 4 — Apply Confirmed Changes

### New skills
Create a skill file at `~/.claude/skills/learned/[pattern-name].md`:

```markdown
# [Descriptive Pattern Name]

**Extracted:** [Date]
**Context:** [Brief description of when this applies]

## Problem
[What problem this solves — be specific]

## Solution
[The pattern/technique/workaround]

## Example
[Code example if applicable]

## When to Use
[Trigger conditions — what should activate this skill]
```

### Doc improvements
Edit the relevant agent `.md` or `docs/instructions.md` file in place using the Edit tool.
- Keep changes minimal and targeted — fix the specific gap, don't rewrite surrounding content

### Command/skill improvements
Edit `agents/commands/<name>.md` AND sync to `~/.claude/commands/<name>.md`:
```bash
cp ~/coherence/agents/commands/<name>.md ~/.claude/commands/<name>.md
```
For agent files (the full logic), edit in place.

---

## Notes

- One pattern per skill file — keep them focused
- Don't extract trivial fixes (typos, simple syntax errors)
- Don't extract one-time issues (specific API outages, temporary breakage)
- Prefer improving existing docs over creating new skills when the gap is in a doc that's always loaded
- For cross-session tool failure analysis and bash command suggestions, use `/tool-patterns`
