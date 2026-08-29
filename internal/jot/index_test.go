package jot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLexicalIndexIsCachedAndInvalidated(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "work/releases", "Releases", longBody)

	if _, err := os.Stat(indexPath(root)); !os.IsNotExist(err) {
		t.Fatal("index cache should not exist before the first search")
	}
	first, err := buildCorpus(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("no chunks built")
	}
	info, err := os.Stat(indexPath(root))
	if err != nil {
		t.Fatalf("index cache was not written: %v", err)
	}

	// A second pass over unchanged files reuses the cache and rewrites nothing.
	if _, err := buildCorpus(root, false); err != nil {
		t.Fatal(err)
	}
	again, err := os.Stat(indexPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(info.ModTime()) {
		t.Error("an unchanged corpus should not rewrite the index cache")
	}

	// Editing a page must be picked up.
	writeConcept(t, root, "work/releases", "Releases", "Now the page talks about hydroponics and nutrient film technique instead of anything else at all.")
	updated, err := buildCorpus(root, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range updated {
		if c.tf["hydroponics"] > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("index cache was not invalidated after an edit")
	}
}

func TestSearchSurvivesACorruptIndexCache(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "work/releases", "Releases", longBody)
	if err := atomicWrite(indexPath(root), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, err := searchVault(root, "deployment release", SearchOptions{Limit: 5, MaxChars: 2000})
	if err != nil {
		t.Fatalf("a corrupt cache must not break search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits after rebuilding from a corrupt cache")
	}
}

func TestDisposableStateIsGitignored(t *testing.T) {
	root := testVault(t)
	ignore := readFileString(t, filepath.Join(root, ".gitignore"))
	for _, name := range []string{".jot/index.json", ".jot/maintain.json", ".jot/queries.json"} {
		if !strings.Contains(ignore, name) {
			t.Errorf("%s should be gitignored:\n%s", name, ignore)
		}
	}
}

func TestPruningRemovesDeletedFilesFromCache(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/one", "One", longBody)
	writeConcept(t, root, "a/two", "Two", longBody)
	if _, err := buildCorpus(root, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "wiki", "a", "two.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := buildCorpus(root, false); err != nil {
		t.Fatal(err)
	}
	idx := loadLexIndex(root)
	if _, ok := idx.Entries["wiki/a/two.md"]; ok {
		t.Fatal("deleted file is still in the index cache")
	}
}
