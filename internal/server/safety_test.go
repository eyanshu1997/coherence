package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeFolderPath(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name   string
		folder string
		wantOK bool
	}{
		{"simple", "repo-a/PROJ-123", true},
		{"root folder", "ai-system", true},
		{"dots allowed", "perf-analysis.v2", true},
		{"empty", "", false},
		{"traversal", "../etc", false},
		{"traversal nested", "a/../../etc/passwd", false},
		{"slash only", "/", false},
		{"spaces stripped to valid", "a b c", true}, // spaces stripped → "abc"
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeFolderPath(dir, tc.folder)
			if tc.wantOK && got == "" {
				t.Errorf("safeFolderPath(%q) = empty, want non-empty", tc.folder)
			}
			if !tc.wantOK && got != "" {
				t.Errorf("safeFolderPath(%q) = %q, want empty", tc.folder, got)
			}
			if got != "" {
				// Must be under dataDir
				dataDirAbs, _ := filepath.Abs(dir)
				if !filepath.HasPrefix(got, dataDirAbs) && got != dataDirAbs {
					t.Errorf("safeFolderPath result %q not under dataDir %q", got, dataDirAbs)
				}
			}
		})
	}
}

// filepath.HasPrefix is not available on all platforms; use strings.HasPrefix instead
func init() {
	_ = os.MkdirAll
	_ = filepath.Join
}

func TestSafeCommentPath(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		folder string
		file   string
		wantOK bool
	}{
		{"repo-a/PROJ-123", "plan", true},
		{"repo-a/PROJ-123", "plan.html", true},
		{"repo-a/PROJ-123", "", false},
		{"", "plan", false},
		{"../escape", "plan", false},
	}
	for _, tc := range tests {
		got := safeCommentPath(dir, tc.folder, tc.file)
		if tc.wantOK && got == "" {
			t.Errorf("safeCommentPath(%q,%q) = empty, want non-empty", tc.folder, tc.file)
		}
		if !tc.wantOK && got != "" {
			t.Errorf("safeCommentPath(%q,%q) = %q, want empty", tc.folder, tc.file, got)
		}
		if got != "" && !filepath.IsAbs(got) {
			t.Errorf("safeCommentPath returned non-absolute path: %q", got)
		}
	}
}
