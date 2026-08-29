package jot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMessageShorthandAndPendingJSON(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"-m", "Here is a session summary"}, &out, &errOut); err != nil {
		t.Fatalf("add: %v (%s)", err, errOut.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"pending", "--json"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Captures []Capture `json:"captures"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Captures) != 1 || result.Captures[0].Title != "Here is a session summary" {
		t.Fatalf("unexpected pending output: %s", out.String())
	}
}

func TestURLStubCapture(t *testing.T) {
	root := testVault(t)
	t.Setenv("JOT_DIR", root)
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"add", "--url", "https://example.com/article", "--json"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	var c Capture
	if err := json.Unmarshal(out.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("pending retrieval")) {
		t.Fatalf("URL capture did not create pending stub: %s", b)
	}
}
