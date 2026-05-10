package analytics

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func RenderReport(stats map[string]*ToolStats, sinceDays, total, failures int) string {
	var lines []string
	lines = append(lines, "# MCP Tool Use Patterns", "")
	now := time.Now().UTC().Format("2006-01-02 15:04")
	lines = append(lines, fmt.Sprintf("_Generated %s UTC — last %d days_", now, sinceDays))
	if total > 0 {
		lines = append(lines, fmt.Sprintf("_Total calls: %d | Failures: %d | Overall failure rate: %.1f%%_",
			total, failures, float64(failures)/float64(total)*100))
	} else {
		lines = append(lines, "_No data_")
	}
	lines = append(lines, "")

	type toolItem struct {
		name string
		s    *ToolStats
		rate float64
	}
	var items []toolItem
	for name, s := range stats {
		rate := 0.0
		if s.Calls > 0 {
			rate = float64(s.Failures) / float64(s.Calls)
		}
		items = append(items, toolItem{name, s, rate})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].rate != items[j].rate {
			return items[i].rate > items[j].rate
		}
		return items[i].s.Calls > items[j].s.Calls
	})

	// High failure rate section
	var highFail []toolItem
	for _, it := range items {
		if it.rate >= 0.3 {
			highFail = append(highFail, it)
		}
	}
	if len(highFail) > 0 {
		lines = append(lines, "## High Failure Rate (≥30%)", "")
		lines = append(lines, "| Tool | Calls | Failures | Rate | Top Error |")
		lines = append(lines, "|------|-------|----------|------|-----------|")
		for _, it := range highFail {
			topErr := topError(it.s.Errors)
			lines = append(lines, fmt.Sprintf("| `%s` | %d | %d | %.0f%% | %s |",
				it.name, it.s.Calls, it.s.Failures, it.rate*100, topErr))
		}
		lines = append(lines, "")
		lines = append(lines, "### Input Patterns in Failures", "")
		foundAny := false
		for _, it := range highFail {
			patterns := FindInputPatterns(it.s.FailInputs)
			if len(patterns) > 0 {
				foundAny = true
				lines = append(lines, "**`"+it.name+"`**:")
				for _, p := range patterns {
					lines = append(lines, "  - "+p)
				}
			}
		}
		if !foundAny {
			lines = append(lines, "_No consistent input patterns detected._")
		}
		lines = append(lines, "")
	}

	// All tool usage table
	lines = append(lines, "## All Tool Usage", "")
	lines = append(lines, "| Tool | Calls | Failures | Rate |")
	lines = append(lines, "|------|-------|----------|------|")
	for _, it := range items {
		lines = append(lines, fmt.Sprintf("| `%s` | %d | %d | %.0f%% |",
			it.name, it.s.Calls, it.s.Failures, it.rate*100))
	}
	lines = append(lines, "")

	// Error details
	var failingItems []toolItem
	for _, it := range items {
		if it.s.Failures > 0 {
			failingItems = append(failingItems, it)
		}
	}
	if len(failingItems) > 0 {
		lines = append(lines, "## Error Details per Tool", "")
		for _, it := range failingItems {
			lines = append(lines, fmt.Sprintf("### `%s` (%d/%d failures)", it.name, it.s.Failures, it.s.Calls))
			for _, ec := range topErrors(it.s.Errors, 5) {
				lines = append(lines, fmt.Sprintf("- (%dx) `%s`", ec.cnt, ec.msg))
			}
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func RenderBashSection(candidates []BashCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "## Repeated Bash Commands — Consider Creating MCP Tools", "")
	lines = append(lines, fmt.Sprintf("These command patterns have been run %d+ times. "+
		"Ask the user if they'd like to create an MCP tool for any of them.", BashSuggestThreshold), "")
	lines = append(lines, "| Pattern | Uses | Example |")
	lines = append(lines, "|---------|------|---------|")
	for _, c := range candidates {
		ex := c.Example
		if len(ex) > 80 {
			ex = ex[:80] + "…"
		}
		ex = strings.ReplaceAll(ex, "|", "\\|")
		lines = append(lines, fmt.Sprintf("| `%s` | %d | `%s` |", c.Pattern, c.Count, ex))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func topError(errors map[string]int) string {
	best := ""
	bestCnt := 0
	for k, v := range errors {
		if v > bestCnt {
			bestCnt = v
			best = k
		}
	}
	if len(best) > 70 {
		return best[:70] + "…"
	}
	return best
}

type errCount struct {
	msg string
	cnt int
}

func topErrors(errors map[string]int, n int) []errCount {
	var all []errCount
	for k, v := range errors {
		all = append(all, errCount{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].cnt > all[j].cnt })
	if len(all) > n {
		all = all[:n]
	}
	return all
}
