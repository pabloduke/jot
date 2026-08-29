package jot

import "testing"

func TestBM25RanksRelevantPassage(t *testing.T) {
	docs := []Document{
		{ID: "work/releases", Path: "wiki/work/releases.md", Title: "Release Process", Description: "How software ships", Type: "Howto", Body: "# Deployment\n\nDeploy releases on Tuesdays after the integration suite passes."},
		{ID: "food/bread", Path: "wiki/food/bread.md", Title: "Bread", Description: "Sourdough notes", Type: "Note", Body: "# Starter\n\nFeed the starter with flour and water."},
	}
	hits := rankDocuments(docs, "when do we deploy releases", 4, 2000)
	if len(hits) == 0 || hits[0].ID != "work/releases" {
		t.Fatalf("unexpected hits: %#v", hits)
	}
}

func TestContextBudget(t *testing.T) {
	docs := []Document{{ID: "x", Path: "wiki/x.md", Title: "Alpha", Description: "Alpha", Type: "Note", Body: "# Alpha\n\n" + string(make([]byte, 1000))}}
	if hits := rankDocuments(docs, "alpha", 5, 1); len(hits) != 0 {
		t.Fatalf("expected budget to exclude hit: %#v", hits)
	}
}
