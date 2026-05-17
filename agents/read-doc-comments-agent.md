# /read-doc-comments — Load User Comments from Doc Pages into Context

Reads all comment sidecars (`*.comments.json`) for a given folder in `~/.coherence/data/`
and surfaces them as feedback for the current session.

## Usage
```
/read-doc-comments PROJ-123
/read-doc-comments _system
```

If no argument is given, ask: "Which folder's comments should I load? (e.g. PROJ-123)"

---

## Steps

### Step 1 — Find comment files

```bash
ls ~/.coherence/data/<folder>/*.comments.json 2>/dev/null
```

If no files found:
> "No comments found for `<folder>`. Comments are added via the browser at <DOC_BASE_URL>/<folder>/"

### Step 2 — Read all comment files

For each `.comments.json` file found:
```bash
cat ~/.coherence/data/<folder>/<slug>.comments.json
```

Each file is a JSON array of `{"ts": "...", "text": "..."}` entries.
The filename without `.comments.json` is the associated document slug.

### Step 3 — Present as structured context

Format the comments clearly. If a comment has a `quote` field (added via text-selection in the browser), show it as the anchor so Claude knows exactly which part of the doc was annotated:

```
## Feedback from prior sessions — PROJ-123

### From: fix-summary.html
  [2026-04-21 18:30] On: "The fix applied to AWS only"
                     → "Need to also handle the Azure case"
  [2026-04-22 09:00] "Reviewer wants the error path extracted to a helper function"

### From: plan-2026-04-21.html
  [2026-04-21 16:00] On: "regenerate proto files after branch switch"
                     → "Check if this approach breaks the existing E2E test for VNET peering"
```

General comments (no quote) are shown as plain instructions. Quoted comments are shown with their anchor text so Claude understands what section prompted the feedback.

### Step 4 — Mark comments as acknowledged

After presenting the comments, mark each one as handled via the acknowledge endpoint so the browser shows a "✓ Handled" badge. Call once per comment, using the exact `ts` value from the JSON:

```bash
COHERENCE_HOME="${COHERENCE_HOME:-$(dirname "$(dirname "$(which coherence-doc)")")}"; source "${COHERENCE_HOME}/.env"
COHERENCE_PORT="${COHERENCE_PORT:-8080}"
curl -s -X POST "http://localhost:${COHERENCE_PORT}/acknowledge" \
  -H "Content-Type: application/json" \
  -d '{"folder": "<folder>", "file": "<slug>", "ts": "<ts>"}'
```

Run all acknowledge calls in parallel (one per comment). This is a fire-and-forget — no need to check the response.

### Step 5 — Incorporate

State clearly which comments you are treating as active instructions for the current session.
If a comment is ambiguous, ask the user to clarify before acting on it.

---

## Notes

- Comments are stored in `~/.coherence/data/<folder>/<slug>.comments.json` — plain JSON, easy to edit manually
- To delete a comment: edit the file and remove the entry
- To clear all comments for a doc: `rm ~/.coherence/data/<folder>/<slug>.comments.json`
- Comments are submitted to the server on `COHERENCE_PORT` (from `.env`; default 8080 standalone, 8081 behind nginx)
- The comment server is managed by systemd: `sudo systemctl status coherence-server`
