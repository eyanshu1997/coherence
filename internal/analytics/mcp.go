package analytics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type MCPEntry struct {
	TS      string         `json:"ts"`
	Tool    string         `json:"tool"`
	Input   map[string]any `json:"input"`
	Success bool           `json:"success"`
	Error   string         `json:"error"`
}

type ToolStats struct {
	Calls      int
	Failures   int
	Errors     map[string]int
	FailInputs []map[string]any
}

func LoadMCPEntries(logPath string, sinceDays int) []MCPEntry {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	cutoff := time.Now().UTC().Add(-time.Duration(sinceDays) * 24 * time.Hour)
	var out []MCPEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var e MCPEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, e.TS)
		if err != nil {
			ts, _ = time.Parse(time.RFC3339, e.TS)
		}
		if ts.Before(cutoff) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func AnalyzeMCP(entries []MCPEntry, minCalls int) map[string]*ToolStats {
	stats := map[string]*ToolStats{}
	for _, e := range entries {
		tool := e.Tool
		if tool == "" {
			tool = "unknown"
		}
		s, ok := stats[tool]
		if !ok {
			s = &ToolStats{Errors: map[string]int{}}
			stats[tool] = s
		}
		s.Calls++
		if !e.Success {
			s.Failures++
			key := e.Error
			if len(key) > 100 {
				key = key[:100]
			}
			s.Errors[key]++
			if e.Input != nil {
				s.FailInputs = append(s.FailInputs, e.Input)
			}
		}
	}
	// Filter by minCalls
	for k, v := range stats {
		if v.Calls < minCalls {
			delete(stats, k)
		}
	}
	return stats
}

func FindInputPatterns(failInputs []map[string]any) []string {
	if len(failInputs) == 0 {
		return nil
	}
	allKeys := map[string]bool{}
	for _, inp := range failInputs {
		for k := range inp {
			allKeys[k] = true
		}
	}
	n := len(failInputs)
	emptyCounts := map[string]int{}
	for _, inp := range failInputs {
		for k := range allKeys {
			v := inp[k]
			empty := v == nil || v == "" || v == "0"
			if empty {
				emptyCounts[k]++
			}
		}
	}
	var patterns []string
	for k, cnt := range emptyCounts {
		if float64(cnt)/float64(n) >= 0.6 {
			patterns = append(patterns, "`"+k+"`"+" empty/missing in "+itoa(cnt)+"/"+itoa(n)+" failures")
		}
	}
	return patterns
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
