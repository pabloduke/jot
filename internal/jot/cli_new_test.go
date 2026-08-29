package jot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI executes one command with optional stdin and returns stdout.
func runCLI(t *testing.T, stdin []byte, args ...string) string {
	t.Helper()
	if stdin != nil {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(stdin); err != nil {
			t.Fatal(err)
		}
		_ = w.Close()
		original := os.Stdin
		os.Stdin = r
		defer func() { os.Stdin = original; _ = r.Close() }()
	}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), args, &out, &errOut); err != nil {
		t.Fatalf("jot %s: %v (%s)", strings.Join(args, " "), err, errOut.String())
	}
	return out.String()
}

func runCLIErr(t *testing.T, stdin []byte, args ...string) error {
	t.Helper()
	if stdin != nil {
		r, w, _ := os.Pipe()
		_, _ = w.Write(stdin)
		_ = w.Close()
		original := os.Stdin
		os.Stdin = r
		defer func() { os.Stdin = original; _ = r.Close() }()
	}
	var out, errOut bytes.Buffer
	return Run(context.Background(), args, &out, &errOut)
}

func contextHits(t *testing.T, out string) []Chunk {
	t.Helper()
	var result struct {
		Hits []Chunk `json:"hits"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("context output is not JSON: %v\n%s", err, out)
	}
	return result.Hits
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	tx := ApplyRequest{
		Summary: "add a concept",
		Upserts: []Upsert{{ID: "systems/retrieval", Content: okfConcept}},
	}
	payload, _ := json.Marshal(tx)
	out := runCLI(t, payload, "apply", "--stdin", "--dry-run", "--json")
	var result ApplyResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(result.Changed) != 1 {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "systems", "retrieval.md")); !os.IsNotExist(err) {
		t.Fatal("dry run must not write the page")
	}

	// The same transaction without --dry-run does write.
	runCLI(t, payload, "apply", "--stdin", "--json")
	if _, err := os.Stat(filepath.Join(root, "wiki", "systems", "retrieval.md")); err != nil {
		t.Fatalf("real apply did not write: %v", err)
	}
}

func TestApplyDryRunStillRejectsInvalidContent(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	tx := ApplyRequest{Summary: "bad", Upserts: []Upsert{{ID: "x", Content: "no frontmatter"}}}
	payload, _ := json.Marshal(tx)
	if err := runCLIErr(t, payload, "apply", "--stdin", "--dry-run"); err == nil {
		t.Fatal("dry run must still validate")
	}
}

func TestCapturesReopenAndLog(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	runCLI(t, nil, "-m", "a session summary")

	out := runCLI(t, nil, "pending", "--json")
	var pending struct {
		Captures []Capture `json:"captures"`
	}
	if err := json.Unmarshal([]byte(out), &pending); err != nil {
		t.Fatal(err)
	}
	id := pending.Captures[0].ID

	runCLI(t, nil, "publish", "--capture", id, "--summary", "compiled the summary")

	out = runCLI(t, nil, "captures", "--status", "compiled", "--json")
	if !strings.Contains(out, id) {
		t.Fatalf("compiled capture missing: %s", out)
	}
	if out := runCLI(t, nil, "pending", "--json"); strings.Contains(out, id) {
		t.Fatal("compiled capture should not be pending")
	}

	out = runCLI(t, nil, "log", "--json")
	var logResult struct {
		Entries []LogEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &logResult); err != nil {
		t.Fatal(err)
	}
	if len(logResult.Entries) == 0 || logResult.Entries[0].Capture != id {
		t.Fatalf("log did not record the ingest: %+v", logResult.Entries)
	}

	runCLI(t, nil, "reopen", id)
	if out := runCLI(t, nil, "pending", "--json"); !strings.Contains(out, id) {
		t.Fatal("reopen should return the capture to pending")
	}
}

func TestContextFiltersAndIncludeRaw(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	writeConcept(t, root, "work/releases", "Releases", longBody)
	writeConcept(t, root, "food/bread", "Bread", "Feed the sourdough starter with flour and water every morning without fail.")
	if _, err := addCapture(root, "Raw note", "message", "", "An uncompiled note about kubernetes ingress controllers.", nil); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t, nil, "context", "--json", "--path", "work", "deployment release")
	if !strings.Contains(out, "work/releases") || strings.Contains(out, "food/bread") {
		t.Fatalf("--path filter failed: %s", out)
	}

	out = runCLI(t, nil, "context", "--json", "--type", "Nonexistent", "deployment")
	var typed struct {
		Hits []Chunk `json:"hits"`
	}
	if err := json.Unmarshal([]byte(out), &typed); err != nil {
		t.Fatal(err)
	}
	if len(typed.Hits) != 0 {
		t.Fatalf("--type filter should have excluded everything: %s", out)
	}

	// The query itself echoes in the JSON, so assert on hits rather than text.
	if hits := contextHits(t, runCLI(t, nil, "context", "--json", "kubernetes ingress")); len(hits) != 0 {
		t.Fatalf("raw captures must not be searched by default: %+v", hits)
	}
	hits := contextHits(t, runCLI(t, nil, "context", "--json", "--include-raw", "kubernetes ingress"))
	if len(hits) == 0 {
		t.Fatal("--include-raw should reach the capture")
	}
	if !hits[0].Raw {
		t.Fatalf("expected a raw capture hit, got %+v", hits[0])
	}
}

func TestContextFullReturnsWholePages(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	writeConcept(t, root, "work/releases", "Releases", longBody)
	out := runCLI(t, nil, "context", "--json", "--full", "deployment release")
	if !strings.Contains(out, "release manager signs off") {
		t.Fatalf("--full should return the whole body: %s", out)
	}
}

func TestBacklinksCommand(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	writeConcept(t, root, "a/target", "Target", longBody)
	writeConcept(t, root, "a/source", "Source", "See [[a/target]] and [Target](target.md) for detail. "+longBody)

	out := runCLI(t, nil, "backlinks", "--json", "a/target")
	var result struct {
		Backlinks []string `json:"backlinks"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Backlinks) != 1 || result.Backlinks[0] != "a/source" {
		t.Fatalf("backlinks = %v", result.Backlinks)
	}
}

func TestPromoteCreatesAnswerPageWithSources(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	writeConcept(t, root, "work/releases", "Releases", longBody)

	out := runCLI(t, nil, "context", "--json", "deployment release")
	var ctxResult struct {
		QueryID string `json:"query_id"`
	}
	if err := json.Unmarshal([]byte(out), &ctxResult); err != nil {
		t.Fatal(err)
	}
	if ctxResult.QueryID == "" {
		t.Fatal("context should journal the query")
	}

	runCLI(t, []byte("Releases ship on Tuesdays."),
		"promote", "--stdin", "--id", "answers/release-day", "--query-id", ctxResult.QueryID)

	page := readFileString(t, filepath.Join(root, "wiki", "answers", "release-day.md"))
	if !strings.Contains(page, "type: Answer") {
		t.Fatalf("promoted page is not an Answer:\n%s", page)
	}
	if !strings.Contains(page, "work/releases") {
		t.Fatalf("promoted page should cite its supporting concepts:\n%s", page)
	}
	if !strings.Contains(page, "Releases ship on Tuesdays.") {
		t.Fatalf("promoted page lost the answer body:\n%s", page)
	}
}

func TestLintFixRegeneratesIndexes(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	writeConcept(t, root, "systems/retrieval", "Retrieval", longBody)
	if err := atomicWrite(filepath.Join(root, "wiki", "index.md"), []byte("# stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, nil, "lint", "--fix")
	if index := readFileString(t, filepath.Join(root, "wiki", "index.md")); !strings.Contains(index, "Retrieval") {
		t.Fatalf("lint --fix did not regenerate the index:\n%s", index)
	}
}

func TestLintExitCodeSignalsFindings(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	if err := atomicWrite(filepath.Join(root, "wiki", "broken.md"), []byte("no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runCLIErr(t, nil, "lint")
	if err == nil {
		t.Fatal("expected lint to fail")
	}
	if code := ExitCode(err); code != ExitLintIssues {
		t.Fatalf("exit code = %d, want %d", code, ExitLintIssues)
	}
}

func TestNotInitializedExitCode(t *testing.T) {
	t.Setenv("JOT_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("JOT_DIR", "")
	err := runCLIErr(t, nil, "status")
	if code := ExitCode(err); code != ExitNotInited {
		t.Fatalf("exit code = %d, want %d (err=%v)", code, ExitNotInited, err)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	err := runCLIErr(t, nil, "frobnicate")
	if code := ExitCode(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestVersionAndCompletion(t *testing.T) {
	if out := runCLI(t, nil, "version"); !strings.HasPrefix(out, "jot ") {
		t.Fatalf("version = %q", out)
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		if out := runCLI(t, nil, "completion", shell); !strings.Contains(out, "jot") {
			t.Errorf("%s completion looks empty", shell)
		}
	}
	if err := runCLIErr(t, nil, "completion", "tcsh"); err == nil {
		t.Error("unsupported shell should fail")
	}
}

func TestExportAndImportRoundTrip(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	writeConcept(t, root, "systems/retrieval", "Retrieval", longBody)
	if _, err := refreshDerived(root, false); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "bundle")
	runCLI(t, nil, "export", dest)
	if index := readFileString(t, filepath.Join(dest, "index.md")); !strings.Contains(index, `okf_version: "0.2"`) {
		t.Fatalf("exported bundle root index is not OKF:\n%s", index)
	}
	if _, err := os.Stat(filepath.Join(dest, "systems", "retrieval.md")); err != nil {
		t.Fatalf("concept missing from export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".jot")); !os.IsNotExist(err) {
		t.Fatal("export must not include .jot state")
	}
	if _, err := os.Stat(filepath.Join(dest, "raw")); !os.IsNotExist(err) {
		t.Fatal("export must not include raw captures")
	}

	other := testVault(t)
	t.Setenv("JOT_DIR", other)
	runCLI(t, nil, "import", "--prefix", "imported", dest)
	if _, err := os.Stat(filepath.Join(other, "wiki", "imported", "systems", "retrieval.md")); err != nil {
		t.Fatalf("import did not land the concept: %v", err)
	}
}

func TestExportRefusesNonEmptyDestination(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "existing"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLIErr(t, nil, "export", dest); err == nil {
		t.Fatal("export into a non-empty directory must fail")
	}
}
