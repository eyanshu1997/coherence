package server

import (
	"coherence/internal/docgen"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Version history endpoints.
//
// All of them are owner-gated: snapshots retain documents that were
// deliberately deleted, so history must not be readable by a GUEST_ACCESS
// reader even though the current page is.
//
// What the UI shows is a diff of the *markdown source*, not of the stored HTML.
// The snapshot tracks the rendered artifact — which is also the source, since
// every generated doc embeds its own markdown — so the server extracts the
// source from each blob before diffing. A diff of rendered markup would be
// unreadable.

// docRelPath maps a folder+file pair to the snapshot-relative path of the doc,
// or "" if the inputs are invalid.
func (h *Handler) docRelPath(folder, file string) string {
	_, dest := safeDocPath(h.cfg.DataDir, folder, file)
	if dest == "" {
		return ""
	}
	dataDirAbs, err := filepath.Abs(h.cfg.DataDir)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(dataDirAbs, dest)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// versionEnabled writes a 503 and reports false when no snapshot store is attached.
func (h *Handler) versionEnabled(w http.ResponseWriter) bool {
	if h.ver.Enabled() {
		return true
	}
	sendJSON(w, 503, map[string]any{"error": "version history is disabled on this server"})
	return false
}

// handleDocHistory lists the snapshots that touched one document, newest first.
func (h *Handler) handleDocHistory(w http.ResponseWriter, r *http.Request) {
	if !h.requireOwner(w, r) || !h.versionEnabled(w) {
		return
	}
	folder := r.URL.Query().Get("folder")
	file := r.URL.Query().Get("file")
	rel := h.docRelPath(folder, file)
	if rel == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder or file"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 50
	}
	revs, err := h.ver.History(rel, limit)
	if err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if revs == nil {
		revs = nil // keep the JSON an empty array rather than null
	}
	out := make([]map[string]any, 0, len(revs))
	for _, rv := range revs {
		out = append(out, map[string]any{
			"rev":     rv.Rev,
			"short":   rv.Short,
			"unix":    rv.Unix,
			"date":    rv.Date,
			"subject": rv.Subject,
			"author":  rv.Author,
		})
	}
	sendJSON(w, 200, map[string]any{"ok": true, "path": rel, "revisions": out})
}

// sourceAt returns the markdown source of a document as of rev. The second
// result is false when the blob is not a generated doc (so it carries no
// embedded source) — the raw bytes are returned in that case.
func (h *Handler) sourceAt(rel, rev string) (string, bool, error) {
	revs, err := h.ver.History(rel, 200)
	if err != nil {
		return "", false, err
	}
	// Resolve the path through the revision list rather than trusting the
	// caller: it is the only way to follow a rename, and it means no
	// client-supplied path ever reaches git.
	for _, rv := range revs {
		if strings.EqualFold(rv.Rev, rev) || rv.Short == strings.ToLower(rev) {
			blob, err := h.ver.FileAt(rv.Rev, rv.Path)
			if err != nil {
				// The revision is listed but its blob will not read — treat it
				// as missing rather than as a server fault.
				return "", false, os.ErrNotExist
			}
			if md, ok := docgen.ExtractRawMarkdown(blob); ok {
				return md, true, nil
			}
			return string(blob), false, nil
		}
	}
	return "", false, os.ErrNotExist
}

// currentSource returns the markdown source of the document as it stands now.
func (h *Handler) currentSource(folder, file string) (string, bool) {
	_, dest := safeDocPath(h.cfg.DataDir, folder, file)
	if dest == "" {
		return "", false
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return "", false
	}
	if md, ok := docgen.ExtractRawMarkdown(data); ok {
		return md, true
	}
	return string(data), false
}

// handleDocVersion returns the markdown source of one past revision.
func (h *Handler) handleDocVersion(w http.ResponseWriter, r *http.Request) {
	if !h.requireOwner(w, r) || !h.versionEnabled(w) {
		return
	}
	folder := r.URL.Query().Get("folder")
	file := r.URL.Query().Get("file")
	rev := r.URL.Query().Get("rev")
	rel := h.docRelPath(folder, file)
	if rel == "" || rev == "" {
		sendJSON(w, 400, map[string]any{"error": "folder, file and rev required"})
		return
	}
	src, isMarkdown, err := h.sourceAt(rel, rev)
	if err == os.ErrNotExist {
		sendJSON(w, 404, map[string]any{"error": "revision not found for this document"})
		return
	}
	if err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "rev": rev, "content": src, "is_markdown": isMarkdown})
}

// handleDocDiff returns a unified diff of one revision against another, or
// against the document as it stands now when "to" is omitted.
func (h *Handler) handleDocDiff(w http.ResponseWriter, r *http.Request) {
	if !h.requireOwner(w, r) || !h.versionEnabled(w) {
		return
	}
	folder := r.URL.Query().Get("folder")
	file := r.URL.Query().Get("file")
	from := r.URL.Query().Get("rev")
	to := r.URL.Query().Get("to")
	rel := h.docRelPath(folder, file)
	if rel == "" || from == "" {
		sendJSON(w, 400, map[string]any{"error": "folder, file and rev required"})
		return
	}

	fromSrc, _, err := h.sourceAt(rel, from)
	if err == os.ErrNotExist {
		sendJSON(w, 404, map[string]any{"error": "revision not found for this document"})
		return
	}
	if err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}

	var toSrc, toLabel string
	if to == "" || to == "working" {
		src, ok := h.currentSource(folder, file)
		if !ok {
			sendJSON(w, 404, map[string]any{"error": "document not found"})
			return
		}
		toSrc, toLabel = src, "current"
	} else {
		src, _, err := h.sourceAt(rel, to)
		if err == os.ErrNotExist {
			sendJSON(w, 404, map[string]any{"error": "revision not found for this document"})
			return
		}
		if err != nil {
			sendJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		toSrc, toLabel = src, to
	}

	name := strings.TrimSuffix(filepath.Base(rel), ".html") + ".md"
	// Short labels keep the diff's ---/+++ header readable; a full SHA reads as
	// a path component.
	diff, err := h.ver.Diff(shortLabel(from), []byte(fromSrc), shortLabel(toLabel), []byte(toSrc), name)
	if err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{
		"ok": true, "from": from, "to": toLabel, "diff": diff, "identical": strings.TrimSpace(diff) == "",
	})
}

// handleRestoreDoc re-renders a past revision as the current document.
//
// This is deliberately not a git revert. The snapshot history is an append-only
// record of what the doc tree looked like over time, so a restore is just
// another edit: the old markdown is re-rendered through the normal generator and
// lands as a new snapshot on top. Nothing is rewritten, and the state being
// replaced is committed first so it stays recoverable.
func (h *Handler) handleRestoreDoc(w http.ResponseWriter, r *http.Request) {
	if !h.requireOwner(w, r) || !h.versionEnabled(w) {
		return
	}
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := strings.TrimSpace(str(body["folder"]))
	file := strings.TrimSpace(str(body["file"]))
	rev := strings.TrimSpace(str(body["rev"]))
	rel := h.docRelPath(folder, file)
	if rel == "" || rev == "" {
		sendJSON(w, 400, map[string]any{"error": "folder, file and rev required"})
		return
	}

	src, isMarkdown, err := h.sourceAt(rel, rev)
	if err == os.ErrNotExist {
		sendJSON(w, 404, map[string]any{"error": "revision not found for this document"})
		return
	}
	if err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if !isMarkdown {
		sendJSON(w, 422, map[string]any{
			"error": "that revision has no embedded markdown source, so it cannot be re-rendered",
		})
		return
	}

	// Capture what is about to be replaced before replacing it.
	h.ver.CommitNow("pre-restore state of " + rel)

	title := ""
	if blob, _, berr := h.sourceAtBlob(rel, rev); berr == nil {
		title = docgen.ExtractTitle(blob)
	}
	if title == "" {
		if data, rerr := os.ReadFile(filepath.Join(h.cfg.DataDir, filepath.FromSlash(rel))); rerr == nil {
			title = docgen.ExtractTitle(data)
		}
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(rel), ".html")
	}

	docURL, genErr := docgen.GenerateDoc(h.dgCfg, folder, title, src, filepath.Base(rel))
	if genErr != nil {
		sendJSON(w, 500, map[string]any{"error": genErr.Error()})
		return
	}
	h.ver.Nudge("restore " + rel + " to " + shortLabel(rev))
	sendJSON(w, 200, map[string]any{"ok": true, "url": docURL, "restored_from": rev, "title": title})
}

// sourceAtBlob returns the raw blob of a document at rev, resolving the path
// through the revision list the same way sourceAt does.
func (h *Handler) sourceAtBlob(rel, rev string) ([]byte, string, error) {
	revs, err := h.ver.History(rel, 200)
	if err != nil {
		return nil, "", err
	}
	for _, rv := range revs {
		if strings.EqualFold(rv.Rev, rev) || rv.Short == strings.ToLower(rev) {
			blob, err := h.ver.FileAt(rv.Rev, rv.Path)
			return blob, rv.Path, err
		}
	}
	return nil, "", os.ErrNotExist
}

func shortLabel(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}
