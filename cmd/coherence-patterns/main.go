package main

import (
	"coherence/internal/analytics"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	sinceDays := flag.Int("since-days", 30, "Days of history to analyze")
	minCalls := flag.Int("min-calls", 1, "Minimum calls to include a tool")
	noSave := flag.Bool("no-save", false, "Print to stdout only, do not save")
	flag.Parse()

	home, _ := os.UserHomeDir()
	toolLog := filepath.Join(home, ".claude", "tool-uses.jsonl")
	bashLog := filepath.Join(home, ".claude", "bash-commands.jsonl")
	patternsOut := filepath.Join(home, ".claude", "tool-patterns.md")

	entries := analytics.LoadMCPEntries(toolLog, *sinceDays)
	bashEntries := analytics.LoadBashEntries(bashLog, *sinceDays)

	if len(entries) == 0 && len(bashEntries) == 0 {
		fmt.Printf("No tool use data found (last %d days).\n", *sinceDays)
		fmt.Println("Data accumulates automatically via PostToolUse hooks as you use tools.")
		os.Exit(0)
	}

	stats := analytics.AnalyzeMCP(entries, *minCalls)
	bashCandidates := analytics.AnalyzeBash(bashEntries)

	total := len(entries)
	failures := 0
	for _, e := range entries {
		if !e.Success {
			failures++
		}
	}

	report := analytics.RenderReport(stats, *sinceDays, total, failures)
	bashSection := analytics.RenderBashSection(bashCandidates)
	if bashSection != "" {
		report = report + "\n" + bashSection
	}

	fmt.Printf("MCP calls: %d (%d failures) | Bash patterns tracked: %d repeat candidate(s)\n",
		total, failures, len(bashCandidates))

	if *noSave {
		fmt.Println()
		fmt.Println(report)
		return
	}

	if err := os.WriteFile(patternsOut, []byte(report), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "Error writing report:", err)
		os.Exit(1)
	}
	fmt.Println("Report saved to", patternsOut)

	// Print high-failure and bash sections to console
	inSection := false
	for _, line := range splitLines(report) {
		if startsWith(line, "## High Failure Rate") || startsWith(line, "## Repeated Bash") {
			inSection = true
		} else if startsWith(line, "## ") && inSection &&
			!startsWith(line, "## High") && !startsWith(line, "## Repeated") {
			inSection = false
		}
		if inSection {
			fmt.Println(line)
		}
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
