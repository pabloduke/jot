package jot

import (
	"path/filepath"
	"strings"
	"testing"
)

const okfConcept = `---
type: Concept
title: Retrieval
description: How relevant knowledge is selected.
status: stable
stale_after: 2027-01-01T00:00:00Z
tags:
  - search
  - okf
generated:
  by: process:jot
  at: 2026-08-29T12:00:00Z
verified:
  - by: human:stephen
    at: 2026-08-29T13:00:00Z
sources:
  - id: spec
    resource: https://example.com/spec
    title: The spec
custom_vendor_key: kept
---

# Retrieval

Ranking uses BM25[^spec].
`

func TestOKFFrontmatterParsesNestedFamilies(t *testing.T) {
	d, err := parseConcept("wiki/systems/retrieval.md", []byte(okfConcept))
	if err != nil {
		t.Fatal(err)
	}
	if d.Type != "Concept" || d.Title != "Retrieval" {
		t.Fatalf("scalars wrong: %+v", d)
	}
	if got := strings.Join(d.Tags, ","); got != "search,okf" {
		t.Fatalf("block-sequence tags = %q", got)
	}
	if d.Generated == nil || d.Generated.By != "process:jot" || d.Generated.At != "2026-08-29T12:00:00Z" {
		t.Fatalf("generated = %+v", d.Generated)
	}
	if len(d.Verified) != 1 || !d.Verified[0].IsHuman() {
		t.Fatalf("verified = %+v", d.Verified)
	}
	if d.Trust != TrustHuman {
		t.Fatalf("trust = %q, want %q", d.Trust, TrustHuman)
	}
	if len(d.Sources) != 1 || d.Sources[0].Resource != "https://example.com/spec" {
		t.Fatalf("sources = %+v", d.Sources)
	}
	if d.Timestamp() != "2026-08-29T12:00:00Z" {
		t.Fatalf("timestamp = %q", d.Timestamp())
	}
	if fmString(d.Frontmatter, "custom_vendor_key") != "kept" {
		t.Fatal("unknown frontmatter keys must round-trip")
	}
	if issues := houseRuleIssues(d); len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
}

func TestInlineAndScalarTagsBothParse(t *testing.T) {
	inline := strings.Replace(okfConcept, "tags:\n  - search\n  - okf", "tags: [search, okf]", 1)
	d, err := parseConcept("wiki/x.md", []byte(inline))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(d.Tags, ",") != "search,okf" {
		t.Fatalf("inline tags = %v", d.Tags)
	}
}

func TestTrustTiersDerive(t *testing.T) {
	cases := []struct {
		verified []Attestation
		want     string
	}{
		{nil, TrustUnverified},
		{[]Attestation{{By: "reference_agent/opus", At: "x"}}, TrustMachine},
		{[]Attestation{{By: "reference_agent/opus"}, {By: "human:s"}}, TrustHuman},
	}
	for _, tc := range cases {
		if got := trustTier(tc.verified); got != tc.want {
			t.Errorf("trustTier(%v) = %q, want %q", tc.verified, got, tc.want)
		}
	}
}

func TestOKFRequiresOnlyType(t *testing.T) {
	minimal := "---\ntype: Note\n---\n\nbody\n"
	if _, err := parseConcept("wiki/min.md", []byte(minimal)); err != nil {
		t.Fatalf("OKF minimum should parse: %v", err)
	}
	if _, err := parseConcept("wiki/none.md", []byte("---\ntitle: x\n---\n\nbody\n")); err == nil {
		t.Fatal("a concept without type must be rejected")
	}
}

func TestOneBadPageDoesNotBreakReads(t *testing.T) {
	root := testVault(t)
	if err := atomicWrite(filepath.Join(root, "wiki", "good.md"), []byte(okfConcept), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(root, "wiki", "bad.md"), []byte("no frontmatter at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs, issues, err := loadConcepts(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != "good" {
		t.Fatalf("healthy page must still load: %+v", docs)
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "bad.md") {
		t.Fatalf("issues = %v", issues)
	}
}

func TestFootnotesMustResolveToSources(t *testing.T) {
	d, err := parseConcept("wiki/x.md", []byte(strings.Replace(okfConcept, "[^spec]", "[^missing]", 1)))
	if err != nil {
		t.Fatal(err)
	}
	issues := footnoteIssues(d)
	if len(issues) != 1 || !strings.Contains(issues[0], "[^missing]") {
		t.Fatalf("issues = %v", issues)
	}
}

func TestPerTopicIndexesAndOKFVersion(t *testing.T) {
	root := testVault(t)
	if err := atomicWrite(filepath.Join(root, "wiki", "systems", "retrieval.md"), []byte(okfConcept), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshDerived(root, false); err != nil {
		t.Fatal(err)
	}
	rootIndex := readFileString(t, filepath.Join(root, "wiki", "index.md"))
	if !strings.Contains(rootIndex, `okf_version: "0.2"`) {
		t.Fatalf("root index lacks okf_version:\n%s", rootIndex)
	}
	topicIndex := readFileString(t, filepath.Join(root, "wiki", "systems", "index.md"))
	if !strings.Contains(topicIndex, "[Retrieval](retrieval.md)") {
		t.Fatalf("topic index wrong:\n%s", topicIndex)
	}
}
