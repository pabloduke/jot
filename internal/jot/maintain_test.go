package jot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeConcept(t *testing.T, root, id, title, body string) {
	t.Helper()
	page := "---\ntype: Concept\ntitle: " + title +
		"\ndescription: About " + title +
		"\ngenerated:\n  by: process:jot\n  at: 2026-08-01T00:00:00Z\n---\n\n# " + title + "\n\n" + body + "\n"
	if err := atomicWrite(filepath.Join(root, "wiki", filepath.FromSlash(id)+".md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
}

const longBody = "Deployment happens on Tuesday after the integration suite passes and the release manager signs off on the changelog and every reviewer has approved the staged artifacts in the pipeline."

func TestScanFindsStructuralProblems(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "work/releases", "Releases", longBody)
	writeConcept(t, root, "work/thin", "Thin", "Short.")

	report, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, f := range report.Findings {
		kinds[f.Kind]++
	}
	if kinds[KindOrphan] < 2 {
		t.Errorf("expected both pages to be orphans, got %v", kinds)
	}
	if kinds[KindThinPage] < 1 {
		t.Errorf("expected a thin-page finding, got %v", kinds)
	}
	if report.Scanned != 2 {
		t.Errorf("scanned = %d", report.Scanned)
	}
}

func TestScanKeepsDistinctBrokenLinkFindings(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "work/one", "One", longBody+"\n\n[[missing-one]]")
	writeConcept(t, root, "work/two", "Two", longBody+"\n\n[[missing-two]]")

	report, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range report.Findings {
		if f.Kind == KindBrokenLink {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("broken-link findings = %d, want 2: %+v", count, report.Findings)
	}
}

func TestValidatePublicURLRejectsPrivateAddresses(t *testing.T) {
	ctx := context.Background()
	for _, raw := range []string{
		"http://127.0.0.1/", "http://192.168.1.2/", "http://169.254.169.254/",
		"http://100.64.0.1/", "http://[::1]/", "http://localhost/", "ftp://example.com/",
	} {
		if err := validatePublicURL(ctx, raw); err == nil {
			t.Errorf("validatePublicURL(%q) succeeded", raw)
		}
	}
	if err := validatePublicURL(ctx, "https://8.8.8.8/"); err != nil {
		t.Fatalf("public address was rejected: %v", err)
	}
}

func TestScanDetectsStaleAndDeprecated(t *testing.T) {
	root := testVault(t)
	page := "---\ntype: Concept\ntitle: Old\ndescription: Old page\nstatus: deprecated\n" +
		"stale_after: 2000-01-01T00:00:00Z\ngenerated:\n  by: process:jot\n  at: 2026-08-01T00:00:00Z\n---\n\n# Old\n\n" + longBody + "\n"
	if err := atomicWrite(filepath.Join(root, "wiki", "old.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var stale, deprecated bool
	for _, f := range report.Findings {
		switch f.Kind {
		case KindStale:
			stale = true
		case KindDeprecated:
			deprecated = true
		}
	}
	if !stale || !deprecated {
		t.Fatalf("stale=%v deprecated=%v findings=%+v", stale, deprecated, report.Findings)
	}
}

func TestNearDuplicateQueuesModelWorkAndVerdictCacheSuppressesRepeat(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/one", "Retrieval One", longBody)
	writeConcept(t, root, "a/two", "Retrieval Two", longBody)

	report, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var target *Finding
	for _, f := range report.Findings {
		if f.NeedsModel {
			target = f
			break
		}
	}
	if target == nil {
		t.Fatalf("expected a model-tier finding, got %+v", report.Findings)
	}

	// Drain marks it dispatched and produces excerpts, never page edits.
	batch, err := drainWorkOrders(context.Background(), root, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Orders) == 0 {
		t.Fatal("drain produced no work orders")
	}
	var order *WorkOrder
	for i := range batch.Orders {
		if batch.Orders[i].FindingID == target.ID {
			order = &batch.Orders[i]
		}
	}
	if order == nil {
		t.Fatalf("target finding not drained: %+v", batch.Orders)
	}
	if len(order.Passages) == 0 || order.Question == "" || len(order.Answers) == 0 {
		t.Fatalf("work order is incomplete: %+v", order)
	}

	verdict := order.Answers[0]
	if _, err := resolveFindings(root, []Resolution{{FindingID: target.ID, Verdict: verdict, By: "human:test"}}); err != nil {
		t.Fatal(err)
	}

	// Re-scanning unchanged content opens no new work: unchanged pages are
	// never re-paired, so the question is not reconsidered. Already-queued
	// items are preserved (see TestIdempotentRescanPreservesQueuedWork).
	second, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Opened != 0 {
		t.Fatalf("unchanged rescan opened %d new findings", second.Opened)
	}
	for _, f := range second.Findings {
		if f.ID == target.ID && (f.Status == StatusOpen || f.Status == StatusDispatched) {
			t.Fatalf("resolved finding was re-opened despite unchanged content: %+v", f)
		}
	}

	// Editing a page re-opens the judgement, because the content it was made
	// about has changed.
	original := readFileString(t, filepath.Join(root, "wiki", "a", "one.md"))
	writeConcept(t, root, "a/one", "Retrieval One", longBody+" Additionally the staging environment mirrors production exactly.")
	third, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if third.ModelQueued == 0 {
		t.Fatal("an edited page should re-queue its pair for judgement")
	}

	// Reverting to previously judged content hits the verdict cache instead of
	// asking again. This is the property that makes steady-state cost track
	// churn rather than corpus size.
	if err := atomicWrite(filepath.Join(root, "wiki", "a", "one.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	fourth, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fourth.CacheHits == 0 {
		t.Error("expected the verdict cache to suppress a repeat question")
	}
	for _, f := range fourth.Findings {
		if f.ID == target.ID && f.Status == StatusOpen {
			t.Fatalf("cached verdict should have kept this closed: %+v", f)
		}
	}
}

func TestResolveRejectsUnknownVerdict(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/one", "One", longBody)
	writeConcept(t, root, "a/two", "Two", longBody)
	report, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if !f.NeedsModel {
			continue
		}
		if _, err := resolveFindings(root, []Resolution{{FindingID: f.ID, Verdict: "banana"}}); err == nil {
			t.Fatal("expected an invalid verdict to be rejected")
		}
		return
	}
	t.Skip("no model-tier finding produced")
}

func TestMaintainNeverWritesToTheWiki(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/one", "One", longBody)
	writeConcept(t, root, "a/two", "Two", longBody)
	before := readFileString(t, filepath.Join(root, "wiki", "a", "one.md"))

	if _, err := scanVault(root, maintainOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := drainWorkOrders(context.Background(), root, 10, ""); err != nil {
		t.Fatal(err)
	}
	if after := readFileString(t, filepath.Join(root, "wiki", "a", "one.md")); after != before {
		t.Fatal("maintain must never modify wiki pages")
	}
}

func TestMaintainCLIRoundTrip(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	writeConcept(t, root, "a/one", "One", longBody)
	writeConcept(t, root, "a/two", "Two", longBody)

	out := runCLI(t, nil, "maintain", "--drain", "5", "--json")
	var batch WorkOrderBatch
	if err := json.Unmarshal([]byte(out), &batch); err != nil {
		t.Fatalf("drain output is not JSON: %v\n%s", err, out)
	}
	if len(batch.Orders) == 0 {
		t.Fatalf("no orders drained: %s", out)
	}
	if !strings.Contains(batch.Contract, "jot maintain --resolve") {
		t.Errorf("contract should tell the harness how to reply: %q", batch.Contract)
	}

	resolutions := []Resolution{{FindingID: batch.Orders[0].FindingID, Verdict: batch.Orders[0].Answers[0]}}
	payload, _ := json.Marshal(resolutions)
	out = runCLI(t, payload, "maintain", "--resolve", "--stdin", "--json")
	var report ResolveReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("resolve output is not JSON: %v\n%s", err, out)
	}
	if report.Applied != 1 {
		t.Fatalf("applied = %d", report.Applied)
	}
}

// A rescan that finds nothing new must not destroy work that is already
// queued. Model-tier findings are seeded only by changed pages, so an
// idempotent rescan cannot re-derive them; dropping them would empty the queue
// before it could ever be drained.
func TestIdempotentRescanPreservesQueuedWork(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/one", "One", longBody)
	writeConcept(t, root, "a/two", "Two", longBody)

	first, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.ModelQueued == 0 {
		t.Fatal("expected model-tier work after the first scan")
	}

	second, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.ModelQueued != first.ModelQueued {
		t.Fatalf("rescan changed queued model work from %d to %d", first.ModelQueued, second.ModelQueued)
	}

	batch, err := drainWorkOrders(context.Background(), root, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Orders) != first.ModelQueued {
		t.Fatalf("drained %d orders, expected %d", len(batch.Orders), first.ModelQueued)
	}
}

func TestEditedPairRefreshesOneStableWorkOrder(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/one", "One", longBody)
	writeConcept(t, root, "a/two", "Two", longBody)

	first, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var pair *Finding
	for _, f := range first.Findings {
		if f.NeedsModel && len(f.Concepts) == 2 {
			pair = f
			break
		}
	}
	if pair == nil {
		t.Fatalf("expected pair work, got %+v", first.Findings)
	}
	if _, err := drainWorkOrders(context.Background(), root, 10, ""); err != nil {
		t.Fatal(err)
	}

	writeConcept(t, root, "a/one", "One", longBody+" One small clarification keeps this page current.")
	second, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range second.Findings {
		if f.Kind == pair.Kind && sameStrings(f.Concepts, pair.Concepts) {
			count++
			if f.ID != pair.ID {
				t.Errorf("edited pair changed finding ID from %s to %s", pair.ID, f.ID)
			}
			if f.Status != StatusOpen {
				t.Errorf("refreshed dispatched finding status = %s, want open", f.Status)
			}
		}
	}
	if count != 1 {
		t.Fatalf("edited pair produced %d work orders, want 1: %+v", count, second.Findings)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A finding whose page has been deleted is dropped rather than kept forever.
func TestDeletedPageClosesItsFindings(t *testing.T) {
	root := testVault(t)
	writeConcept(t, root, "a/one", "One", longBody)
	writeConcept(t, root, "a/two", "Two", longBody)
	if _, err := scanVault(root, maintainOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "wiki", "a", "two.md")); err != nil {
		t.Fatal(err)
	}
	report, err := scanVault(root, maintainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		for _, id := range f.Concepts {
			if id == "a/two" {
				t.Fatalf("finding still references a deleted page: %+v", f)
			}
		}
	}
}
