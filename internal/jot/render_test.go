package jot

import (
	"strings"
	"testing"
)

func render(t *testing.T, body string, titles map[string]string) string {
	t.Helper()
	return renderMarkdown(Document{ID: "topic/page", Title: "Page", Body: body}, titles)
}

func TestRendererSupportsTables(t *testing.T) {
	out := render(t, "| Field | Type |\n| --- | --- |\n| id | string |\n", nil)
	for _, want := range []string{"<table>", "<th>Field</th>", "<td>id</td>", `class="tablewrap"`} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRendererSupportsOrderedListsAndRules(t *testing.T) {
	out := render(t, "1. first\n2. second\n\n---\n\ntext\n", nil)
	if !strings.Contains(out, "<ol><li>first</li><li>second</li></ol>") {
		t.Errorf("ordered list wrong:\n%s", out)
	}
	if !strings.Contains(out, "<hr>") {
		t.Errorf("horizontal rule missing:\n%s", out)
	}
}

func TestRendererSupportsEmphasis(t *testing.T) {
	out := render(t, "This is **bold** and *italic* and _also italic_ and `code`.\n", nil)
	for _, want := range []string{"<strong>bold</strong>", "<em>italic</em>", "<em>also italic</em>", "<code>code</code>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestRendererResolvesWikiLinks(t *testing.T) {
	titles := map[string]string{"topic/other": "The Other Page"}
	out := render(t, "See [[other]] for detail.\n", titles)
	if !strings.Contains(out, `href="/wiki/topic/other"`) {
		t.Errorf("wiki link not resolved:\n%s", out)
	}
	if !strings.Contains(out, "The Other Page") {
		t.Errorf("wiki link should use the target's title:\n%s", out)
	}
	out = render(t, "See [[other|custom label]].\n", titles)
	if !strings.Contains(out, "custom label") {
		t.Errorf("piped label ignored:\n%s", out)
	}
}

func TestRendererEscapesHostileContent(t *testing.T) {
	out := render(t, "<script>alert(1)</script>\n\n[x](javascript:alert(1))\n", nil)
	if strings.Contains(out, "<script>") {
		t.Fatalf("script tag was not escaped:\n%s", out)
	}
	if strings.Contains(out, `href="javascript:`) {
		t.Fatalf("javascript: URL was not neutralised:\n%s", out)
	}
}

func TestRendererKeepsAuthoritativeBadge(t *testing.T) {
	out := render(t, authorityMarker+"\nMarkdown is canonical.\n", nil)
	if !strings.Contains(out, `class="badge">Authoritative`) {
		t.Fatalf("authoritative badge missing:\n%s", out)
	}
}

func TestHighlightWrapsTermsAndEscapes(t *testing.T) {
	got := highlight("BM25 ranks <b>passages</b>", tokenize("bm25 passages"))
	if !strings.Contains(got, "<mark>BM25</mark>") {
		t.Errorf("query term not highlighted: %s", got)
	}
	if strings.Contains(got, "<b>") {
		t.Errorf("content was not escaped: %s", got)
	}
}

func TestFootnotesRenderAsSourceReferences(t *testing.T) {
	out := render(t, "Ranking uses BM25[^spec].\n", nil)
	if !strings.Contains(out, `href="#source-spec"`) {
		t.Fatalf("footnote did not render:\n%s", out)
	}
}

func TestRendererNestsLists(t *testing.T) {
	out := render(t, "- outer\n  - inner\n  - inner two\n- outer two\n", nil)
	want := "<ul><li>outer<ul><li>inner</li><li>inner two</li></ul></li><li>outer two</li></ul>"
	if !strings.Contains(out, want) {
		t.Fatalf("nested list wrong:\ngot  %s\nwant %s", out, want)
	}
}

func TestRendererNestsMixedListTypes(t *testing.T) {
	out := render(t, "1. first\n   - detail\n2. second\n", nil)
	for _, want := range []string{"<ol><li>first<ul>", "<ul><li>detail</li></ul></li>", "<li>second</li>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "<ol>") != 1 {
		t.Errorf("outer ordered list should not restart:\n%s", out)
	}
}

func TestRendererClosesAllListLevels(t *testing.T) {
	out := render(t, "- a\n  - b\n    - c\n\nAfter.\n", nil)
	if strings.Count(out, "<ul>") != strings.Count(out, "</ul>") {
		t.Fatalf("unbalanced list tags:\n%s", out)
	}
	if !strings.Contains(out, "<p>After.</p>") {
		t.Fatalf("paragraph after list missing:\n%s", out)
	}
}
