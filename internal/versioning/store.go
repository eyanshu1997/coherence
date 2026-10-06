// Package versioning keeps a git-backed snapshot history of the generated doc
// tree.
//
// Two properties are deliberate:
//
//   - The repository lives OUTSIDE the served data directory. serveStatic will
//     happily serve any path under DataDir that is not "..", so a .git inside
//     DataDir would let any reader walk loose objects and reconstruct documents
//     that were intentionally deleted.
//   - Snapshots are debounced and committed by a single serialized worker. The
//     browser editor autosaves every 2s and several handlers reindex the tree
//     from a goroutine, so commit-per-write would both spam history and race.
package versioning

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultDebounce is how long the store waits for writes to settle before
// cutting a snapshot.
const DefaultDebounce = 60 * time.Second

// maxDelayFactor bounds the debounce: a continuous stream of writes (an editing
// session autosaving every 2s) must not postpone the snapshot forever, so once
// debounce*maxDelayFactor has elapsed since the first pending write the snapshot
// is cut regardless of further activity.
const maxDelayFactor = 5

// gitTimeout caps every git invocation so a wedged git can never hang a request.
const gitTimeout = 2 * time.Minute

// maxReasons bounds how many write reasons are folded into one commit subject.
const maxReasons = 6

// excludePatterns is written to $GIT_DIR/info/exclude on every start, so pattern
// changes take effect on upgrade without touching the data directory.
//
// index.html is derived — every doc write regenerates every ancestor index plus
// the home page, so tracking it would turn each edit into a tree-wide diff.
// Logs are large and reproducible from Jenkins/Snowflake; one in this tree is
// 66MB. A nested .git is excluded so an embedded repo can never be half-tracked.
var excludePatterns = []string{
	"# Managed by coherence — edits here are overwritten on server start.",
	"index.html",
	"*.pyc",
	"__pycache__/",
	".git/",
	".DS_Store",
}

// DefaultMaxBlobBytes bounds what a snapshot will take in.
//
// This used to be an extension list (*.log, *.jsonl), which was the wrong axis:
// the upload handler puts every generic upload under <folder>/logs/ keeping the
// uploader's extension, and .log is a first-class viewable document type — so
// uploading a log returned ok and then silently versioned nothing. Size is the
// property actually worth excluding on. One log in a real tree is 66MB; the
// small ones are recoverable like any other document.
const DefaultMaxBlobBytes = 8 << 20

var (
	revRe   = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	shortRe = regexp.MustCompile(`[^0-9a-f]`)
)

// Revision is one snapshot in a document's history.
type Revision struct {
	Rev     string `json:"rev"`
	Short   string `json:"short"`
	Unix    int64  `json:"unix"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	// Path is the document's path as of this revision. It differs from the
	// current path when the doc has been renamed or moved, and is what must be
	// handed to FileAt — "rev:current/path" does not resolve across a rename.
	Path string `json:"path"`
}

// Store is a git snapshot repository over the doc tree.
type Store struct {
	gitDir   string
	workTree string
	debounce time.Duration
	maxDelay time.Duration

	maxBlob int64

	mu      sync.Mutex
	timer   *time.Timer
	reasons []string
	firstAt time.Time
	lastErr error

	// commitSeq serializes the add+commit pair. Index manipulation is not
	// concurrency-safe and the async reindex goroutines can overlap.
	commitSeq sync.Mutex
}

// New opens (and creates on first use) the snapshot repository. A nil Store is
// usable and inert, so a caller that cannot initialize versioning can keep
// running with history disabled.
func New(gitDir, workTree string, debounce time.Duration) (*Store, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git not found on PATH: %w", err)
	}
	wt, err := filepath.Abs(workTree)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(wt); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("work tree %s is not a directory", wt)
	}
	gd, err := filepath.Abs(gitDir)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(gd, wt+string(filepath.Separator)) {
		return nil, fmt.Errorf("version store %s must not live inside the served data dir %s", gd, wt)
	}

	s := &Store{
		gitDir:   gd,
		workTree: wt,
		debounce: debounce,
		maxDelay: debounce * maxDelayFactor,
		maxBlob:  DefaultMaxBlobBytes,
	}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	if _, err := os.Stat(filepath.Join(s.gitDir, "HEAD")); err != nil {
		if err := os.MkdirAll(s.gitDir, 0700); err != nil {
			return err
		}
		if _, err := s.plain(nil, "init", "--bare", "--initial-branch=main", s.gitDir); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}
	// core.bare=false + core.worktree is the standard "separate work tree"
	// arrangement: the repository is self-contained but tracks DataDir.
	cfg := [][2]string{
		{"core.bare", "false"},
		{"core.worktree", s.workTree},
		{"core.autocrlf", "false"},
		{"user.name", "coherence"},
		{"user.email", "coherence@localhost"},
		{"commit.gpgsign", "false"},
		{"gc.auto", "256"},
		// Explicit rather than relying on the side effect of core.bare=false:
		// without a reflog, a clobbered ref would have no recovery path.
		{"core.logAllRefUpdates", "true"},
	}
	for _, kv := range cfg {
		if _, err := s.run("config", kv[0], kv[1]); err != nil {
			return fmt.Errorf("git config %s: %w", kv[0], err)
		}
	}
	infoDir := filepath.Join(s.gitDir, "info")
	if err := os.MkdirAll(infoDir, 0700); err != nil {
		return err
	}
	body := strings.Join(excludePatterns, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(infoDir, "exclude"), []byte(body), 0600); err != nil {
		return err
	}
	s.warnInTreeIgnores()
	return nil
}

// warnInTreeIgnores reports .gitignore files inside the doc tree. info/exclude
// is managed here, but an in-tree .gitignore is also honoured — and copying a
// repo-shaped folder into the data dir is a normal workflow, which can import
// rules that quietly stop real documents from being versioned.
func (s *Store) warnInTreeIgnores() {
	filepath.Walk(s.workTree, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != s.workTree {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == ".gitignore" {
			rel, _ := filepath.Rel(s.workTree, path)
			log.Printf("versioning: %s is honoured by snapshots and may exclude documents; "+
				"check it with: git --git-dir=%s --work-tree=%s check-ignore -v <path>",
				filepath.ToSlash(rel), s.gitDir, s.workTree)
		}
		return nil
	})
}

// SetMaxBlobBytes overrides the size above which a file is left out of
// snapshots. Zero disables the limit.
func (s *Store) SetMaxBlobBytes(n int64) {
	if s == nil {
		return
	}
	s.maxBlob = n
}

// LastError reports the most recent snapshot failure, so callers can surface a
// store that has silently stopped recording. Nil once a snapshot succeeds.
func (s *Store) LastError() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// Enabled reports whether snapshots are being taken.
func (s *Store) Enabled() bool { return s != nil }

// GitDir returns the repository location, for diagnostics.
func (s *Store) GitDir() string {
	if s == nil {
		return ""
	}
	return s.gitDir
}

// ── git plumbing ───────────────────────────────────────────────────────────

// env isolates git from system and global config so a stray gpgsign, hooksPath
// or alias in the operator's ~/.gitconfig cannot change snapshot behaviour.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
}

// plain runs git without repository flags (for "init" and "diff --no-index").
func (s *Store) plain(dir []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv()
	if len(dir) > 0 {
		cmd.Dir = dir[0]
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// run executes git against the snapshot repository. stderr is folded into the
// error only, so callers get clean stdout.
func (s *Store) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := append([]string{"--git-dir=" + s.gitDir, "--work-tree=" + s.workTree}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = s.workTree
	cmd.Env = gitEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// ── snapshot scheduling ────────────────────────────────────────────────────

// Nudge records that something under the data dir changed and schedules a
// snapshot. The reason is only used for the commit subject: the snapshot itself
// is "git add -A", so a nudge never has to enumerate what changed.
func (s *Store) Nudge(reason string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.reasons) < maxReasons {
		s.reasons = append(s.reasons, sanitizeReason(reason))
	} else {
		s.reasons[maxReasons-1] = "…"
	}
	if s.firstAt.IsZero() {
		s.firstAt = time.Now()
	}
	if time.Since(s.firstAt) >= s.maxDelay {
		s.fireLocked()
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.debounce, s.fire)
}

// sanitizeReason strips the bytes History() uses as field delimiters, plus
// newlines. Reasons are built from caller-supplied folder and file names, so a
// name containing \x1f could otherwise shift the parsed fields and forge the
// author shown against a revision.
func sanitizeReason(reason string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case 0x00, 0x1f, '\n', '\r':
			return ' '
		}
		return r
	}, reason)
}

// drainLocked consumes the pending reasons and returns a commit subject.
func (s *Store) drainLocked() string {
	if len(s.reasons) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var uniq []string
	for _, r := range s.reasons {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		uniq = append(uniq, r)
	}
	s.reasons = nil
	s.firstAt = time.Time{}
	return "snapshot: " + strings.Join(uniq, ", ")
}

func (s *Store) fire() {
	s.mu.Lock()
	s.timer = nil
	msg := s.drainLocked()
	s.mu.Unlock()
	if msg == "" {
		return
	}
	s.record(msg, s.commit(msg))
}

// record stores and logs a snapshot outcome. A failure used to be dropped
// entirely: the pending reasons had already been cleared, nothing rescheduled
// the attempt, and the only symptom was that no new commits appeared.
func (s *Store) record(msg string, err error) {
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
	if err == nil {
		return
	}
	log.Printf("versioning: snapshot failed (%s): %v", msg, err)
	// Put the work back so the next tick retries instead of losing it.
	s.Nudge("retry after failure")
}

// fireLocked is the max-delay path; the caller already holds s.mu.
func (s *Store) fireLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	msg := s.drainLocked()
	if msg == "" {
		return
	}
	go s.commit(msg)
}

// CommitNow flushes any pending snapshot and commits synchronously. Used by the
// short-lived CLI, and before a restore so the state being overwritten is
// captured first.
func (s *Store) CommitNow(reason string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.reasons = append(s.reasons, sanitizeReason(reason))
	msg := s.drainLocked()
	s.mu.Unlock()
	err := s.commit(msg)
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
	return err
}

// RunPeriodic snapshots the tree on a fixed interval until stop is closed.
//
// Nudge only fires for writes that went through the CLI or an API handler. A
// change made any other way — a file copied into the data dir, a comments
// sidecar edited by hand, a tool dropping output in — would otherwise sit
// uncommitted indefinitely. This makes the guarantee unconditional: whatever is
// on disk is in history within one interval, however it got there.
//
// An idle tree costs nothing: commit() returns early when the index matches
// HEAD, so a sweep over unchanged content produces no commit.
func (s *Store) RunPeriodic(interval time.Duration, stop <-chan struct{}) {
	if s == nil || interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if err := s.CommitNow("periodic sweep"); err != nil {
				log.Printf("versioning: periodic snapshot failed: %v", err)
			}
		}
	}
}

func (s *Store) commit(msg string) error {
	s.commitSeq.Lock()
	defer s.commitSeq.Unlock()

	// Oversized files are excluded per commit rather than by extension, so a
	// file that grows past the limit stops being staged and one that is
	// replaced by something small starts again.
	if err := s.writeSizeExcludes(); err != nil {
		log.Printf("versioning: could not refresh size excludes: %v", err)
	}

	// commitSeq only serializes this process. The coherence-doc CLI snapshots
	// from a separate process against the same index, so a lost race on
	// index.lock is expected rather than exceptional — back off and retry.
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if _, err = s.run("add", "-A", "--", "."); err == nil {
			break
		}
		if !isLockContention(err) {
			return err
		}
		time.Sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
	}
	if err != nil {
		return err
	}
	// "diff --cached --quiet" exits 0 when the index matches HEAD, i.e. there is
	// nothing to snapshot. Against an unborn HEAD it compares to the empty tree,
	// so the first commit is handled too.
	if _, err := s.run("diff", "--cached", "--quiet"); err == nil {
		return nil
	}
	if len(msg) > 200 {
		msg = msg[:197] + "…"
	}
	for attempt := 0; attempt < 5; attempt++ {
		if _, err = s.run("commit", "--no-verify", "-m", msg); err == nil {
			return nil
		}
		if !isLockContention(err) {
			return err
		}
		time.Sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
	}
	return err
}

// writeSizeExcludes regenerates the size-based half of $GIT_DIR/info/exclude.
// The static patterns are rewritten with it so the file always has both halves.
func (s *Store) writeSizeExcludes() error {
	body := strings.Join(excludePatterns, "\n") + "\n"
	if s.maxBlob > 0 {
		var oversized []string
		filepath.Walk(s.workTree, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if strings.HasPrefix(info.Name(), ".") && path != s.workTree {
					return filepath.SkipDir
				}
				return nil
			}
			if info.Size() <= s.maxBlob {
				return nil
			}
			rel, err := filepath.Rel(s.workTree, path)
			if err != nil {
				return nil
			}
			// A leading slash anchors the pattern at the repository root, and
			// the escape keeps a literal "[" or "*" in a filename from being
			// read as a glob.
			oversized = append(oversized, "/"+escapeGlob(filepath.ToSlash(rel)))
			return nil
		})
		if len(oversized) > 0 {
			sort.Strings(oversized)
			body += "\n# Excluded for exceeding the size limit; regenerated every commit.\n"
			body += strings.Join(oversized, "\n") + "\n"
		}
	}
	return os.WriteFile(filepath.Join(s.gitDir, "info", "exclude"), []byte(body), 0600)
}

// escapeGlob quotes the gitignore pattern metacharacters so a path is matched
// literally.
func escapeGlob(p string) string {
	var b strings.Builder
	for _, r := range p {
		switch r {
		case '*', '?', '[', ']', '\\', '!', '#':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isLockContention reports whether an error is another process holding the
// repository index or ref locks, as opposed to a real failure.
func isLockContention(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "index.lock") ||
		strings.Contains(msg, "Unable to create") ||
		strings.Contains(msg, "cannot lock ref") ||
		strings.Contains(msg, "ref_lock")
}

// ── history reads ──────────────────────────────────────────────────────────

// History returns the snapshots that touched relPath, newest first. relPath is
// slash-separated and relative to the data dir. Renames are followed.
func (s *Store) History(relPath string, limit int) ([]Revision, error) {
	if s == nil {
		return nil, fmt.Errorf("versioning disabled")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// --name-only with a pathspec reports the path as it was named in each
	// commit, which is what FileAt needs across renames. \x00 delimits commits
	// and \x1f the metadata fields, neither of which can occur in the values.
	//
	// --diff-filter=d (lowercase: exclude) drops commits that deleted the file.
	// Those carry no blob, so listing them would offer a version that cannot be
	// read or restored. Excluding them also means a deleted document's history
	// still ends at its last real content, which is what makes it recoverable.
	//
	// --follow's rename attribution has to be validated, not trusted — see
	// truncateFalseRenames. Similarity thresholds cannot do it: every generated
	// doc shares the whole HTML template, so two unrelated short docs are ~98%
	// alike.
	out, err := s.run("log", "--follow", "--diff-filter=d", "-n", strconv.Itoa(limit),
		"--format=%x00%H%x1f%at%x1f%s%x1f%an", "--name-only", "--", relPath)
	if err != nil {
		// A document with no snapshot yet is not an error.
		if strings.Contains(err.Error(), "unknown revision") || strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, err
	}

	var revs []Revision
	for _, block := range strings.Split(string(out), "\x00") {
		block = strings.Trim(block, "\n")
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		fields := strings.Split(lines[0], "\x1f")
		if len(fields) < 4 {
			continue
		}
		ts, _ := strconv.ParseInt(fields[1], 10, 64)
		rev := Revision{
			Rev:     fields[0],
			Short:   shortRev(fields[0]),
			Unix:    ts,
			Date:    time.Unix(ts, 0).Format("Jan 02, 2006 15:04"),
			Subject: fields[2],
			Author:  fields[3],
			Path:    relPath,
		}
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l != "" {
				rev.Path = l
				break
			}
		}
		revs = append(revs, rev)
	}
	return revs, nil
}

// FilterByIdentity keeps only the revisions whose blob is the same document as
// uid, and drops the rest.
//
// This replaces guessing. git decides renames by content similarity, which is
// useless for this corpus: every generated doc carries the whole HTML template,
// so two unrelated short docs are ~98% alike and --follow will present a
// brand-new document's first revision as whichever existing doc it resembles.
// A previous attempt discriminated by whether the attributed path still existed
// in HEAD, which closed only half the hole — a path that had been *deleted* was
// accepted, so a deleted document's content surfaced as another document's
// history and restore would write it over the live one. And when a path is
// reused (delete then recreate the same slug) every revision's path matches, so
// there was nothing to discriminate on at all.
//
// Identity is the only sound answer: each generated doc embeds a stable id that
// survives edits, renames and moves. A revision counts as this document's
// history when its blob carries the same id.
//
// uid == "" means the current document predates ids. Nothing can be verified in
// that case, so only revisions at the exact same path are kept — no rename
// following, but never another document's content.
func (s *Store) FilterByIdentity(revs []Revision, uid string, idOf func([]byte) string) []Revision {
	if s == nil || len(revs) == 0 {
		return revs
	}
	out := make([]Revision, 0, len(revs))
	for _, rv := range revs {
		blob, err := s.FileAt(rv.Rev, rv.Path)
		if err != nil {
			continue // unreadable blob: not offerable, so not history
		}
		got := idOf(blob)
		switch {
		case uid != "" && got == uid:
			out = append(out, rv)
		case uid == "" && got == "" && rv.Path == revs[0].Path:
			// Pre-id document: same path only.
			out = append(out, rv)
		}
	}
	return out
}

// FileAt returns the bytes of relPath as of rev.
func (s *Store) FileAt(rev, relPath string) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("versioning disabled")
	}
	if !revRe.MatchString(rev) {
		return nil, fmt.Errorf("invalid revision")
	}
	if relPath == "" || strings.Contains(relPath, "..") {
		return nil, fmt.Errorf("invalid path")
	}
	return s.run("cat-file", "blob", rev+":"+relPath)
}

// Diff returns a unified diff between two labelled contents. It shells out to
// "git diff --no-index" rather than implementing a diff, so the output matches
// what git produces everywhere else.
//
// The inputs are content rather than revisions because what users want to read
// is a diff of the markdown source, not of the rendered HTML the snapshot
// actually stores.
func (s *Store) Diff(fromLabel string, from []byte, toLabel string, to []byte, name string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("versioning disabled")
	}
	tmp, err := os.MkdirTemp("", "coherence-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	fromLabel = safeLabel(fromLabel)
	toLabel = safeLabel(toLabel)
	if fromLabel == toLabel {
		toLabel += "-b"
	}
	name = safeLabel(name)
	if name == "" {
		name = "doc.md"
	}

	write := func(label string, content []byte) (string, error) {
		dir := filepath.Join(tmp, label)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, content, 0600); err != nil {
			return "", err
		}
		return filepath.Join(label, name), nil
	}
	a, err := write(fromLabel, from)
	if err != nil {
		return "", err
	}
	b, err := write(toLabel, to)
	if err != nil {
		return "", err
	}

	// --no-index exits 1 when the files differ, which is the normal case here.
	out, err := s.plain([]string{tmp}, "diff", "--no-index", "--no-prefix", "--unified=3", "--", a, b)
	if err != nil && len(out) == 0 {
		return "", err
	}
	return string(out), nil
}

func shortRev(rev string) string {
	r := shortRe.ReplaceAllString(strings.ToLower(rev), "")
	if len(r) > 8 {
		return r[:8]
	}
	return r
}

// safeLabel reduces a caller-supplied label to something safe to use as a path
// component in the temp directory handed to git diff.
func safeLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return -1
		}
	}, s)
	s = strings.TrimLeft(s, ".")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
