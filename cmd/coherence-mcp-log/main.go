package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func isError(response any) (bool, string) {
	if response == nil {
		return false, ""
	}

	// try to parse string as JSON
	if s, ok := response.(string); ok {
		var parsed map[string]any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			response = parsed
		} else {
			// plain string — only flag if starts with "Error:"
			trimmed := strings.TrimSpace(s)
			if strings.HasPrefix(trimmed, "Error:") || strings.HasPrefix(trimmed, "error:") {
				msg := trimmed
				if len(msg) > 300 {
					msg = msg[:300]
				}
				return true, msg
			}
			return false, ""
		}
	}

	if m, ok := response.(map[string]any); ok {
		// check error/Error field
		for _, key := range []string{"error", "Error"} {
			if errVal, exists := m[key]; exists {
				if errStr, ok := errVal.(string); ok && errStr != "" {
					// check for wrapped JSON false-positive
					var inner map[string]any
					if json.Unmarshal([]byte(errStr), &inner) == nil {
						if result, ok := inner["result"].(string); ok {
							if strings.TrimSpace(result) != "" && !strings.HasPrefix(strings.TrimSpace(result), "Error:") {
								return false, ""
							}
						}
					}
					msg := errStr
					if len(msg) > 300 {
						msg = msg[:300]
					}
					return true, msg
				}
			}
		}
		// check result field
		if result, ok := m["result"].(string); ok {
			trimmed := strings.TrimSpace(result)
			if strings.HasPrefix(trimmed, "Error:") {
				if len(trimmed) > 300 {
					trimmed = trimmed[:300]
				}
				return true, trimmed
			}
		}
	}
	return false, ""
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func truncateDict(m map[string]any, max int) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = truncate(toString(v), max)
	}
	return out
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func main() {
	var payload map[string]any
	if err := json.NewDecoder(os.Stdin).Decode(&payload); err != nil {
		return
	}
	toolName, _ := payload["tool_name"].(string)
	if !strings.HasPrefix(toolName, "mcp__") {
		return
	}

	toolInput := payload["tool_input"]
	toolResponse := payload["tool_response"]

	errOk, errMsg := isError(toolResponse)

	sessionID := os.Getenv("CLAUDE_SESSION_ID")
	if sessionID == "" {
		sessionID = "unknown"
	}
	cwd, _ := os.Getwd()
	if v := os.Getenv("PWD"); v != "" {
		cwd = v
	}

	// strip mcp__<server>__ prefix
	parts := strings.SplitN(toolName, "__", 3)
	shortTool := toolName
	if len(parts) == 3 {
		shortTool = parts[2]
	}

	var inputField any
	if m, ok := toolInput.(map[string]any); ok {
		inputField = truncateDict(m, 500)
	} else {
		inputField = truncate(toString(toolInput), 500)
	}

	entry := map[string]any{
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
		"tool":       shortTool,
		"input":      inputField,
		"success":    !errOk,
		"error":      errMsg,
		"session_id": sessionID,
		"cwd":        cwd,
	}

	home := os.Getenv("HOME")
	toolLog := filepath.Join(home, ".claude", "tool-uses.jsonl")
	failLog := filepath.Join(home, ".claude", "tool-failures.jsonl")
	os.MkdirAll(filepath.Dir(toolLog), 0755)

	writeJSONL(toolLog, entry)

	if errOk {
		failEntry := map[string]any{
			"ts":         entry["ts"],
			"tool":       toolName,
			"short_tool": shortTool,
			"input":      toolInput,
			"error":      errMsg,
			"session_id": sessionID,
			"cwd":        cwd,
		}
		writeJSONL(failLog, failEntry)
	}
}

func writeJSONL(path string, v any) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(v)
	f.Write(line)
	f.WriteString("\n")
}
