package server

import (
	"bytes"
	"coherence/internal/auth"
	"coherence/internal/config"
	"coherence/internal/docgen"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const sessionTTL = 86400 * 15 // 15 days
const maxUploadBytes = 50 * 1024 * 1024
const maxImageBytes = 10 * 1024 * 1024

var allowedImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Handler holds shared server state.
type Handler struct {
	cfg    *config.Config
	dgCfg  *docgen.Config
	mux    *http.ServeMux
}

func New(cfg *config.Config, dgCfg *docgen.Config) *Handler {
	h := &Handler{cfg: cfg, dgCfg: dgCfg, mux: http.NewServeMux()}
	h.mux.HandleFunc("/", h.dispatch)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request) {
	// Strip nginx proxy prefix
	p := r.URL.Path
	if strings.HasPrefix(p, "/comment-api") {
		p = p[len("/comment-api"):]
	}

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(200)
		return
	}

	switch r.Method {
	case http.MethodGet:
		switch p {
		case "/comments":
			h.handleGetComments(w, r)
		case "/list-folders":
			h.handleListFolders(w, r)
		case "/search":
			h.handleSearch(w, r)
		case "/auth/check":
			h.handleAuthCheck(w, r)
		case "/auth/login":
			h.handleLoginPage(w, r)
		case "/auth/logout":
			h.handleLogout(w, r)
		case "/auth/share/check":
			h.handleShareCheck(w, r)
		default:
			if strings.HasPrefix(p, "/assets/") {
				h.serveAsset(w, r, p[len("/assets/"):])
			} else {
				h.serveStatic(w, r, p)
			}
		}
	case http.MethodPost:
		switch p {
		case "/comment":
			h.handlePostComment(w, r)
		case "/acknowledge":
			h.handleAcknowledgeComment(w, r)
		case "/delete-doc":
			h.handleDeleteDoc(w, r)
		case "/delete-folder":
			h.handleDeleteFolder(w, r)
		case "/exclude-session":
			h.handleExcludeSession(w, r)
		case "/delete-session":
			h.handleDeleteSession(w, r)
		case "/auth/login":
			h.handleLoginPost(w, r)
		case "/auth/share/create":
			h.handleShareCreate(w, r)
		case "/reply-comment":
			h.handleReplyComment(w, r)
		case "/create-folder":
			h.handleCreateFolder(w, r)
		case "/rename-folder":
			h.handleRenameFolder(w, r)
		case "/move-folder":
			h.handleMoveFolder(w, r)
		case "/rename-doc":
			h.handleRenameDoc(w, r)
		case "/move-doc":
			h.handleMoveDoc(w, r)
		case "/create-doc":
			h.handleCreateDoc(w, r)
		case "/update-doc":
			h.handleUpdateDoc(w, r)
		case "/reindex":
			h.handleReindex(w, r)
		case "/upload-file":
			h.handleUploadFile(w, r)
		case "/upload-image":
			h.handleUploadImage(w, r)
		default:
			sendJSON(w, 404, map[string]any{"error": "not found"})
		}
	default:
		w.WriteHeader(405)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func sendJSON(w http.ResponseWriter, code int, data any) {
	body, _ := json.Marshal(data)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	w.Write(body)
}

func sendHTML(w http.ResponseWriter, code int, body string) {
	b := []byte(body)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(code)
	w.Write(b)
}

func redirect(w http.ResponseWriter, r *http.Request, location string, extraHeaders ...[]string) {
	for _, kv := range extraHeaders {
		w.Header().Add(kv[0], kv[1])
	}
	http.Redirect(w, r, location, http.StatusFound)
}

func readBody(r *http.Request) (map[string]any, error) {
	body := map[string]any{}
	if r.ContentLength == 0 {
		return body, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

func (h *Handler) loadAuthConfig() *auth.AuthConfig {
	return auth.LoadConfig(h.cfg.AuthFile)
}

func (h *Handler) sessionOK(r *http.Request) bool {
	cfg := h.loadAuthConfig()
	if cfg == nil {
		return true // no auth configured — open
	}
	cookie, err := r.Cookie("docs_session")
	if err != nil {
		return false
	}
	return auth.VerifySessionToken(cookie.Value, cfg.SessionSecret)
}

func (h *Handler) apiKeyOK(r *http.Request) bool {
	if h.cfg.APIKey == "" {
		return false
	}
	return r.Header.Get("Authorization") == "Bearer "+h.cfg.APIKey
}

// apiWriteAllowed returns true when the request is permitted to call a write
// endpoint. When no API key is configured the server is in local-only mode and
// all writes are allowed (backward-compatible). When an API key is configured,
// the request must present either a valid session cookie or the API key.
func (h *Handler) apiWriteAllowed(r *http.Request) bool {
	if h.cfg.APIKey == "" {
		return true
	}
	authCfg := h.loadAuthConfig()
	if authCfg != nil && h.sessionOK(r) {
		return true
	}
	return h.apiKeyOK(r)
}

func parseCookies(cookieHeader string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		if k, v, ok := strings.Cut(part, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

var sanitizeNameRe = regexp.MustCompile(`[^a-zA-Z0-9_\-.]`)

// ── comment endpoints ──────────────────────────────────────────────────────

func (h *Handler) handleGetComments(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")
	file := r.URL.Query().Get("file")
	p := safeCommentPath(h.cfg.DataDir, folder, file)
	if p == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder or file"})
		return
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		sendJSON(w, 200, []any{})
		return
	}
	if err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	var comments []any
	json.Unmarshal(data, &comments)
	sendJSON(w, 200, comments)
}

func (h *Handler) handlePostComment(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := str(body["folder"])
	file := str(body["file"])
	text := strings.TrimSpace(str(body["text"]))
	quote := strings.TrimSpace(str(body["quote"]))
	if text == "" {
		sendJSON(w, 400, map[string]any{"error": "text required"})
		return
	}
	p := safeCommentPath(h.cfg.DataDir, folder, file)
	if p == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder or file"})
		return
	}
	os.MkdirAll(filepath.Dir(p), 0755)
	var comments []map[string]any
	if data, err := os.ReadFile(p); err == nil {
		json.Unmarshal(data, &comments)
	}
	entry := map[string]any{
		"ts":     time.Now().UTC().Format(time.RFC3339),
		"text":   text,
		"author": h.currentUser(r),
	}
	if quote != "" {
		entry["quote"] = quote
	}
	comments = append(comments, entry)
	out, _ := json.MarshalIndent(comments, "", "  ")
	os.WriteFile(p, out, 0644)
	sendJSON(w, 200, map[string]any{"ok": true, "entry": entry})
}

func (h *Handler) handleAcknowledgeComment(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := str(body["folder"])
	file := str(body["file"])
	ts := str(body["ts"])
	if ts == "" {
		sendJSON(w, 400, map[string]any{"error": "ts required"})
		return
	}
	p := safeCommentPath(h.cfg.DataDir, folder, file)
	if p == "" {
		sendJSON(w, 404, map[string]any{"error": "comment file not found"})
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		sendJSON(w, 404, map[string]any{"error": "comment file not found"})
		return
	}
	var comments []map[string]any
	json.Unmarshal(data, &comments)
	ackTs := time.Now().UTC().Format(time.RFC3339)
	matched := 0
	for _, c := range comments {
		if c["ts"] == ts {
			c["acknowledged"] = true
			c["ack_ts"] = ackTs
			matched++
		}
	}
	if matched == 0 {
		sendJSON(w, 404, map[string]any{"error": "comment not found"})
		return
	}
	out, _ := json.MarshalIndent(comments, "", "  ")
	os.WriteFile(p, out, 0644)
	sendJSON(w, 200, map[string]any{"ok": true, "ack_ts": ackTs})
}

func (h *Handler) handleReplyComment(w http.ResponseWriter, r *http.Request) {
	isLocal := strings.HasPrefix(r.RemoteAddr, "127.0.0.1:")
	if !isLocal && !h.sessionOK(r) {
		sendJSON(w, 401, map[string]any{"error": "not authenticated"})
		return
	}
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := str(body["folder"])
	file := str(body["file"])
	ts := str(body["ts"])
	reply := strings.TrimSpace(str(body["reply"]))
	if folder == "" || file == "" || ts == "" || reply == "" {
		sendJSON(w, 400, map[string]any{"error": "folder, file, ts, and reply required"})
		return
	}
	p := safeCommentPath(h.cfg.DataDir, folder, file)
	if p == "" {
		sendJSON(w, 404, map[string]any{"error": "comment file not found"})
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		sendJSON(w, 404, map[string]any{"error": "comment file not found"})
		return
	}
	var comments []map[string]any
	json.Unmarshal(data, &comments)
	replyTs := time.Now().UTC().Format(time.RFC3339)
	matched := 0
	replyAuthor := h.currentUser(r)
	for _, c := range comments {
		if c["ts"] == ts {
			c["reply"] = reply
			c["reply_ts"] = replyTs
			c["reply_author"] = replyAuthor
			c["handled"] = true
			matched++
		}
	}
	if matched == 0 {
		sendJSON(w, 404, map[string]any{"error": "comment not found"})
		return
	}
	out, _ := json.MarshalIndent(comments, "", "  ")
	os.WriteFile(p, out, 0644)
	sendJSON(w, 200, map[string]any{"ok": true, "reply_ts": replyTs})
}

// ── folder management ──────────────────────────────────────────────────────

func (h *Handler) handleListFolders(w http.ResponseWriter, r *http.Request) {
	var folders []string
	var recurse func(path, prefix string)
	recurse = func(path, prefix string) {
		entries, err := os.ReadDir(path)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			rel := e.Name()
			if prefix != "" {
				rel = prefix + "/" + e.Name()
			}
			folders = append(folders, rel)
			recurse(filepath.Join(path, e.Name()), rel)
		}
	}
	recurse(h.cfg.DataDir, "")
	sort.Strings(folders)
	sendJSON(w, 200, map[string]any{"folders": folders})
}

func (h *Handler) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := strings.TrimSpace(str(body["folder"]))
	if folder == "" {
		sendJSON(w, 400, map[string]any{"error": "folder required"})
		return
	}
	fp := safeFolderPath(h.cfg.DataDir, folder)
	if fp == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder path"})
		return
	}
	if _, err := os.Stat(fp); err == nil {
		sendJSON(w, 409, map[string]any{"error": "folder already exists"})
		return
	}
	if err := os.MkdirAll(fp, 0755); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	// Chmod ancestors up to DATA_DIR
	p := filepath.Dir(fp)
	dataDirAbs, _ := filepath.Abs(h.cfg.DataDir)
	for p != dataDirAbs && p != filepath.Dir(p) {
		os.Chmod(p, 0755)
		p = filepath.Dir(p)
	}
	go docgen.ReindexAll(h.dgCfg)
	sendJSON(w, 200, map[string]any{"ok": true, "folder": folder, "path": "/" + folder + "/"})
}

func (h *Handler) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := str(body["folder"])
	if folder == "" {
		sendJSON(w, 400, map[string]any{"error": "folder required"})
		return
	}
	fp := safeFolderPath(h.cfg.DataDir, folder)
	if fp == "" {
		sendJSON(w, 404, map[string]any{"error": "folder not found"})
		return
	}
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		sendJSON(w, 404, map[string]any{"error": "folder not found"})
		return
	}
	if err := os.RemoveAll(fp); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	go docgen.ReindexAll(h.dgCfg)
	sendJSON(w, 200, map[string]any{"ok": true, "deleted": folder})
}

func (h *Handler) handleRenameFolder(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	oldFolder := strings.TrimSpace(str(body["folder"]))
	newName := strings.TrimSpace(str(body["new_name"]))
	if oldFolder == "" || newName == "" {
		sendJSON(w, 400, map[string]any{"error": "folder and new_name required"})
		return
	}
	newNameClean := strings.Trim(sanitizeNameRe.ReplaceAllString(newName, "-"), "-")
	if newNameClean == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid new name"})
		return
	}
	oldFp := safeFolderPath(h.cfg.DataDir, oldFolder)
	if oldFp == "" {
		sendJSON(w, 404, map[string]any{"error": "folder not found"})
		return
	}
	if _, err := os.Stat(oldFp); os.IsNotExist(err) {
		sendJSON(w, 404, map[string]any{"error": "folder not found"})
		return
	}
	newFp := filepath.Join(filepath.Dir(oldFp), newNameClean)
	if _, err := os.Stat(newFp); err == nil {
		sendJSON(w, 409, map[string]any{"error": "a folder with that name already exists"})
		return
	}
	dataDirAbs, _ := filepath.Abs(h.cfg.DataDir)
	oldRel, _ := filepath.Rel(dataDirAbs, oldFp)
	if err := os.Rename(oldFp, newFp); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	newRel, _ := filepath.Rel(dataDirAbs, newFp)
	rewriteFolderLinks(newFp, oldRel, newRel)
	go docgen.ReindexAll(h.dgCfg)
	sendJSON(w, 200, map[string]any{"ok": true, "new_folder": newRel, "path": "/" + newRel + "/"})
}

func (h *Handler) handleMoveFolder(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := strings.TrimSpace(str(body["folder"]))
	destParent := strings.TrimSpace(str(body["dest_parent"]))
	if folder == "" {
		sendJSON(w, 400, map[string]any{"error": "folder required"})
		return
	}
	srcFp := safeFolderPath(h.cfg.DataDir, folder)
	if srcFp == "" {
		sendJSON(w, 404, map[string]any{"error": "folder not found"})
		return
	}
	if _, err := os.Stat(srcFp); os.IsNotExist(err) {
		sendJSON(w, 404, map[string]any{"error": "folder not found"})
		return
	}
	var destParentFp string
	if destParent != "" {
		destParentFp = safeFolderPath(h.cfg.DataDir, destParent)
		if destParentFp == "" {
			sendJSON(w, 400, map[string]any{"error": "invalid dest_parent"})
			return
		}
		if _, err := os.Stat(destParentFp); os.IsNotExist(err) {
			sendJSON(w, 404, map[string]any{"error": "dest_parent not found"})
			return
		}
	} else {
		destParentFp = h.cfg.DataDir
	}
	newFp := filepath.Join(destParentFp, filepath.Base(srcFp))
	if newFp == srcFp {
		sendJSON(w, 400, map[string]any{"error": "source and destination are the same"})
		return
	}
	if _, err := os.Stat(newFp); err == nil {
		sendJSON(w, 409, map[string]any{"error": "a folder with that name already exists at destination"})
		return
	}
	dataDirAbs, _ := filepath.Abs(h.cfg.DataDir)
	newFpAbs, _ := filepath.Abs(newFp)
	if !strings.HasPrefix(newFpAbs, dataDirAbs+string(filepath.Separator)) {
		sendJSON(w, 400, map[string]any{"error": "destination outside data dir"})
		return
	}
	oldRel, _ := filepath.Rel(dataDirAbs, srcFp)
	if err := os.Rename(srcFp, newFp); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	newRel, _ := filepath.Rel(dataDirAbs, newFp)
	rewriteFolderLinks(newFp, oldRel, newRel)
	go docgen.ReindexAll(h.dgCfg)
	sendJSON(w, 200, map[string]any{"ok": true, "new_folder": newRel, "path": "/" + newRel + "/"})
}

// ── doc management ─────────────────────────────────────────────────────────

func (h *Handler) handleDeleteDoc(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := str(body["folder"])
	file := strings.TrimSuffix(str(body["file"]), ".html")
	if folder == "" || file == "" {
		sendJSON(w, 400, map[string]any{"error": "folder and file required"})
		return
	}
	folderPath := safeFolderPath(h.cfg.DataDir, folder)
	if folderPath == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder"})
		return
	}
	fileClean := allowedFileRe.ReplaceAllString(file, "")
	docPath := filepath.Join(folderPath, fileClean+".html")
	commentsPath := filepath.Join(folderPath, fileClean+".comments.json")
	if _, err := os.Stat(docPath); os.IsNotExist(err) {
		sendJSON(w, 404, map[string]any{"error": "document not found"})
		return
	}
	os.Remove(docPath)
	os.Remove(commentsPath)
	go docgen.ReindexAll(h.dgCfg)
	sendJSON(w, 200, map[string]any{"ok": true, "deleted": folder + "/" + fileClean + ".html"})
}

func (h *Handler) handleRenameDoc(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := strings.TrimSpace(str(body["folder"]))
	filename := strings.TrimSpace(str(body["filename"]))
	newName := strings.TrimSpace(str(body["new_name"]))
	if folder == "" || filename == "" || newName == "" {
		sendJSON(w, 400, map[string]any{"error": "folder, filename, and new_name required"})
		return
	}
	fp, oldPath := safeDocPath(h.cfg.DataDir, folder, filename)
	if fp == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder or filename"})
		return
	}
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		sendJSON(w, 404, map[string]any{"error": "document not found"})
		return
	}
	newNameClean := strings.Trim(sanitizeNameRe.ReplaceAllString(newName, "-"), "-")
	if !strings.HasSuffix(newNameClean, ".html") {
		newNameClean += ".html"
	}
	newPath := filepath.Join(fp, newNameClean)
	if _, err := os.Stat(newPath); err == nil {
		sendJSON(w, 409, map[string]any{"error": "a document with that name already exists"})
		return
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	oldStem := strings.TrimSuffix(filepath.Base(oldPath), ".html")
	newStem := strings.TrimSuffix(newNameClean, ".html")
	oldComments := filepath.Join(fp, oldStem+".comments.json")
	newComments := filepath.Join(fp, newStem+".comments.json")
	if _, err := os.Stat(oldComments); err == nil {
		os.Rename(oldComments, newComments)
	}
	patchDocVars(newPath, "", newStem)
	go docgen.ReindexAll(h.dgCfg)
	sendJSON(w, 200, map[string]any{"ok": true, "path": "/" + folder + "/" + newNameClean})
}

func (h *Handler) handleMoveDoc(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := strings.TrimSpace(str(body["folder"]))
	filename := strings.TrimSpace(str(body["filename"]))
	destFolder := strings.TrimSpace(str(body["dest_folder"]))
	if folder == "" || filename == "" || destFolder == "" {
		sendJSON(w, 400, map[string]any{"error": "folder, filename, and dest_folder required"})
		return
	}
	srcFp, srcPath := safeDocPath(h.cfg.DataDir, folder, filename)
	if srcFp == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder or filename"})
		return
	}
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		sendJSON(w, 404, map[string]any{"error": "document not found"})
		return
	}
	dstFp := safeFolderPath(h.cfg.DataDir, destFolder)
	if dstFp == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid dest_folder"})
		return
	}
	if _, err := os.Stat(dstFp); os.IsNotExist(err) {
		sendJSON(w, 404, map[string]any{"error": "dest_folder not found"})
		return
	}
	dstPath := filepath.Join(dstFp, filepath.Base(srcPath))
	if dstPath == srcPath {
		sendJSON(w, 400, map[string]any{"error": "source and destination are the same"})
		return
	}
	if _, err := os.Stat(dstPath); err == nil {
		sendJSON(w, 409, map[string]any{"error": "a document with that name already exists at destination"})
		return
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	stem := strings.TrimSuffix(filepath.Base(srcPath), ".html")
	srcComments := filepath.Join(srcFp, stem+".comments.json")
	if _, err := os.Stat(srcComments); err == nil {
		os.Rename(srcComments, filepath.Join(dstFp, stem+".comments.json"))
	}
	patchDocVars(dstPath, destFolder, stem)
	go docgen.ReindexAll(h.dgCfg)
	newRel := "/" + destFolder + "/" + filepath.Base(srcPath)
	sendJSON(w, 200, map[string]any{"ok": true, "path": newRel})
}

func (h *Handler) handleReindex(w http.ResponseWriter, r *http.Request) {
	if !h.apiWriteAllowed(r) {
		sendJSON(w, 401, map[string]any{"error": "not authenticated"})
		return
	}
	go docgen.ReindexAll(h.dgCfg)
	sendJSON(w, 200, map[string]any{"ok": true})
}

func (h *Handler) handleCreateDoc(w http.ResponseWriter, r *http.Request) {
	if !h.apiWriteAllowed(r) {
		sendJSON(w, 401, map[string]any{"error": "not authenticated"})
		return
	}
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := strings.TrimSpace(str(body["folder"]))
	title := strings.TrimSpace(str(body["title"]))
	filename := strings.TrimSpace(str(body["filename"]))
	content := str(body["content"])
	overwrite, _ := body["overwrite"].(bool)

	if folder == "" || title == "" {
		sendJSON(w, 400, map[string]any{"error": "folder and title required"})
		return
	}
	if filename == "" {
		filename = slugify(title) + ".html"
	}
	fp, dest := safeDocPath(h.cfg.DataDir, folder, filename)
	if fp == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder or filename"})
		return
	}
	if _, err := os.Stat(dest); err == nil && !overwrite {
		sendJSON(w, 409, map[string]any{"error": "file already exists", "filename": filepath.Base(dest)})
		return
	}
	docURL, genErr := docgen.GenerateDoc(h.dgCfg, folder, title, content, filepath.Base(dest))
	if genErr != nil {
		sendJSON(w, 500, map[string]any{"error": genErr.Error()})
		return
	}
	dataDirAbs, _ := filepath.Abs(h.cfg.DataDir)
	destAbs, _ := filepath.Abs(dest)
	rel, _ := filepath.Rel(dataDirAbs, destAbs)
	sendJSON(w, 200, map[string]any{"ok": true, "url": docURL, "path": "/" + rel})
}

func (h *Handler) handleUpdateDoc(w http.ResponseWriter, r *http.Request) {
	if !h.apiWriteAllowed(r) {
		sendJSON(w, 401, map[string]any{"error": "not authenticated"})
		return
	}
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := strings.TrimSpace(str(body["folder"]))
	filename := strings.TrimSpace(str(body["filename"]))
	title := strings.TrimSpace(str(body["title"]))
	content := str(body["content"])
	if folder == "" || filename == "" {
		sendJSON(w, 400, map[string]any{"error": "folder and filename required"})
		return
	}
	fp, dest := safeDocPath(h.cfg.DataDir, folder, filename)
	if fp == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder or filename"})
		return
	}
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		sendJSON(w, 404, map[string]any{"error": "document not found"})
		return
	}
	if title == "" {
		if data, err := os.ReadFile(dest); err == nil {
			if m := regexp.MustCompile(`<title>(.*?)</title>`).FindSubmatch(data); m != nil {
				title = strings.Split(strings.TrimSpace(string(m[1])), " — ")[0]
			}
		}
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(dest), ".html")
		}
	}
	docURL, genErr := docgen.GenerateDoc(h.dgCfg, folder, title, content, filepath.Base(dest))
	if genErr != nil {
		sendJSON(w, 500, map[string]any{"error": genErr.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "url": docURL})
}

// ── session endpoints ──────────────────────────────────────────────────────

func (h *Handler) handleExcludeSession(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := str(body["folder"])
	sessionID := strings.TrimSpace(str(body["session_id"]))
	if folder == "" || sessionID == "" {
		sendJSON(w, 400, map[string]any{"error": "folder and session_id required"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "excluded": sessionID})
}

var uuidRe = regexp.MustCompile(`^[0-9a-f\-]{36}$`)

func (h *Handler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	folder := str(body["folder"])
	sessionID := strings.TrimSpace(str(body["session_id"]))
	if folder == "" || sessionID == "" {
		sendJSON(w, 400, map[string]any{"error": "folder and session_id required"})
		return
	}
	if !uuidRe.MatchString(sessionID) {
		sendJSON(w, 400, map[string]any{"error": "invalid session_id"})
		return
	}
	home, _ := os.UserHomeDir()
	projectsDir := filepath.Join(home, ".claude", "projects")
	var deletedFile string
	filepath.WalkDir(projectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == sessionID+".jsonl" {
			os.Remove(path)
			deletedFile = path
			return filepath.SkipAll
		}
		return nil
	})
	// Remove from sessions index
	idxPath := filepath.Join(home, ".claude", "sessions", "index.json")
	if data, err := os.ReadFile(idxPath); err == nil {
		var idx map[string]any
		if json.Unmarshal(data, &idx) == nil {
			if sessions, ok := idx["sessions"].([]any); ok {
				var kept []any
				for _, s := range sessions {
					if sm, ok := s.(map[string]any); ok {
						if sm["session_id"] != sessionID {
							kept = append(kept, s)
						}
					}
				}
				idx["sessions"] = kept
				if out, err := json.MarshalIndent(idx, "", "  "); err == nil {
					os.WriteFile(idxPath, out, 0644)
				}
			}
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "deleted_file": deletedFile})
}

// ── search ─────────────────────────────────────────────────────────────────

var tagRe = regexp.MustCompile(`<[^>]+>`)
var wsRe = regexp.MustCompile(`\s+`)
var titleRe = regexp.MustCompile(`(?i)<title>([^<]*)</title>`)
var contentRe = regexp.MustCompile(`(?is)<(?:div|main)[^>]+class="[^"]*content[^"]*"[^>]*>(.*?)</(?:div|main)>`)

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	if q == "" || len(q) < 2 {
		sendJSON(w, 200, map[string]any{"results": []any{}, "query": q})
		return
	}
	terms := strings.Fields(q)
	type result struct {
		Folder  string `json:"folder"`
		File    string `json:"file"`
		Title   string `json:"title"`
		Snippet string `json:"snippet"`
		URL     string `json:"url"`
		Mtime   int64  `json:"mtime"`
	}
	var results []result
	dataDirAbs, _ := filepath.Abs(h.cfg.DataDir)
	filepath.WalkDir(dataDirAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".html") || d.Name() == "index.html" {
			return nil
		}
		if len(results) >= 30 {
			return filepath.SkipAll
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		raw, _ := io.ReadAll(io.LimitReader(f, 40000))
		rawStr := string(raw)

		var title string
		if m := titleRe.FindStringSubmatch(rawStr); m != nil {
			title = html.UnescapeString(strings.TrimSpace(m[1]))
		} else {
			title = strings.TrimSuffix(d.Name(), ".html")
		}

		var bodyHTML string
		if m := contentRe.FindStringSubmatch(rawStr); m != nil {
			bodyHTML = m[1]
		} else {
			bodyHTML = rawStr
		}
		bodyText := wsRe.ReplaceAllString(tagRe.ReplaceAllString(html.UnescapeString(bodyHTML), " "), " ")
		bodyLower := strings.ToLower(bodyText)
		titleLower := strings.ToLower(title)

		for _, t := range terms {
			if !strings.Contains(titleLower, t) && !strings.Contains(bodyLower, t) {
				return nil
			}
		}

		snippet := ""
		if idx := strings.Index(bodyLower, terms[0]); idx >= 0 {
			start := idx - 60
			if start < 0 {
				start = 0
			}
			end := idx + 120
			if end > len(bodyLower) {
				end = len(bodyLower)
			}
			prefix := ""
			if start > 0 {
				prefix = "…"
			}
			suffix := ""
			if end < len(bodyLower) {
				suffix = "…"
			}
			snippet = prefix + strings.TrimSpace(bodyText[start:end]) + suffix
		} else {
			if len(bodyText) > 160 {
				snippet = bodyText[:160] + "…"
			} else {
				snippet = bodyText
			}
		}

		rel, _ := filepath.Rel(dataDirAbs, path)
		folderPath := ""
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) > 1 {
			folderPath = strings.Join(parts[:len(parts)-1], "/")
		}
		info, _ := d.Info()
		var mtime int64
		if info != nil {
			mtime = info.ModTime().UnixMilli()
		}
		results = append(results, result{
			Folder:  folderPath,
			File:    d.Name(),
			Title:   title,
			Snippet: snippet,
			URL:     "/" + filepath.ToSlash(rel),
			Mtime:   mtime,
		})
		return nil
	})
	sort.Slice(results, func(i, j int) bool { return results[i].Mtime > results[j].Mtime })
	if len(results) > 20 {
		results = results[:20]
	}
	sendJSON(w, 200, map[string]any{"results": results, "query": q})
}

// ── upload ─────────────────────────────────────────────────────────────────

func (h *Handler) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if !strings.Contains(ct, "multipart/form-data") {
		sendJSON(w, 400, map[string]any{"error": "multipart/form-data required"})
		return
	}
	if r.ContentLength > maxUploadBytes {
		sendJSON(w, 413, map[string]any{"error": "file too large (max 50MB)"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		sendJSON(w, 400, map[string]any{"error": "parse error: " + err.Error()})
		return
	}
	folder := strings.TrimSpace(r.FormValue("folder"))
	rename := strings.TrimSpace(r.FormValue("rename"))
	if folder == "" {
		sendJSON(w, 400, map[string]any{"error": "folder required"})
		return
	}
	fp := safeFolderPath(h.cfg.DataDir, folder)
	if fp == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "no file provided"})
		return
	}
	defer file.Close()
	logsDir := filepath.Join(fp, "logs")
	os.MkdirAll(logsDir, 0755)
	os.Chmod(fp, 0755)
	os.Chmod(logsDir, 0755)

	destName := sanitizeNameRe.ReplaceAllString(rename, "_")
	if destName == "" {
		destName = sanitizeNameRe.ReplaceAllString(header.Filename, "_")
	}
	destFile := filepath.Join(logsDir, destName)
	data, _ := io.ReadAll(file)
	if err := os.WriteFile(destFile, data, 0644); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	dataDirAbs, _ := filepath.Abs(h.cfg.DataDir)
	rel, _ := filepath.Rel(dataDirAbs, destFile)
	go docgen.ReindexAll(h.dgCfg)
	sendJSON(w, 200, map[string]any{"ok": true, "path": rel, "size": len(data)})
}

func (h *Handler) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if !strings.Contains(ct, "multipart/form-data") {
		sendJSON(w, 400, map[string]any{"error": "multipart/form-data required"})
		return
	}
	if r.ContentLength > maxImageBytes {
		sendJSON(w, 413, map[string]any{"error": "image too large (max 10MB)"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes)
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		sendJSON(w, 400, map[string]any{"error": "parse error: " + err.Error()})
		return
	}
	folder := strings.TrimSpace(r.FormValue("folder"))
	if folder == "" {
		sendJSON(w, 400, map[string]any{"error": "folder required"})
		return
	}
	fp := safeFolderPath(h.cfg.DataDir, folder)
	if fp == "" {
		sendJSON(w, 400, map[string]any{"error": "invalid folder"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "no file provided"})
		return
	}
	defer file.Close()

	data, _ := io.ReadAll(file)

	detectedMIME := http.DetectContentType(data)
	ext, ok := allowedImageTypes[detectedMIME]
	if !ok {
		sendJSON(w, 400, map[string]any{"error": "file must be a PNG, JPEG, GIF, or WebP image"})
		return
	}

	origBase := strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	base := sanitizeNameRe.ReplaceAllString(origBase, "_")
	if base == "" {
		base = "image"
	}
	destName := fmt.Sprintf("%d-%s%s", time.Now().UnixMilli(), base, ext)

	imagesDir := filepath.Join(fp, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	os.Chmod(imagesDir, 0755)
	os.Chmod(fp, 0755)

	destFile := filepath.Join(imagesDir, destName)
	if err := os.WriteFile(destFile, data, 0644); err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}

	dataDirAbs, _ := filepath.Abs(h.cfg.DataDir)
	rel, _ := filepath.Rel(dataDirAbs, destFile)
	urlPath := "/" + filepath.ToSlash(rel)
	sendJSON(w, 200, map[string]any{
		"ok":       true,
		"path":     urlPath,
		"markdown": fmt.Sprintf("![image](%s)", urlPath),
		"size":     len(data),
	})
}

// ── auth ───────────────────────────────────────────────────────────────────

func (h *Handler) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	cfg := h.loadAuthConfig()
	if cfg == nil {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(200)
		return
	}
	origURI := r.Header.Get("X-Original-URI")
	if origURI == "" {
		origURI = r.URL.String()
	}
	parsed, _ := url.Parse(origURI)
	origPath := parsed.Path
	shareToken := parsed.Query().Get("share")
	if shareToken != "" {
		if auth.CheckShare(h.cfg.SharesFile, shareToken, origPath) {
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(200)
			return
		}
	}
	if h.sessionOK(r) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(200)
	} else {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(401)
	}
}

const loginPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Sign in — coherence</title>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{background:#f6f8fa;color:#1f2328;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;padding:20px}
.card{background:#ffffff;border:1px solid #d0d7de;border-radius:8px;padding:32px 36px;width:100%;max-width:360px;box-shadow:0 1px 3px rgba(31,35,40,.12)}
.logo{display:flex;align-items:center;gap:8px;margin-bottom:24px;text-decoration:none;color:#1f2328}
.logo-sep{color:#d0d7de;font-size:20px;font-weight:100;margin:0 2px;line-height:1}
.logo-name{font-size:14px;font-weight:600;color:#57606a}
h1{font-size:18px;font-weight:600;margin-bottom:20px;color:#1f2328}
label{display:block;font-size:13px;color:#57606a;margin-bottom:5px}
input[type=password]{width:100%;background:#ffffff;border:1px solid #d0d7de;border-radius:6px;color:#1f2328;font-size:14px;padding:8px 12px;outline:none;transition:border-color .15s}
input[type=password]:focus{border-color:#0969da;box-shadow:0 0 0 3px rgba(9,105,218,.15)}
.btn{display:block;width:100%;margin-top:16px;background:#0969da;border:1px solid rgba(0,0,0,.1);color:#fff;border-radius:6px;padding:8px 18px;font-size:14px;font-weight:600;cursor:pointer;transition:background .15s}
.btn:hover{background:#0860ca}
.error{background:rgba(207,34,46,.08);border:1px solid rgba(207,34,46,.3);color:#cf222e;border-radius:6px;padding:10px 14px;font-size:13px;margin-bottom:16px}
</style>
</head>
<body>
<div class="card">
  <a class="logo" href="/">
    <svg width="22" height="22" viewBox="0 0 22 22" fill="none" xmlns="http://www.w3.org/2000/svg"><rect x="4" y="2" width="11" height="14" rx="2" fill="#0969da" opacity="0.15"/><rect x="4" y="2" width="11" height="14" rx="2" stroke="#0969da" stroke-width="1.5"/><rect x="7" y="6" width="14" height="14" rx="2" fill="#ffffff" stroke="#0969da" stroke-width="1.5"/><path d="M13 10.5l-2.5 3.5h2l-1 3.5 3.5-4.5h-2.2l1.2-2.5z" fill="#0969da"/></svg>
    <span class="logo-sep">/</span>
    <span class="logo-name">coherence</span>
  </a>
  <h1>Sign in</h1>
  __ERROR_HTML__
  <form method="POST" action="/auth/login">
    <input type="hidden" name="next" value="__NEXT_URL__">
    <label for="pw">Password</label>
    <input type="password" id="pw" name="password" autofocus autocomplete="current-password">
    <button class="btn" type="submit">Sign in</button>
  </form>
</div>
</body>
</html>`

func (h *Handler) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	nextURL := r.URL.Query().Get("next")
	if nextURL == "" {
		nextURL = "/"
	}
	errMsg := r.URL.Query().Get("error")
	errHTML := ""
	if errMsg != "" {
		errHTML = `<div class="error">` + html.EscapeString(errMsg) + `</div>`
	}
	page := strings.ReplaceAll(loginPage, "__NEXT_URL__", html.EscapeString(nextURL))
	page = strings.ReplaceAll(page, "__ERROR_HTML__", errHTML)
	sendHTML(w, 200, page)
}

func (h *Handler) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	pw := r.FormValue("password")
	nextU := r.FormValue("next")
	if nextU == "" {
		nextU = "/"
	}
	cfg := h.loadAuthConfig()
	if cfg == nil {
		http.Redirect(w, r, "/auth/login?error=Auth+not+configured&next="+url.QueryEscape(nextU), http.StatusFound)
		return
	}
	pwHash := auth.HashPassword(pw)
	if !auth.ConstantTimeEqual(pwHash, cfg.PasswordHash) {
		http.Redirect(w, r, "/auth/login?error=Incorrect+password&next="+url.QueryEscape(nextU), http.StatusFound)
		return
	}
	token := auth.MakeSessionToken(cfg.SessionSecret)
	cookie := fmt.Sprintf("docs_session=%s; Path=/; Max-Age=%d; HttpOnly; SameSite=Lax; Secure", token, sessionTTL)
	w.Header().Set("Set-Cookie", cookie)
	http.Redirect(w, r, nextU, http.StatusFound)
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Set-Cookie", "docs_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax; Secure")
	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

func (h *Handler) handleShareCheck(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	if auth.CheckShare(h.cfg.SharesFile, token, path) {
		sendJSON(w, 200, map[string]any{"ok": true})
	} else {
		sendJSON(w, 403, map[string]any{"ok": false, "error": "invalid or expired token"})
	}
}

func (h *Handler) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	if !h.apiWriteAllowed(r) {
		sendJSON(w, 401, map[string]any{"error": "not authenticated"})
		return
	}
	body, err := readBody(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	path := strings.TrimSpace(str(body["path"]))
	if path == "" {
		path = "/"
	}
	days := 30
	if d, ok := body["days"]; ok {
		switch v := d.(type) {
		case float64:
			days = int(v)
		case string:
			days, _ = strconv.Atoi(v)
		}
	}
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	token, expires, err := auth.CreateShare(h.cfg.SharesFile, path, days)
	if err != nil {
		sendJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	expDt := time.Unix(expires, 0).Format("Jan 02, 2006")
	sendJSON(w, 200, map[string]any{
		"token":   token,
		"url":     h.cfg.DocBase + path + "?share=" + token,
		"expires": expDt,
		"days":    days,
		"path":    path,
	})
}

// ── user identity ──────────────────────────────────────────────────────────

func (h *Handler) currentUser(r *http.Request) string {
	if h.cfg.RemoteUserJWTHeader != "" {
		if jwt := r.Header.Get(h.cfg.RemoteUserJWTHeader); jwt != "" {
			if email := extractEmailFromJWT(jwt); email != "" {
				return email
			}
		}
	}
	if h.cfg.RemoteUserHeader != "" {
		if user := r.Header.Get(h.cfg.RemoteUserHeader); user != "" {
			return user
		}
	}
	return "anonymous"
}

// extractEmailFromJWT decodes a JWT payload and returns the "email" claim.
// No signature verification — the auth proxy already verified the token.
func extractEmailFromJWT(token string) string {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if email, ok := claims["email"].(string); ok {
		return email
	}
	return ""
}

// userAllowed checks the user against the configured allowlist.
// When no allowlist is configured, all non-anonymous users are allowed.
// The check is skipped entirely (returns true) when neither AllowedUsers
// nor AllowedDomain is set, preserving open-access for no-proxy deployments.
func (h *Handler) userAllowed(user string) bool {
	if len(h.cfg.AllowedUsers) == 0 && h.cfg.AllowedDomain == "" {
		return true
	}
	if user == "anonymous" {
		return false
	}
	for _, u := range h.cfg.AllowedUsers {
		if strings.EqualFold(u, user) {
			return true
		}
	}
	if h.cfg.AllowedDomain != "" {
		return strings.HasSuffix(strings.ToLower(user), "@"+strings.ToLower(h.cfg.AllowedDomain))
	}
	return false
}

// ── static file serving ────────────────────────────────────────────────────

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request, relPath string) {
	relPath = strings.TrimLeft(relPath, "/")
	assetsDir := filepath.Join(h.cfg.CoherenceHome, "www", "assets")
	fp, err := filepath.Abs(filepath.Join(assetsDir, relPath))
	if err != nil {
		w.WriteHeader(403)
		return
	}
	assetsDirAbs, _ := filepath.Abs(assetsDir)
	if !strings.HasPrefix(fp, assetsDirAbs+string(filepath.Separator)) {
		w.WriteHeader(403)
		return
	}
	info, err := os.Stat(fp)
	if err != nil || !info.Mode().IsRegular() {
		w.WriteHeader(404)
		return
	}
	mimeType := mime.TypeByExtension(filepath.Ext(fp))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.WriteHeader(200)
	w.Write(data)
}

func (h *Handler) serveStatic(w http.ResponseWriter, r *http.Request, path string) {
	shareToken := r.URL.Query().Get("share")

	cfg := h.loadAuthConfig()
	if cfg != nil {
		if shareToken != "" {
			if !auth.CheckShare(h.cfg.SharesFile, shareToken, path) && !h.sessionOK(r) {
				http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(path), http.StatusFound)
				return
			}
		} else if !h.sessionOK(r) {
			http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(path), http.StatusFound)
			return
		}
	}

	// Allowlist check — only enforced when ALLOWED_USERS or ALLOWED_DOMAIN is configured.
	// Share tokens always bypass this check. When GUEST_ACCESS=true, authenticated
	// non-allowed users are served the page in read-only mode instead of getting 403.
	user := h.currentUser(r)
	validShare := shareToken != "" && auth.CheckShare(h.cfg.SharesFile, shareToken, path)
	isOwner := h.userAllowed(user) || validShare
	if !isOwner && !h.cfg.GuestAccess {
		sendHTML(w, 403, "<h1>403 Forbidden</h1><p>Your account is not authorized.</p>")
		return
	}

	// Resolve path to file
	var parts []string
	for _, seg := range strings.Split(path, "/") {
		if seg != "" && seg != ".." {
			parts = append(parts, seg)
		}
	}
	var fp string
	if len(parts) == 0 {
		fp = filepath.Join(h.cfg.DataDir, "index.html")
	} else {
		candidate := filepath.Join(append([]string{h.cfg.DataDir}, parts...)...)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			fp = filepath.Join(candidate, "index.html")
		} else if filepath.Ext(candidate) == "" {
			fp = candidate + ".html"
		} else {
			fp = candidate
		}
	}

	dataDirAbs, _ := filepath.Abs(h.cfg.DataDir)
	fpAbs, err := filepath.Abs(fp)
	if err != nil || !strings.HasPrefix(fpAbs, dataDirAbs) {
		w.WriteHeader(403)
		return
	}
	data, err := os.ReadFile(fpAbs)
	if err != nil {
		w.WriteHeader(404)
		return
	}
	mimeType := mime.TypeByExtension(filepath.Ext(fpAbs))
	if mimeType == "" {
		mimeType = "text/html"
	}
	if strings.Contains(mimeType, "html") {
		userJSON, _ := json.Marshal(user)
		ownerJSON, _ := json.Marshal(isOwner)
		inject := []byte(`<script>window.REMOTE_USER=` + string(userJSON) + `;window.IS_OWNER=` + string(ownerJSON) + `;</script>`)
		data = bytes.Replace(data, []byte("</head>"), append(inject, []byte("</head>")...), 1)
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(200)
	w.Write(data)
}

// ── internal helpers ───────────────────────────────────────────────────────

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slugify(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func patchDocVars(path, folder, filenameStem string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	s := string(data)
	if folder != "" {
		folderJSON, _ := json.Marshal(folder)
		s = regexp.MustCompile(`(window\.DOC_FOLDER\s*=\s*)"[^"]*"`).
			ReplaceAllString(s, `${1}`+string(folderJSON))
	}
	if filenameStem != "" {
		stemJSON, _ := json.Marshal(filenameStem)
		s = regexp.MustCompile(`(window\.DOC_FILE\s*=\s*)"[^"]*"`).
			ReplaceAllString(s, `${1}`+string(stemJSON))
	}
	os.WriteFile(path, []byte(s), 0644)
}

func rewriteFolderLinks(folderPath, oldRel, newRel string) {
	oldSeg := "/" + strings.Trim(oldRel, "/") + "/"
	newSeg := "/" + strings.Trim(newRel, "/") + "/"
	entries, _ := filepath.Glob(filepath.Join(folderPath, "*.html"))
	for _, htmlFile := range entries {
		if filepath.Base(htmlFile) == "index.html" {
			continue
		}
		data, err := os.ReadFile(htmlFile)
		if err != nil {
			continue
		}
		s := string(data)
		if !strings.Contains(s, oldSeg) {
			continue
		}
		s = strings.ReplaceAll(s, oldSeg, newSeg)
		newRelJSON, _ := json.Marshal(strings.Trim(newRel, "/"))
		s = regexp.MustCompile(`(window\.DOC_FOLDER\s*=\s*)"[^"]*"`).
			ReplaceAllString(s, `${1}`+string(newRelJSON))
		os.WriteFile(htmlFile, []byte(s), 0644)
	}
}
