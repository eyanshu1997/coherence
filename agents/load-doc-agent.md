# /load-doc — Load a Doc Folder into Context

Loads all HTML docs from a `~/.coherence/data/<folder>/` into context as readable content,
then automatically runs the `/read-doc-comments` flow for that folder.

## Usage
```
/load-doc PROJ-123
/load-doc ai-system
/load-doc perf-analysis
/load-doc https://<your-host>/PROJ-123/plan.html
/load-doc https://<your-host>/ai-system/
```

If no argument is given, list available folders and ask which one to load:
```bash
ls ~/.coherence/data/
```

---

## Steps

### Step 0 — Resolve the argument

The argument may be a folder name, a doc URL, or a URL pointing to a folder root.

**If the argument is a URL** (matching the public base URL constructed from `DOC_HOST`/`DOC_PORT`/`DOC_SCHEME` in `$COHERENCE_HOME/.env`):
- Strip the host prefix to get the path
- If the path ends in `.html` → extract `<folder>` and `<filename>` from it, load only that one file
  - Example: `PROJ-123/plan.html` → folder=`PROJ-123`, file=`plan.html`
- If the path ends in `/` or has no filename → treat the path as a folder name and load all docs
  - Example: `ai-system/` → folder=`ai-system`
- Local file path: `~/.coherence/data/<folder>/<filename>.html`

**If the argument is a plain folder name** → load all docs in that folder.

### Step 1 — List docs in the folder

```bash
ls ~/.coherence/data/<folder>/*.html 2>/dev/null
```

If no HTML files found:
> "No docs found in `<folder>`. Use `/generate-doc <folder>` to create one."

### Step 2 — Extract content from each doc

For each `.html` file (skip `index.html`), extract the raw markdown embedded in the file.
The raw markdown is stored in `<script type="application/x-markdown" id="doc-raw-markdown">` inside each HTML file.

Use grep to extract it:
```bash
grep -oP '(?<=<script type="application/x-markdown" id="doc-raw-markdown">).*?(?=</script>)' \
  ~/.coherence/data/<folder>/<filename>.html
```

Or use sed for multi-line content:
```bash
sed -n '/<script[^>]*id="doc-raw-markdown"/,/<\/script>/p' \
  ~/.coherence/data/<folder>/<filename>.html \
  | sed '1d;$d'
```

If no embedded markdown found (old format), fall back to stripping HTML tags with grep.

Present the content with the filename as a header so it's clear which doc each section came from:

```
## Loaded: plan.html
<content>

## Loaded: fix-summary.html
<content>
```

### Step 2b — Detect embedded images

After extracting raw markdown, check for image references:

```bash
grep -oP '!\[[^\]]*\]\([^)]+\)' ~/.coherence/data/<folder>/<filename>.html 2>/dev/null
```

For each image path found (e.g. `![image](/coherence/images/1747393200000-screenshot.png)`):
- Map the URL path to a local filesystem path: `~/.coherence/data/<relative-path>`
  (strip the leading `/` and prepend the data dir)
- Note in the loaded context: "Doc contains N image(s): [list of paths]"
- If Claude needs to visually understand an image's contents, use the `Read` tool on the local file path — Claude is multimodal and can interpret PNG/JPG screenshots directly

### Step 3 — Load comments (read-doc-comments flow)

After loading the docs, automatically run the full `/read-doc-comments` flow for the same folder:

1. Find comment files:
   ```bash
   ls ~/.coherence/data/<folder>/*.comments.json 2>/dev/null
   ```

2. For each `.comments.json` found, read it and present as structured feedback (same format as `/read-doc-comments`):
   ```
   ## Comments from prior sessions — <folder>

   ### From: plan.html
     [2026-04-21 18:30] On: "the fix applied to AWS only"
                        → "Need to also handle Azure VNET case"
   ```

3. Acknowledge each comment:
   ```bash
   COHERENCE_HOME="${COHERENCE_HOME:-$(dirname "$(dirname "$(which coherence-doc)")")}"
   "${COHERENCE_HOME}/scripts/coherence-api" /acknowledge \
     '{"folder": "<folder>", "file": "<slug>", "ts": "<ts>"}'
   ```
   Run all acknowledge calls in parallel (fire-and-forget).

4. State which comments are being treated as active instructions.

### Step 4 — Summary

After loading, output a one-line summary:
> "Loaded N doc(s) and M comment(s) from `<folder>`. Active instructions: [list or 'none']."

---

## Comment data model — what fields to expect

Each comment in a `.comments.json` file is a JSON object. Fields:

| Field | Meaning |
|-------|---------|
| `ts` | RFC3339 timestamp — primary key |
| `text` | The comment body |
| `author` | Email or "anonymous" |
| `quote` | Selected text excerpt (up to 120 chars) — present on inline comments |
| `range` | DOM range path `{sc, so, ec, eo}` — present on **new-style inline comments** (see below) |
| `acknowledged` | `true` when acknowledged via the API |
| `ack_ts` | Timestamp of acknowledgement |
| `reply` | Claude's reply text (posted via `/reply-comment`) |
| `reply_ts` | Timestamp of Claude's reply |
| `reply_author` | Who posted the Claude reply |
| `handled` | `true` when a Claude reply has been posted |
| `replies` | Array of user thread replies `[{ts, text, author}]` — posted via `/add-reply` |

### Inline comments vs doc-level comments

**Inline comments** have a `range` field. They are anchored to a specific span of text in the document body. The `quote` field contains the selected text for context.

**Doc-level comments** have no `range` field. They appear at the bottom of the page. Old-style inline comments (those with `quote` but no `range`) are also treated as doc-level.

### Thread replies (`replies` array)

A comment may have a `replies` array — peer replies from users in a thread. These are separate from `reply`/`reply_ts` (which is Claude's reply via `/reply-comment`).

When presenting comments to Claude:
- Show `quote` for inline comments as the anchor context ("on: '...'")
- Show `replies` as a sub-thread beneath the main comment
- An inline comment with a `quote` should be addressed in context of that specific section of the doc
- A comment with `reply` already set is handled — still surface it as context but note it's been addressed

### How to reply to an inline comment

Use the `/reply-comment` endpoint. Go through `scripts/coherence-api` rather
than bare `curl`: mutating endpoints require the API key when one is
configured, and a raw curl sends no credentials, so it gets a 401. The wrapper
reads the key from the server's `.env`.

```bash
"${COHERENCE_HOME:-$HOME/coherence}/scripts/coherence-api" /reply-comment \
  '{"folder": "<folder>", "file": "<slug>", "ts": "<ts>", "reply": "<your reply text>"}'
```

This sets `reply`, `reply_ts`, `reply_author`, and `handled: true` on the comment — the user will see your reply as a "Claude:" bubble inline next to the highlighted text.

### Presenting inline comments in context

When a comment has a `quote`, present it with the doc section it belongs to in mind:
```
[INLINE on: "the fix applied to AWS only"]
→ "Need to also handle Azure VNET case"
  Thread: eygupta@cisco.com (2026-05-01): "Azure uses VNET peering, not TGW"
```

When a comment has no `quote`, it's a general note on the whole doc:
```
[DOC-LEVEL]
→ "This whole section is outdated — gateway provisioning changed in v4.2"
```

---

## Notes

- Only reads `.html` files — skips `index.html` (it's a navigation page, not a content doc)
- Raw markdown is stored in `<script type="application/x-markdown" id="doc-raw-markdown">` inside each HTML file
- If a doc has no embedded markdown (old format), falls back to stripping HTML tags
- Comments are stored in `~/.coherence/data/<folder>/<slug>.comments.json`
- The comment server port is read from `COHERENCE_PORT` in `.env` (default 8080 standalone, 8081 behind nginx): `sudo systemctl status coherence-server`

## IMPORTANT — Do NOT call non-existent API endpoints

The coherence-server serves files and handles the comment/share API — there is no `get_doc_content` endpoint. Always read doc content directly from the filesystem as shown in Step 2 above.
