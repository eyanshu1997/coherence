package docgen

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// MarkdownToHTML converts markdown to HTML, supporting GFM tables, nested
// lists, fenced code blocks, Mermaid diagrams, and inline formatting.
// It faithfully ports the Python markdown_to_html() from generate_doc.py.
func MarkdownToHTML(text, jiraBaseURL, githubOrg string) string {
	stash := map[string]string{}
	counter := 0

	addStash := func(html string) string {
		key := fmt.Sprintf("\x00BLOCK%d\x00", counter)
		counter++
		stash[key] = html
		return key
	}

	inline := func(s string) string {
		return applyInline(s, jiraBaseURL, githubOrg)
	}

	esc := func(s string) string {
		return html.EscapeString(s)
	}

	// 1. Stash fenced code blocks
	fencedRe := regexp.MustCompile("(?s)```(\\w*)\n(.*?)```")
	text = fencedRe.ReplaceAllStringFunc(text, func(m string) string {
		groups := fencedRe.FindStringSubmatch(m)
		lang := groups[1]
		code := esc(groups[2])
		return addStash(fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>\n", lang, code))
	})

	// 2. Stash raw HTML blocks
	rawHTMLRe := regexp.MustCompile(`(?ms)^(<(?:div|button|label|input|form|table|ul|ol|li|hr|details|summary)[^>]*>.*?)(?:\n\n|\z)`)
	text = rawHTMLRe.ReplaceAllStringFunc(text, func(m string) string {
		return addStash(m)
	})

	// 3. Stash GFM tables
	text = collectTables(text, addStash, inline)

	// 4. Stash block elements
	hrRe := regexp.MustCompile(`(?m)^---$`)
	text = hrRe.ReplaceAllStringFunc(text, func(m string) string { return addStash("<hr>\n") })

	h4Re := regexp.MustCompile(`(?m)^#### (.+)$`)
	text = h4Re.ReplaceAllStringFunc(text, func(m string) string {
		groups := h4Re.FindStringSubmatch(m)
		return addStash(fmt.Sprintf("<h4>%s</h4>\n", inline(groups[1])))
	})
	h3Re := regexp.MustCompile(`(?m)^### (.+)$`)
	text = h3Re.ReplaceAllStringFunc(text, func(m string) string {
		groups := h3Re.FindStringSubmatch(m)
		return addStash(fmt.Sprintf("<h3>%s</h3>\n", inline(groups[1])))
	})
	h2Re := regexp.MustCompile(`(?m)^## (.+)$`)
	text = h2Re.ReplaceAllStringFunc(text, func(m string) string {
		groups := h2Re.FindStringSubmatch(m)
		return addStash(fmt.Sprintf("<h2>%s</h2>\n", inline(groups[1])))
	})
	h1Re := regexp.MustCompile(`(?m)^# (.+)$`)
	text = h1Re.ReplaceAllStringFunc(text, func(m string) string {
		groups := h1Re.FindStringSubmatch(m)
		return addStash(fmt.Sprintf("<h2>%s</h2>\n", inline(groups[1])))
	})

	bqRe := regexp.MustCompile(`(?m)^> (.+)$`)
	text = bqRe.ReplaceAllStringFunc(text, func(m string) string {
		groups := bqRe.FindStringSubmatch(m)
		return addStash(fmt.Sprintf("<blockquote>%s</blockquote>\n", inline(groups[1])))
	})

	// 5. Stash list blocks
	listBlockRe := regexp.MustCompile(`(?m)(^[ \t]*(?:[-*]|\d+\.) .+(?:\n|$))+`)
	text = listBlockRe.ReplaceAllStringFunc(text, func(m string) string {
		lines := strings.Split(strings.TrimRight(m, "\n"), "\n")
		return addStash(parseListBlock(lines, inline) + "\n")
	})

	// 6. Paragraph wrapping
	parts := regexp.MustCompile(`\n{2,}`).Split(text, -1)
	var output []string
	isStashOnly := regexp.MustCompile(`^(\x00BLOCK\d+\x00\s*)+$`)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if isStashOnly.MatchString(part) {
			output = append(output, part)
		} else {
			output = append(output, fmt.Sprintf("<p>%s</p>", inline(part)))
		}
	}
	text = strings.Join(output, "\n")

	// 7. Restore stashes
	for k, v := range stash {
		text = strings.ReplaceAll(text, k, v)
	}

	return text
}

func collectTables(text string, addStash func(string) string, inline func(string) string) string {
	lines := strings.Split(text, "\n")
	var out []string
	var buf []string

	sepRe := regexp.MustCompile(`^\|?[\s:|\-]+\|[\s:|\-|]*$`)

	flushTable := func() {
		if len(buf) == 0 {
			return
		}
		var rows [][]string
		for _, l := range buf {
			if strings.TrimSpace(strings.ReplaceAll(l, "|", "")) == "" {
				continue
			}
			rows = append(rows, splitCells(l))
		}

		var theadRows, tbodyRows [][]string
		headerDone := false
		for _, row := range rows {
			if !headerDone {
				joined := strings.Join(row, "|")
				if sepRe.MatchString(joined) || sepRe.MatchString("|"+joined+"|") {
					headerDone = true
					continue
				}
			}
			if !headerDone {
				theadRows = append(theadRows, row)
			} else {
				tbodyRows = append(tbodyRows, row)
			}
		}

		var sb strings.Builder
		sb.WriteString("<table>\n")
		if len(theadRows) > 0 {
			sb.WriteString("  <thead>\n")
			for _, row := range theadRows {
				sb.WriteString("    <tr>")
				for _, c := range row {
					sb.WriteString(fmt.Sprintf("<th>%s</th>", inline(strings.TrimSpace(c))))
				}
				sb.WriteString("</tr>\n")
			}
			sb.WriteString("  </thead>\n")
		}
		if len(tbodyRows) > 0 {
			sb.WriteString("  <tbody>\n")
			for _, row := range tbodyRows {
				sb.WriteString("    <tr>")
				for _, c := range row {
					sb.WriteString(fmt.Sprintf("<td>%s</td>", inline(strings.TrimSpace(c))))
				}
				sb.WriteString("</tr>\n")
			}
			sb.WriteString("  </tbody>\n")
		}
		sb.WriteString("</table>")
		out = append(out, addStash(sb.String()+"\n"))
		buf = buf[:0]
	}

	pipeRe := regexp.MustCompile(`^\s*\|`)
	for _, line := range lines {
		if pipeRe.MatchString(line) {
			buf = append(buf, line)
		} else {
			if len(buf) > 0 {
				flushTable()
			}
			out = append(out, line)
		}
	}
	flushTable()
	return strings.Join(out, "\n")
}

func splitCells(row string) []string {
	row = strings.Trim(row, " \t")
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	return strings.Split(row, "|")
}

func parseListBlock(lines []string, inline func(string) string) string {
	ulRe := regexp.MustCompile(`^(\s*)[-*] (.+)$`)
	olRe := regexp.MustCompile(`^(\s*)\d+\. (.+)$`)

	var render func(lines []string, baseIndent int) string
	render = func(lines []string, baseIndent int) string {
		if len(lines) == 0 {
			return ""
		}
		var tag string
		var items []string
		i := 0
		for i < len(lines) {
			line := lines[i]
			mUL := ulRe.FindStringSubmatch(line)
			mOL := olRe.FindStringSubmatch(line)
			m := mUL
			curTag := "ul"
			if m == nil {
				m = mOL
				curTag = "ol"
			}
			if m == nil {
				i++
				continue
			}
			if tag == "" {
				tag = curTag
			}
			indent := len(m[1])
			itemText := inline(m[2])

			// collect child lines (more indented)
			var children []string
			j := i + 1
			for j < len(lines) {
				next := lines[j]
				if strings.TrimSpace(next) == "" {
					j++
					continue
				}
				childM := regexp.MustCompile(`^(\s+)[-*\d]`).FindStringSubmatch(next)
				if childM != nil && len(childM[1]) > indent {
					children = append(children, next)
					j++
				} else {
					break
				}
			}

			if len(children) > 0 {
				items = append(items, fmt.Sprintf("  <li>%s\n%s\n  </li>", itemText, render(children, indent+1)))
			} else {
				items = append(items, fmt.Sprintf("  <li>%s</li>", itemText))
			}
			i = j
		}
		if tag == "" {
			tag = "ul"
		}
		return fmt.Sprintf("<%s>\n%s\n</%s>", tag, strings.Join(items, "\n"), tag)
	}
	return render(lines, 0)
}

var (
	imgRe    = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	codeRe   = regexp.MustCompile("`([^`\n]+)`")
	boldRe   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe = regexp.MustCompile(`\*(.+?)\*`)
	linkRe   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	bareURLRe = regexp.MustCompile(`(https?://[^\s<>"']+)`)
	anchorRe = regexp.MustCompile(`(?s)<a\b[^>]*>.*?</a>`)
)

func applyInline(s, jiraBaseURL, githubOrg string) string {
	// images — must run before link processing so ![alt](url) isn't partially matched
	s = imgRe.ReplaceAllStringFunc(s, func(m string) string {
		groups := imgRe.FindStringSubmatch(m)
		alt := html.EscapeString(groups[1])
		src := groups[2]
		if strings.HasPrefix(src, "javascript:") || strings.HasPrefix(src, "data:") {
			return m
		}
		return fmt.Sprintf(`<img src="%s" alt="%s" class="doc-image" loading="lazy">`,
			html.EscapeString(src), alt)
	})
	// inline code (escape content)
	s = codeRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := codeRe.FindStringSubmatch(m)[1]
		return "<code>" + html.EscapeString(inner) + "</code>"
	})
	// bold, italic
	s = boldRe.ReplaceAllString(s, "<strong>$1</strong>")
	s = italicRe.ReplaceAllString(s, "<em>$1</em>")
	// markdown links
	s = linkRe.ReplaceAllString(s, `<a href="$2">$1</a>`)

	// GitHub PR URLs
	if githubOrg != "" {
		ghRe := regexp.MustCompile(`https://github\.com/(` + regexp.QuoteMeta(githubOrg) + `/[^/]+)/pull/(\d+)(?:[^\w/]|$)`)
		s = ghRe.ReplaceAllStringFunc(s, func(m string) string {
			groups := ghRe.FindStringSubmatch(m)
			suffix := m[len(groups[0]):]
			return fmt.Sprintf(`<a href="https://github.com/%s/pull/%s">%s#PR-%s</a>%s`,
				groups[1], groups[2], groups[1], groups[2], suffix)
		})
	}

	// Jira browse URLs
	if jiraBaseURL != "" {
		jiraBrowseRe := regexp.MustCompile(`https?://` + regexp.QuoteMeta(strings.TrimPrefix(strings.TrimPrefix(jiraBaseURL, "https://"), "http://")) + `/browse/([A-Z]+-\d+)(?:[^\w]|$)`)
		s = jiraBrowseRe.ReplaceAllStringFunc(s, func(m string) string {
			groups := jiraBrowseRe.FindStringSubmatch(m)
			suffix := m[len(groups[0]):]
			return fmt.Sprintf(`<a href="%s/browse/%s">%s</a>%s`, jiraBaseURL, groups[1], groups[1], suffix)
		})
	}

	// stash existing <a> tags to prevent double-processing
	linkStash := map[string]string{}
	stashIdx := 0
	s = anchorRe.ReplaceAllStringFunc(s, func(m string) string {
		key := fmt.Sprintf("\x00LNK%d\x00", stashIdx)
		stashIdx++
		linkStash[key] = m
		return key
	})

	// bare https:// URLs
	s = bareURLRe.ReplaceAllString(s, `<a href="$1">$1</a>`)

	// bare Jira IDs
	if jiraBaseURL != "" {
		jiraIDRe := regexp.MustCompile(`(?:[^"/=\w\-])([A-Z]+-\d+)(?:\D|$)`)
		s = jiraIDRe.ReplaceAllStringFunc(s, func(m string) string {
			groups := jiraIDRe.FindStringSubmatch(m)
			prefix := m[:len(m)-len(groups[0])+1]  // keep leading char
			ticket := groups[1]
			suffix := m[len(prefix)+len(ticket):]
			return prefix + fmt.Sprintf(`<a href="%s/browse/%s">%s</a>`, jiraBaseURL, ticket, ticket) + suffix
		})
	}

	// restore stashed links
	for k, v := range linkStash {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}
