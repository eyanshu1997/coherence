package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var skipPrefixes = []string{
	"echo", "printf", "cat", "ls", "pwd", "cd", "true", "false",
	"sleep", "exit", "source", "export", "cp ", "rm -f", "mkdir",
	"chmod", "date", "which", "type", "hash",
}

var stripRe = regexp.MustCompile(
	`'[^']*'|"[^"]*"` + // quoted strings
		`|/[\w/.\-]+` + // absolute paths
		`|\b[0-9a-f]{7,40}\b` + // git hashes
		`|\b\d+\b` + // numbers
		`|[\w][-\w]*\.[-\w]+\.\S+` + // branch names (user.ticket format)
		`|[A-Z]+-\d+` + // Jira IDs
		`|PR-\d+`, // PR refs
)

var spaceRe = regexp.MustCompile(`\s+`)

func normalize(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if nl := strings.IndexByte(cmd, '\n'); nl != -1 {
		cmd = cmd[:nl]
	}
	cmd = stripRe.ReplaceAllString(cmd, "…")
	cmd = spaceRe.ReplaceAllString(cmd, " ")
	cmd = strings.TrimSpace(cmd)
	if len(cmd) > 120 {
		cmd = cmd[:120]
	}
	return cmd
}

func shouldSkip(cmd string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	for _, p := range skipPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func main() {
	var payload map[string]any
	if err := json.NewDecoder(os.Stdin).Decode(&payload); err != nil {
		return
	}
	if payload["tool_name"] != "Bash" {
		return
	}
	input, _ := payload["tool_input"].(map[string]any)
	cmd, _ := input["command"].(string)
	if cmd == "" || shouldSkip(cmd) {
		return
	}
	pattern := normalize(cmd)
	if pattern == "" || len(pattern) < 8 {
		return
	}

	sessionID := os.Getenv("CLAUDE_SESSION_ID")
	if sessionID == "" {
		sessionID = "unknown"
	}
	cwd, _ := os.Getwd()
	if v := os.Getenv("PWD"); v != "" {
		cwd = v
	}

	entry := map[string]any{
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
		"pattern":    pattern,
		"cmd":        cmd[:min(len(cmd), 300)],
		"session_id": sessionID,
		"cwd":        cwd,
	}

	logPath := filepath.Join(os.Getenv("HOME"), ".claude", "bash-commands.jsonl")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(entry)
	f.Write(line)
	f.WriteString("\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
