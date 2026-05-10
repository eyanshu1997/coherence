package server

import (
	"path/filepath"
	"regexp"
	"strings"
)

var allowedPartRe = regexp.MustCompile(`[^a-zA-Z0-9_\-.]`)
var allowedFileRe = regexp.MustCompile(`[^a-zA-Z0-9_\-.]`)

// safeFolderPath validates a folder string and returns an absolute path under dataDir.
// Returns "" if the folder is invalid or would escape dataDir.
func safeFolderPath(dataDir, folder string) string {
	parts := strings.Split(strings.Trim(folder, "/"), "/")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		c := allowedPartRe.ReplaceAllString(p, "")
		if c == "" {
			return ""
		}
		clean = append(clean, c)
	}
	joined := filepath.Join(append([]string{dataDir}, clean...)...)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return ""
	}
	dataDirAbs, err := filepath.Abs(dataDir)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(abs, dataDirAbs+string(filepath.Separator)) && abs != dataDirAbs {
		return ""
	}
	return abs
}

// safeCommentPath validates folder+file and returns the path to the .comments.json file.
// Returns "" if inputs are invalid or would escape dataDir.
func safeCommentPath(dataDir, folder, file string) string {
	folderPath := safeFolderPath(dataDir, folder)
	if folderPath == "" {
		return ""
	}
	fileClean := allowedFileRe.ReplaceAllString(strings.TrimSuffix(file, ".html"), "")
	if fileClean == "" {
		return ""
	}
	result := filepath.Join(folderPath, fileClean+".comments.json")
	dataDirAbs, err := filepath.Abs(dataDir)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(result, dataDirAbs+string(filepath.Separator)) {
		return ""
	}
	return result
}

// safeDocPath validates folder+filename and returns (folderAbsPath, fileAbsPath).
// filename is sanitized; .html is appended if missing.
// Returns ("", "") if invalid.
func safeDocPath(dataDir, folder, filename string) (string, string) {
	fp := safeFolderPath(dataDir, folder)
	if fp == "" {
		return "", ""
	}
	fname := allowedFileRe.ReplaceAllString(filename, "")
	if !strings.HasSuffix(fname, ".html") {
		fname += ".html"
	}
	dest := filepath.Join(fp, fname)
	dataDirAbs, err := filepath.Abs(dataDir)
	if err != nil {
		return "", ""
	}
	if !strings.HasPrefix(dest, dataDirAbs+string(filepath.Separator)) {
		return "", ""
	}
	return fp, dest
}
