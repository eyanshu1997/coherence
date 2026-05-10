package analytics

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"time"
)

const BashSuggestThreshold = 5

type BashEntry struct {
	TS      string `json:"ts"`
	Pattern string `json:"pattern"`
	Cmd     string `json:"cmd"`
}

type BashCandidate struct {
	Pattern string
	Count   int
	Example string
}

func LoadBashEntries(logPath string, sinceDays int) []BashEntry {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	cutoff := time.Now().UTC().Add(-time.Duration(sinceDays) * 24 * time.Hour)
	var out []BashEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var e BashEntry
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

func AnalyzeBash(entries []BashEntry) []BashCandidate {
	counts := map[string]int{}
	examples := map[string]string{}
	for _, e := range entries {
		if e.Pattern == "" {
			continue
		}
		counts[e.Pattern]++
		if _, ok := examples[e.Pattern]; !ok {
			examples[e.Pattern] = e.Cmd
		}
	}
	var result []BashCandidate
	for pat, cnt := range counts {
		if cnt >= BashSuggestThreshold {
			result = append(result, BashCandidate{Pattern: pat, Count: cnt, Example: examples[pat]})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Count > result[j].Count })
	return result
}
