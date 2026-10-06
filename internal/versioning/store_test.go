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

func TestExcludedPathsAreNotSnapshotted(t *testing.T) {
	s, work := newStore(t)
	// index.html is regenerated on every write, and logs are large and
	// reproducible; neither belongs in history.
	writeDoc(t, work, "proj/index.html", "<html>index</html>")
	writeDoc(t, work, "proj/logs/huge.log", "noise")
	writeDoc(t, work, "proj/doc.html", "v1")
	if err := s.CommitNow("first"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"proj/index.html", "proj/logs/huge.log"} {
		revs, err := s.History(p, 10)
		if err != nil {
			t.Fatalf("History(%s): %v", p, err)
		}
		if len(revs) != 0 {
			t.Errorf("%s should be excluded from snapshots, got %d revisions", p, len(revs))
		}
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
