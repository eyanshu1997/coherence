package docgen

import (
	"strings"
	"testing"
)

func TestMarkdownToHTML_Headings(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"# Title\n", "<h2>Title</h2>"},
		{"## Section\n", "<h2>Section</h2>"},
		{"### Sub\n", "<h3>Sub</h3>"},
		{"#### Deep\n", "<h4>Deep</h4>"},
	}
	for _, tc := range tests {
		out := MarkdownToHTML(tc.input, "", "")
		if !strings.Contains(out, tc.contains) {
			t.Errorf("MarkdownToHTML(%q) missing %q\ngot: %s", tc.input, tc.contains, out)
		}
	}
}

func TestMarkdownToHTML_CodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```\n"
	out := MarkdownToHTML(md, "", "")
	if !strings.Contains(out, `<pre><code class="language-go">`) {
		t.Errorf("expected fenced code block, got: %s", out)
	}
	if !strings.Contains(out, `fmt.Println(&#34;hello&#34;)`) {
		t.Errorf("expected HTML-escaped quotes, got: %s", out)
	}
}

func TestMarkdownToHTML_Bold(t *testing.T) {
	out := MarkdownToHTML("**bold text**", "", "")
	if !strings.Contains(out, "<strong>bold text</strong>") {
		t.Errorf("expected bold, got: %s", out)
	}
}

func TestMarkdownToHTML_InlineCode(t *testing.T) {
	out := MarkdownToHTML("`code`", "", "")
	if !strings.Contains(out, "<code>code</code>") {
		t.Errorf("expected inline code, got: %s", out)
	}
}

func TestMarkdownToHTML_Link(t *testing.T) {
	out := MarkdownToHTML("[text](https://example.com)", "", "")
	if !strings.Contains(out, `<a href="https://example.com">text</a>`) {
		t.Errorf("expected markdown link, got: %s", out)
	}
}

func TestMarkdownToHTML_Table(t *testing.T) {
	md := "| A | B |\n|---|---|\n| 1 | 2 |\n"
	out := MarkdownToHTML(md, "", "")
	if !strings.Contains(out, "<table>") {
		t.Errorf("expected table, got: %s", out)
	}
	if !strings.Contains(out, "<th>A</th>") {
		t.Errorf("expected th header, got: %s", out)
	}
	if !strings.Contains(out, "<td>1</td>") {
		t.Errorf("expected td cell, got: %s", out)
	}
}

func TestMarkdownToHTML_UnorderedList(t *testing.T) {
	md := "- item one\n- item two\n"
	out := MarkdownToHTML(md, "", "")
	if !strings.Contains(out, "<ul>") || !strings.Contains(out, "<li>item one</li>") {
		t.Errorf("expected unordered list, got: %s", out)
	}
}

func TestMarkdownToHTML_OrderedList(t *testing.T) {
	md := "1. first\n2. second\n"
	out := MarkdownToHTML(md, "", "")
	if !strings.Contains(out, "<ol>") || !strings.Contains(out, "<li>first</li>") {
		t.Errorf("expected ordered list, got: %s", out)
	}
}

func TestMarkdownToHTML_Blockquote(t *testing.T) {
	out := MarkdownToHTML("> quoted\n", "", "")
	if !strings.Contains(out, "<blockquote>quoted</blockquote>") {
		t.Errorf("expected blockquote, got: %s", out)
	}
}

func TestMarkdownToHTML_HR(t *testing.T) {
	out := MarkdownToHTML("---\n", "", "")
	if !strings.Contains(out, "<hr>") {
		t.Errorf("expected hr, got: %s", out)
	}
}

func TestMarkdownToHTML_NoDoubleEscapeInCode(t *testing.T) {
	md := "```\n<script>alert('xss')</script>\n```\n"
	out := MarkdownToHTML(md, "", "")
	if strings.Contains(out, "<script>") {
		t.Errorf("script tag should be escaped in code block, got: %s", out)
	}
}

func TestMarkdownToHTML_JiraID(t *testing.T) {
	out := MarkdownToHTML("Fix PROJ-123 now", "https://jira.example.com", "")
	if !strings.Contains(out, `href="https://jira.example.com/browse/PROJ-123"`) {
		t.Errorf("expected Jira link, got: %s", out)
	}
}
