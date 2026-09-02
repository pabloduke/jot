package jot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupGitPair(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("JOT_NO_SYNC", "")
	ctx := context.Background()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	if _, err := command(ctx, base, "git", "init", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := command(ctx, first, "git", "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"user.name", "Jot Test"}, {"user.email", "jot@example.test"}} {
		if _, err := command(ctx, first, "git", "config", pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := scaffoldVault(first); err != nil {
		t.Fatal(err)
	}
	if _, err := commitAll(ctx, first, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := command(ctx, first, "git", "remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := command(ctx, first, "git", "push", "-u", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := command(ctx, remote, "git", "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := command(ctx, base, "git", "clone", remote, second); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{first, second} {
		for _, pair := range [][2]string{{"user.name", "Jot Test"}, {"user.email", "jot@example.test"}} {
			if _, err := command(ctx, repo, "git", "config", pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
	}
	return first, second
}

func TestSyncPublishesAndDownloadsChanges(t *testing.T) {
	ctx := context.Background()
	first, second := setupGitPair(t)
	page := filepath.Join(first, "wiki", "work", "retrieval.md")
	if err := atomicWrite(page, []byte(validConcept), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncAfter(ctx, first, "add retrieval", false); err != nil {
		t.Fatal(err)
	}
	if err := syncBefore(ctx, second, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second, "wiki", "work", "retrieval.md")); err != nil {
		t.Fatal(err)
	}
}

func TestFirstPublishEstablishesUpstream(t *testing.T) {
	t.Setenv("JOT_NO_SYNC", "")
	ctx := context.Background()
	base := t.TempDir()
	remote := filepath.Join(base, "empty.git")
	root := filepath.Join(base, "vault")
	if _, err := command(ctx, base, "git", "init", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := command(ctx, root, "git", "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"user.name", "Jot Test"}, {"user.email", "jot@example.test"}} {
		if _, err := command(ctx, root, "git", "config", pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := command(ctx, root, "git", "remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	if err := scaffoldVault(root); err != nil {
		t.Fatal(err)
	}
	if err := syncAfter(ctx, root, "initialize", false); err != nil {
		t.Fatal(err)
	}
	if !hasUpstream(ctx, root) {
		t.Fatal("first publish did not configure an upstream")
	}
}

func TestSyncPushesCleanBranchAheadOfUpstream(t *testing.T) {
	ctx := context.Background()
	first, second := setupGitPair(t)
	page := filepath.Join(first, "wiki", "work", "offline-capture.md")
	if err := atomicWrite(page, []byte(validConcept), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := commitAll(ctx, first, "capture while offline"); err != nil {
		t.Fatal(err)
	}
	dirty, err := gitDirty(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Fatal("expected clean worktree after local commit")
	}
	if err := syncBefore(ctx, first, false); err != nil {
		t.Fatal(err)
	}
	if _, err := command(ctx, second, "git", "pull", "--ff-only"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second, "wiki", "work", "offline-capture.md")); err != nil {
		t.Fatal("clean ahead commit was not pushed:", err)
	}
}

func TestSyncPreservesConflictForResolution(t *testing.T) {
	ctx := context.Background()
	first, second := setupGitPair(t)
	page1 := filepath.Join(first, "wiki", "work", "retrieval.md")
	if err := atomicWrite(page1, []byte(validConcept), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncAfter(ctx, first, "add page", false); err != nil {
		t.Fatal(err)
	}
	if err := syncBefore(ctx, second, false); err != nil {
		t.Fatal(err)
	}
	remoteEdit := strings.Replace(validConcept, "BM25 lexical search", "Remote lexical search", 1)
	if err := atomicWrite(page1, []byte(remoteEdit), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncAfter(ctx, first, "remote edit", false); err != nil {
		t.Fatal(err)
	}
	page2 := filepath.Join(second, "wiki", "work", "retrieval.md")
	localEdit := strings.Replace(validConcept, "BM25 lexical search", "Local lexical search", 1)
	if err := atomicWrite(page2, []byte(localEdit), 0o644); err != nil {
		t.Fatal(err)
	}
	err := syncBefore(ctx, second, false)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected synchronization conflict, got %v", err)
	}
	if !rebaseInProgress(second) {
		t.Fatal("rebase state was not preserved")
	}
}
