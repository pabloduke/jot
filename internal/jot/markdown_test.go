package jot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validConcept = `---
type: Concept
title: Retrieval Systems
description: How Jot finds useful personal knowledge.
timestamp: 2026-08-29T12:00:00Z
tags: [search, knowledge]
---

# Retrieval Systems

BM25 lexical search is useful for compact Markdown collections.
`

func testVault(t *testing.T) string {
	t.Helper()
	t.Setenv("JOT_NO_SYNC", "1")
	root := t.TempDir()
	if err := scaffoldVault(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestValidateConceptAndGenerateIndex(t *testing.T) {
	root := testVault(t)
	path := filepath.Join(root, "wiki", "systems", "retrieval.md")
	if err := atomicWrite(path, []byte(validConcept), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := refreshDerived(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Concepts != 1 {
		t.Fatalf("concepts = %d", result.Concepts)
	}
	index, err := os.ReadFile(filepath.Join(root, "wiki", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "[Retrieval Systems](systems/retrieval.md)") {
		t.Fatalf("index did not contain concept:\n%s", index)
	}
}

func TestAuthoritativeBlocksRequireExplicitChange(t *testing.T) {
	root := testVault(t)
	path := filepath.Join(root, "wiki", "preferences", "tools.md")
	original := strings.Replace(validConcept, "BM25 lexical search is useful", authorityMarker+"\nUse plain files whenever possible", 1)
	if err := atomicWrite(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshDerived(root, false); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(original, "Use plain files whenever possible", "Use a hosted database", 1)
	if err := atomicWrite(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshDerived(root, false); err == nil || !strings.Contains(err.Error(), "authoritative block") {
		t.Fatalf("expected authoritative change error, got %v", err)
	}
	if _, err := refreshDerived(root, true); err != nil {
		t.Fatalf("explicit authoritative change failed: %v", err)
	}
}

func TestRawCaptureImmutability(t *testing.T) {
	root := testVault(t)
	c, err := addCapture(root, "Session", "message", "", "Completed the search implementation.", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(c.Path))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("changed\n")
	_ = f.Close()
	if _, err := validateVault(root, false); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected immutable capture error, got %v", err)
	}
}

func TestConceptRequiresFrontmatter(t *testing.T) {
	if _, err := validateConcept("wiki/test.md", []byte("# Missing metadata\n")); err == nil {
		t.Fatal("expected missing frontmatter error")
	}
}

func TestLintFindsBrokenLinksAndUnregisteredRaw(t *testing.T) {
	root := testVault(t)
	broken := strings.Replace(validConcept, "BM25 lexical search is useful", "See [missing](missing.md).", 1)
	if err := atomicWrite(filepath.Join(root, "wiki", "systems", "retrieval.md"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(root, "raw", "inbox", "manual.md"), []byte("unregistered"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := validateVault(root, false)
	if err == nil || !strings.Contains(err.Error(), "broken link") || !strings.Contains(err.Error(), "unregistered raw") {
		t.Fatalf("expected link and raw inventory errors, got %v", err)
	}
}

func TestReservedConceptIDs(t *testing.T) {
	for _, id := range []string{"index", "topic/log", "../escape", `bad\\path`} {
		if _, err := safeID(id); err == nil {
			t.Fatalf("expected %q to be rejected", id)
		}
	}
}
