package jot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyCompilesCaptureAtomically(t *testing.T) {
	root := testVault(t)
	c, err := addCapture(root, "Session", "message", "", "Implemented ranked search.", nil)
	if err != nil {
		t.Fatal(err)
	}
	req := ApplyRequest{
		CaptureID:   c.ID,
		Disposition: "compiled",
		Summary:     "compile ranked search session",
		Upserts:     []Upsert{{ID: "work/ranked-search", Content: validConcept}},
	}
	result, err := applyRequest(context.Background(), root, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 1 {
		t.Fatalf("changed = %#v", result.Changed)
	}
	m, _ := loadManifest(root)
	if m.Captures[c.ID].Status != "compiled" {
		t.Fatalf("capture status = %q", m.Captures[c.ID].Status)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "work", "ranked-search.md")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRollsBackInvalidAuthoritativeChange(t *testing.T) {
	root := testVault(t)
	path := filepath.Join(root, "wiki", "preferences", "storage.md")
	original := strings.Replace(validConcept, "BM25 lexical search is useful", authorityMarker+"\nMarkdown is canonical", 1)
	if err := atomicWrite(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshDerived(root, false); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(original, "Markdown is canonical", "The database is canonical", 1)
	_, err := applyRequest(context.Background(), root, ApplyRequest{Summary: "bad rewrite", Upserts: []Upsert{{ID: "preferences/storage", Content: changed}}})
	if err == nil {
		t.Fatal("expected authoritative change rejection")
	}
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Fatalf("transaction was not rolled back:\n%s", after)
	}
}

func TestNoMaterialRejectsChanges(t *testing.T) {
	root := testVault(t)
	_, err := applyRequest(context.Background(), root, ApplyRequest{Summary: "none", Disposition: "no-material", Upserts: []Upsert{{ID: "x", Content: validConcept}}})
	if err == nil {
		t.Fatal("expected no-material validation error")
	}
}

func TestApplyRejectsUpsertAtMoveDestination(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "work/original", "Original", longBody)

	_, err := applyRequest(context.Background(), root, ApplyRequest{
		Summary: "conflicting targets",
		Moves:   []Move{{From: "work/original", To: "work/moved"}},
		Upserts: []Upsert{{ID: "work/moved", Content: validConcept}},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate transaction target") {
		t.Fatalf("expected duplicate-target rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "work", "original.md")); err != nil {
		t.Fatalf("move source changed despite validation failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "work", "moved.md")); !os.IsNotExist(err) {
		t.Fatalf("move destination exists despite validation failure: %v", err)
	}
}
