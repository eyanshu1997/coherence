package versioning

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newStore builds a store whose repository lives outside the work tree, the way
// the server does.
func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/git"); err != nil {
		if _, err2 := os.Stat("/bin/git"); err2 != nil {
			t.Skip("git not available")
		}
	}
	work := t.TempDir()
	gitDir := filepath.Join(t.TempDir(), "versions.git")
	s, err := New(gitDir, work, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, work
}

func writeDoc(t *testing.T, work, rel, content string) {
	t.Helper()
	p := filepath.Join(work, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsRepoInsideWorkTree(t *testing.T) {
	work := t.TempDir()
	// A repository under the served data dir would be reachable over HTTP.
	if _, err := New(filepath.Join(work, "versions.git"), work, time.Second); err == nil {
		t.Fatal("expected New to refuse a git dir inside the work tree")
	}
}

func TestSnapshotAndHistory(t *testing.T) {
	s, work := newStore(t)

	writeDoc(t, work, "proj/doc.html", "v1")
	if err := s.CommitNow("first"); err != nil {
		t.Fatalf("CommitNow: %v", err)
	}
	writeDoc(t, work, "proj/doc.html", "v2")
	if err := s.CommitNow("second"); err != nil {
		t.Fatalf("CommitNow: %v", err)
	}

	revs, err := s.History("proj/doc.html", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(revs))
	}
	if !strings.Contains(revs[0].Subject, "second") {
		t.Errorf("newest revision should be first, got subject %q", revs[0].Subject)
	}
	if revs[0].Unix == 0 || revs[0].Short == "" {
		t.Errorf("revision metadata not populated: %+v", revs[0])
	}

	// The older revision must still yield the old bytes.
	old, err := s.FileAt(revs[1].Rev, revs[1].Path)
	if err != nil {
		t.Fatalf("FileAt: %v", err)
	}
	if string(old) != "v1" {
		t.Errorf("expected old content %q, got %q", "v1", string(old))
	}
}

func TestNoCommitWhenNothingChanged(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/doc.html", "v1")
	if err := s.CommitNow("first"); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitNow("second with no change"); err != nil {
		t.Fatal(err)
	}
	revs, err := s.History("proj/doc.html", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Fatalf("an unchanged tree must not produce a commit; got %d revisions", len(revs))
	}
}

func TestHistoryEmptyForUnknownDoc(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/doc.html", "v1")
	if err := s.CommitNow("first"); err != nil {
		t.Fatal(err)
	}
	revs, err := s.History("proj/other.html", 10)
	if err != nil {
		t.Fatalf("History on an untracked doc should not error: %v", err)
	}
	if len(revs) != 0 {
		t.Fatalf("expected no revisions, got %d", len(revs))
	}
}

func TestHistoryOnEmptyRepo(t *testing.T) {
	s, _ := newStore(t)
	revs, err := s.History("proj/doc.html", 10)
	if err != nil {
		t.Fatalf("History before the first commit should not error: %v", err)
	}
	if len(revs) != 0 {
		t.Fatalf("expected no revisions, got %d", len(revs))
	}
}

func TestDerivedFilesAreNotSnapshotted(t *testing.T) {
	s, work := newStore(t)
	// index.html is regenerated on every write, so tracking it would turn each
	// edit into a tree-wide diff. A log, by contrast, is a viewable document
	// and is versioned like any other — it is excluded only if oversized
	// (see TestOversizedFilesExcludedBySize).
	writeDoc(t, work, "proj/index.html", "<html>index</html>")
	writeDoc(t, work, "proj/logs/modest.log", "a log small enough to keep")
	writeDoc(t, work, "proj/doc.html", "v1")
	if err := s.CommitNow("first"); err != nil {
		t.Fatal(err)
	}

	if revs, err := s.History("proj/index.html", 10); err != nil || len(revs) != 0 {
		t.Errorf("index.html should be excluded, got %d revisions (err %v)", len(revs), err)
	}
	if revs, err := s.History("proj/logs/modest.log", 10); err != nil || len(revs) != 1 {
		t.Errorf("a modest log should be versioned, got %d revisions (err %v)", len(revs), err)
	}
	if revs, err := s.History("proj/doc.html", 10); err != nil || len(revs) != 1 {
		t.Errorf("the doc should be versioned, got %d revisions (err %v)", len(revs), err)
	}
}

func TestHistoryFollowsRename(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/old.html", "shared content that survives the rename\n")
	if err := s.CommitNow("create"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(work, "proj/old.html"), filepath.Join(work, "proj/new.html")); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitNow("rename"); err != nil {
		t.Fatal(err)
	}

	revs, err := s.History("proj/new.html", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("expected history to follow the rename, got %d revisions", len(revs))
	}
	// The pre-rename revision must report the old path, since "rev:new.html"
	// does not resolve in that commit.
	if revs[1].Path != "proj/old.html" {
		t.Errorf("expected pre-rename path proj/old.html, got %q", revs[1].Path)
	}
	if _, err := s.FileAt(revs[1].Rev, revs[1].Path); err != nil {
		t.Errorf("FileAt on the pre-rename path failed: %v", err)
	}
}

func TestDiff(t *testing.T) {
	s, _ := newStore(t)
	out, err := s.Diff("abc1234", []byte("line one\nline two\n"), "current", []byte("line one\nline TWO\n"), "doc.md")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(out, "-line two") || !strings.Contains(out, "+line TWO") {
		t.Errorf("diff did not contain the changed lines:\n%s", out)
	}
}

func TestDiffIdentical(t *testing.T) {
	s, _ := newStore(t)
	out, err := s.Diff("abc1234", []byte("same\n"), "current", []byte("same\n"), "doc.md")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected an empty diff, got:\n%s", out)
	}
}

func TestFileAtRejectsBadRevision(t *testing.T) {
	s, _ := newStore(t)
	for _, bad := range []string{"", "HEAD", "../etc/passwd", "abc; rm -rf /", "main"} {
		if _, err := s.FileAt(bad, "proj/doc.html"); err == nil {
			t.Errorf("FileAt accepted an invalid revision %q", bad)
		}
	}
}

func TestNudgeDebouncesIntoOneCommit(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/doc.html", "v1")
	// Three writes inside one debounce window must collapse to one snapshot.
	s.Nudge("update a")
	writeDoc(t, work, "proj/doc.html", "v2")
	s.Nudge("update b")
	writeDoc(t, work, "proj/doc.html", "v3")
	s.Nudge("update c")

	deadline := time.Now().Add(5 * time.Second)
	var revs []Revision
	for time.Now().Before(deadline) {
		var err error
		revs, err = s.History("proj/doc.html", 10)
		if err == nil && len(revs) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(revs) != 1 {
		t.Fatalf("expected one coalesced snapshot, got %d", len(revs))
	}
	if !strings.Contains(revs[0].Subject, "update a") || !strings.Contains(revs[0].Subject, "update c") {
		t.Errorf("coalesced subject should mention each reason, got %q", revs[0].Subject)
	}
	content, err := s.FileAt(revs[0].Rev, revs[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "v3" {
		t.Errorf("snapshot should hold the latest content, got %q", content)
	}
}

func TestNilStoreIsInert(t *testing.T) {
	var s *Store
	if s.Enabled() {
		t.Error("a nil store must report disabled")
	}
	s.Nudge("whatever") // must not panic
	if err := s.CommitNow("whatever"); err != nil {
		t.Errorf("CommitNow on a nil store should be a no-op, got %v", err)
	}
	if _, err := s.History("x", 1); err == nil {
		t.Error("History on a nil store should error")
	}
}

// A deleted document stays recoverable: the commit that removed it carries no
// blob, so history must end at its last real content instead of offering a
// revision that cannot be read or restored.
func TestHistoryExcludesDeletionCommits(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/doomed.html", "the content that must survive deletion\n")
	if err := s.CommitNow("create"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(work, "proj/doomed.html")); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitNow("delete"); err != nil {
		t.Fatal(err)
	}

	revs, err := s.History("proj/doomed.html", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("expected only the content revision, got %d: %+v", len(revs), revs)
	}
	// The newest listed revision must be readable, which is the whole point.
	got, err := s.FileAt(revs[0].Rev, revs[0].Path)
	if err != nil {
		t.Fatalf("the newest listed revision of a deleted doc must be readable: %v", err)
	}
	if !strings.Contains(string(got), "must survive") {
		t.Errorf("unexpected recovered content: %q", got)
	}
}

// A change that never nudges — a file written straight into the tree — must
// still reach history via the periodic sweep.
func TestRunPeriodicSnapshotsUnnudgedChanges(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/doc.html", "v1")
	if err := s.CommitNow("seed"); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	defer close(stop)
	go s.RunPeriodic(60*time.Millisecond, stop)

	// Written directly, with no Nudge at all.
	writeDoc(t, work, "proj/smuggled.html", "arrived without the API")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		revs, err := s.History("proj/smuggled.html", 5)
		if err == nil && len(revs) > 0 {
			return // swept up as intended
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("periodic sweep never committed a file written outside the API")
}

// The sweep must not manufacture empty commits on an idle tree.
func TestRunPeriodicIdleMakesNoCommits(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/doc.html", "v1")
	if err := s.CommitNow("seed"); err != nil {
		t.Fatal(err)
	}
	before, err := s.run("rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	go s.RunPeriodic(40*time.Millisecond, stop)
	time.Sleep(400 * time.Millisecond)
	close(stop)

	after, err := s.run("rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(before)) != strings.TrimSpace(string(after)) {
		t.Errorf("idle sweeps created commits: %s -> %s",
			strings.TrimSpace(string(before)), strings.TrimSpace(string(after)))
	}
}

func TestRunPeriodicZeroIntervalIsNoop(t *testing.T) {
	s, _ := newStore(t)
	done := make(chan struct{})
	go func() { s.RunPeriodic(0, nil); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("RunPeriodic with a zero interval should return immediately")
	}
}

// idOf is a stand-in for docgen.ExtractUID: it reads an "id:<value>" marker.
func idOf(blob []byte) string {
	for _, line := range strings.Split(string(blob), "\n") {
		if rest, ok := cutPrefix(line, "id:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func cutPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return "", false
}

// --follow attributes a new file to whichever existing file it resembles, and
// for generated docs that resemblance is near-total: they share the whole HTML
// template. History must never offer another document's revision, since restore
// would write it over the real one.
func TestFilterByIdentityRejectsFalseRenameFromLiveDoc(t *testing.T) {
	s, work := newStore(t)
	boiler := strings.Repeat("<!-- shared template line -->\n", 200)
	writeDoc(t, work, "proj/original.html", boiler+"id:AAAA\n<p>the original body</p>\n")
	if err := s.CommitNow("create original"); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, work, "proj/newcomer.html", boiler+"id:BBBB\n<p>a different body</p>\n")
	if err := s.CommitNow("create newcomer"); err != nil {
		t.Fatal(err)
	}

	revs, err := s.History("proj/newcomer.html", 10)
	if err != nil {
		t.Fatal(err)
	}
	// git itself conflates them: that is the whole problem.
	if len(revs) < 2 {
		t.Skip("rename detection did not fire; nothing to filter")
	}
	kept := s.FilterByIdentity(revs, "BBBB", idOf)
	if len(kept) != 1 {
		t.Fatalf("expected only the newcomer's own revision, got %d: %+v", len(kept), kept)
	}
	blob, err := s.FileAt(kept[0].Rev, kept[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), "a different body") {
		t.Errorf("kept another document's content: %q", blob)
	}
}

// The half the previous heuristic missed: it accepted an attributed path that
// had been DELETED, so a deleted document's content surfaced as another
// document's history and restore would write it over the live one.
func TestFilterByIdentityRejectsFalseRenameFromDeletedDoc(t *testing.T) {
	s, work := newStore(t)
	boiler := strings.Repeat("<!-- shared template line -->\n", 200)
	writeDoc(t, work, "secret/secret.html", boiler+"id:SECRET\n<p>acquisition price is 4.2B</p>\n")
	if err := s.CommitNow("create secret"); err != nil {
		t.Fatal(err)
	}
	// Delete and create in one commit, which is exactly what the debounce produces.
	if err := os.Remove(filepath.Join(work, "secret/secret.html")); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, work, "public/notes.html", boiler+"id:PUBLIC\n<p>nothing to see here</p>\n")
	if err := s.CommitNow("delete secret, create notes"); err != nil {
		t.Fatal(err)
	}

	revs, err := s.History("public/notes.html", 10)
	if err != nil {
		t.Fatal(err)
	}
	kept := s.FilterByIdentity(revs, "PUBLIC", idOf)
	for _, rv := range kept {
		blob, err := s.FileAt(rv.Rev, rv.Path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(blob), "acquisition price") {
			t.Fatalf("a deleted document's content leaked into another doc's history via %s", rv.Path)
		}
	}
	if len(kept) != 1 {
		t.Errorf("expected notes' own single revision, got %d", len(kept))
	}
}

// Identity follows a genuine rename, which is what path-based history cannot do.
func TestFilterByIdentityFollowsGenuineRename(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/alpha.html", "id:SAME\n<p>v1</p>\n")
	if err := s.CommitNow("create alpha"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(work, "proj/alpha.html"), filepath.Join(work, "proj/beta.html")); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitNow("rename to beta"); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, work, "proj/beta.html", "id:SAME\n<p>v2</p>\n")
	if err := s.CommitNow("edit beta"); err != nil {
		t.Fatal(err)
	}

	revs, err := s.History("proj/beta.html", 10)
	if err != nil {
		t.Fatal(err)
	}
	kept := s.FilterByIdentity(revs, "SAME", idOf)
	if len(kept) < 2 {
		t.Fatalf("a genuine rename should still be followed, got %d revisions", len(kept))
	}
}

// When a path is reused, every revision's path matches, so there is nothing for
// a path-based rule to discriminate on at all — identity still separates them.
func TestFilterByIdentityRejectsReoccupiedPath(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/slug.html", "id:FIRST\n<p>the first document</p>\n")
	if err := s.CommitNow("create first"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(work, "proj/slug.html")); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, work, "proj/slug.html", "id:SECOND\n<p>a new document at the same slug</p>\n")
	if err := s.CommitNow("recreate at the same slug"); err != nil {
		t.Fatal(err)
	}

	revs, err := s.History("proj/slug.html", 10)
	if err != nil {
		t.Fatal(err)
	}
	kept := s.FilterByIdentity(revs, "SECOND", idOf)
	for _, rv := range kept {
		blob, err := s.FileAt(rv.Rev, rv.Path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(blob), "the first document") {
			t.Fatal("the previous occupant of the path leaked into this document's history")
		}
	}
	if len(kept) != 1 {
		t.Errorf("expected one revision, got %d", len(kept))
	}
}

// A document with no id predates the mechanism: nothing can be verified, so
// only exact-path revisions are kept rather than guessing.
func TestFilterByIdentityPreIdDocKeepsOnlySamePath(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/legacy.html", "<p>no id marker here</p>\n")
	if err := s.CommitNow("create legacy"); err != nil {
		t.Fatal(err)
	}
	revs, err := s.History("proj/legacy.html", 10)
	if err != nil {
		t.Fatal(err)
	}
	kept := s.FilterByIdentity(revs, "", idOf)
	if len(kept) != 1 {
		t.Fatalf("expected the one same-path revision, got %d", len(kept))
	}
	if kept[0].Path != "proj/legacy.html" {
		t.Errorf("path %q", kept[0].Path)
	}
}

// Size, not extension, decides what is too big to snapshot. An uploaded .log is
// a first-class viewable document and must be recoverable; a 66MB one must not
// enter history.
func TestOversizedFilesExcludedBySize(t *testing.T) {
	s, work := newStore(t)
	s.SetMaxBlobBytes(1024)

	writeDoc(t, work, "proj/small.log", strings.Repeat("a", 100))
	writeDoc(t, work, "proj/huge.log", strings.Repeat("b", 4096))
	writeDoc(t, work, "proj/doc.html", "content")
	if err := s.CommitNow("uploads"); err != nil {
		t.Fatal(err)
	}

	if revs, err := s.History("proj/small.log", 5); err != nil || len(revs) != 1 {
		t.Errorf("a small log should be versioned, got %d revisions (err %v)", len(revs), err)
	}
	if revs, err := s.History("proj/huge.log", 5); err != nil || len(revs) != 0 {
		t.Errorf("an oversized log should be excluded, got %d revisions (err %v)", len(revs), err)
	}
}

// A filename containing glob metacharacters must be excluded literally rather
// than as a pattern that could match other documents.
func TestOversizedExcludeEscapesGlobs(t *testing.T) {
	s, work := newStore(t)
	s.SetMaxBlobBytes(1024)
	writeDoc(t, work, "proj/a[1].log", strings.Repeat("b", 4096))
	writeDoc(t, work, "proj/a1.html", "a normal doc that must survive")
	if err := s.CommitNow("mixed"); err != nil {
		t.Fatal(err)
	}
	if revs, err := s.History("proj/a1.html", 5); err != nil || len(revs) != 1 {
		t.Errorf("the glob-escaped exclusion swallowed an unrelated doc: %d revisions (err %v)", len(revs), err)
	}
}

// Delimiter bytes in a caller-supplied name must not be able to shift the
// parsed fields and forge the author shown against a revision.
func TestReasonDelimitersAreStripped(t *testing.T) {
	s, work := newStore(t)
	writeDoc(t, work, "proj/doc.html", "v1")
	if err := s.CommitNow("create proj/A\x1fadmin@corp.example\x1fX"); err != nil {
		t.Fatal(err)
	}
	revs, err := s.History("proj/doc.html", 5)
	if err != nil || len(revs) != 1 {
		t.Fatalf("got %d revisions (err %v)", len(revs), err)
	}
	if revs[0].Author != "coherence" {
		t.Errorf("author was forged through the reason field: %q", revs[0].Author)
	}
	if strings.Contains(revs[0].Subject, "\x1f") {
		t.Errorf("delimiter survived into the subject: %q", revs[0].Subject)
	}
}
