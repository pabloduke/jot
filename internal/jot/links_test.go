package jot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLinkGraphResolvesBothLinkForms(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/target", "Target", longBody)
	writeConcept(t, root, "a/wiki-source", "Wiki Source", "Refers to [[a/target]]. "+longBody)
	writeConcept(t, root, "a/md-source", "Md Source", "Refers to [Target](target.md). "+longBody)

	docs, _, err := loadConcepts(root, true)
	if err != nil {
		t.Fatal(err)
	}
	graph := buildLinkGraph(root, docs)
	back := graph.Backlinks("a/target")
	if len(back) != 2 || back[0] != "a/md-source" || back[1] != "a/wiki-source" {
		t.Fatalf("backlinks = %v", back)
	}
	if len(graph.Issues) != 0 {
		t.Fatalf("unexpected link issues: %v", graph.Issues)
	}
}

func TestUnresolvedWikiLinkIsReported(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/source", "Source", "Refers to [[nowhere-at-all]]. "+longBody)
	docs, _, err := loadConcepts(root, true)
	if err != nil {
		t.Fatal(err)
	}
	graph := buildLinkGraph(root, docs)
	if len(graph.Issues) != 1 || !strings.Contains(graph.Issues[0], "nowhere-at-all") {
		t.Fatalf("issues = %v", graph.Issues)
	}
}

func TestWikiLinkResolvesByUniqueBasename(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "deep/nested/target", "Target", longBody)
	writeConcept(t, root, "other/source", "Source", "See [[target]]. "+longBody)
	docs, _, err := loadConcepts(root, true)
	if err != nil {
		t.Fatal(err)
	}
	graph := buildLinkGraph(root, docs)
	if back := graph.Backlinks("deep/nested/target"); len(back) != 1 {
		t.Fatalf("basename resolution failed: %v (issues %v)", back, graph.Issues)
	}
}

func TestOrphanDetection(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/linked", "Linked", longBody)
	writeConcept(t, root, "a/lonely", "Lonely", longBody)
	writeConcept(t, root, "a/source", "Source", "See [[a/linked]]. "+longBody)

	docs, _, err := loadConcepts(root, true)
	if err != nil {
		t.Fatal(err)
	}
	orphans := buildLinkGraph(root, docs).Orphans(docs)
	if len(orphans) != 2 || orphans[0] != "a/lonely" || orphans[1] != "a/source" {
		t.Fatalf("orphans = %v", orphans)
	}
}

func TestServerRendersBacklinksAndSources(t *testing.T) {
	root := testVault(t)
	page := "---\ntype: Concept\ntitle: Target\ndescription: A target\n" +
		"generated:\n  by: process:jot\n  at: 2026-08-01T00:00:00Z\n" +
		"sources:\n  - id: spec\n    resource: https://example.com/spec\n    title: The spec\n" +
		"---\n\n# Target\n\n" + longBody + "\n"
	if err := atomicWrite(root+"/wiki/a/target.md", []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	writeConcept(t, root, "a/source", "Source Page", "See [[a/target]]. "+longBody)

	handler := newWikiHandler(root)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/wiki/a/target", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, want := range []string{"Referenced by", "/wiki/a/source", "Source Page", "Sources", "https://example.com/spec"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q:\n%s", want, body)
		}
	}
}

func TestServerLogRoute(t *testing.T) {
	root := testVault(t)
	if err := appendLog(root, "ingest", "compiled something", "Capture: abc"); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	newWikiHandler(root).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/log", nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "compiled something") {
		t.Fatalf("log route: %d %s", res.Code, res.Body.String())
	}
}

func TestServerServesThemeScriptUnderCSP(t *testing.T) {
	root := testVault(t)
	handler := newWikiHandler(root)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/assets/wiki.js", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	csp := res.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP must allow the theme script from self only: %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Errorf("CSP must not relax to unsafe-inline: %q", csp)
	}
}

func TestServerSetsRobotsNoindex(t *testing.T) {
	root := testVault(t)
	res := httptest.NewRecorder()
	newWikiHandler(root).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := res.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag = %q", got)
	}
	if !strings.Contains(res.Body.String(), `name="robots" content="noindex,nofollow"`) {
		t.Error("robots meta tag missing")
	}
}
