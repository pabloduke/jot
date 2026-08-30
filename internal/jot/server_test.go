package jot

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWikiServerRendersIndexPageAndSearch(t *testing.T) {
	root := testVault(t)
	content := strings.Replace(validConcept, "BM25 lexical search is useful", authorityMarker+"\nBM25 lexical search is useful", 1)
	if err := atomicWrite(filepath.Join(root, "wiki", "systems", "retrieval.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshDerived(root, false); err != nil {
		t.Fatal(err)
	}
	handler := newWikiHandler(root)

	tests := []struct {
		path string
		want []string
	}{
		{"/", []string{"Knowledge Wiki", "/wiki/systems/retrieval", "Retrieval Systems", `/assets/favicon.svg`}},
		{"/wiki/systems/retrieval", []string{"Authoritative", "BM25 lexical search", "Retrieval Systems"}},
		{"/search?q=BM25", []string{"results for", "<mark>BM25</mark> lexical search", "/wiki/systems/retrieval"}},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("%s status = %d: %s", tc.path, res.Code, res.Body.String())
		}
		for _, want := range tc.want {
			if !strings.Contains(res.Body.String(), want) {
				t.Errorf("%s did not contain %q: %s", tc.path, want, res.Body.String())
			}
		}
		if got := res.Header().Get("Content-Security-Policy"); got == "" {
			t.Errorf("%s has no content security policy", tc.path)
		}
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/assets/favicon.svg", nil))
	if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("unexpected favicon response: %d %q", res.Code, res.Header().Get("Content-Type"))
	}
	if !strings.Contains(res.Body.String(), `<svg`) || !strings.Contains(res.Body.String(), `#df7542`) {
		t.Fatalf("favicon response is not the embedded Jot icon: %s", res.Body.String())
	}
}

func TestWikiServerDoesNotExposeRawOrTraversal(t *testing.T) {
	root := testVault(t)
	if err := os.WriteFile(filepath.Join(root, "raw", "secret.md"), []byte("private source"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := newWikiHandler(root)
	for _, path := range []string{"/raw/secret.md", "/wiki/../../raw/secret", "/.jot/manifest.json"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusNotFound && res.Code != http.StatusMovedPermanently {
			t.Fatalf("%s status = %d, want 404 or canonical-path redirect", path, res.Code)
		}
		if strings.Contains(res.Body.String(), "private source") {
			t.Fatalf("%s exposed raw content", path)
		}
	}
}

func TestWikiServerHealth(t *testing.T) {
	root := testVault(t)
	res := httptest.NewRecorder()
	newWikiHandler(root).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected health response: %d %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), root) || strings.Contains(res.Body.String(), `"wiki"`) {
		t.Fatalf("health response leaked vault information: %s", res.Body.String())
	}
}
