package jot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type vaultLock struct {
	f *os.File
}

func lockVault(root string) (*vaultLock, error) {
	if err := os.MkdirAll(filepath.Join(root, ".jot"), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, ".jot", "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &vaultLock{f: f}, nil
}

func (l *vaultLock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

func command(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return stdout.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return stdout.String(), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

func isGitRepo(ctx context.Context, root string) bool {
	_, err := command(ctx, root, "git", "rev-parse", "--git-dir")
	return err == nil
}

func gitDirty(ctx context.Context, root string) (bool, error) {
	out, err := command(ctx, root, "git", "status", "--porcelain", "--untracked-files=all")
	return strings.TrimSpace(out) != "", err
}

func ensureGitIdentity(ctx context.Context, root string) error {
	if out, err := command(ctx, root, "git", "config", "user.name"); err == nil && strings.TrimSpace(out) != "" {
		if out, err := command(ctx, root, "git", "config", "user.email"); err == nil && strings.TrimSpace(out) != "" {
			return nil
		}
	}
	login, err := command(ctx, root, "gh", "api", "user", "--jq", ".login")
	if err != nil {
		return errors.New("Git user identity is not configured; set git config user.name and user.email")
	}
	login = strings.TrimSpace(login)
	if _, err := command(ctx, root, "git", "config", "user.name", login); err != nil {
		return err
	}
	_, err = command(ctx, root, "git", "config", "user.email", login+"@users.noreply.github.com")
	return err
}

func commitAll(ctx context.Context, root, message string) (bool, error) {
	dirty, err := gitDirty(ctx, root)
	if err != nil || !dirty {
		return false, err
	}
	if err := ensureGitIdentity(ctx, root); err != nil {
		return false, err
	}
	if _, err := command(ctx, root, "git", "add", "--all"); err != nil {
		return false, err
	}
	if _, err := command(ctx, root, "git", "commit", "-m", message); err != nil {
		return false, err
	}
	return true, nil
}

func hasOrigin(ctx context.Context, root string) bool {
	_, err := command(ctx, root, "git", "remote", "get-url", "origin")
	return err == nil
}

func currentRevision(ctx context.Context, root string) string {
	out, err := command(ctx, root, "git", "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func currentBranch(ctx context.Context, root string) string {
	out, err := command(ctx, root, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func hasUpstream(ctx context.Context, root string) bool {
	_, err := command(ctx, root, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	return err == nil
}

func pushWithRetry(ctx context.Context, root string) error {
	if !hasUpstream(ctx, root) {
		branch := currentBranch(ctx, root)
		if branch == "" {
			return errors.New("cannot determine current Git branch")
		}
		_, err := command(ctx, root, "git", "push", "--set-upstream", "origin", branch)
		return err
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := command(ctx, root, "git", "push"); err == nil {
			return nil
		} else {
			last = err
		}
		if attempt == 2 {
			break
		}
		if _, err := command(ctx, root, "git", "pull", "--rebase"); err != nil {
			if rebaseInProgress(root) {
				return codedf(ExitConflict, "Git push race produced conflicts; resolve them and run jot sync --continue")
			}
			return err
		}
	}
	return last
}

func rebaseInProgress(root string) bool {
	gitDir := filepath.Join(root, ".git")
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
			return true
		}
	}
	return false
}

func syncBefore(ctx context.Context, root string, allowAuthorityChange bool) error {
	if os.Getenv("JOT_NO_SYNC") == "1" || !isGitRepo(ctx, root) {
		return nil
	}
	if rebaseInProgress(root) {
		return codedf(ExitConflict, "a Git rebase conflict is in progress; resolve the files and run jot sync --continue")
	}
	dirty, err := gitDirty(ctx, root)
	if err != nil {
		return err
	}
	if dirty {
		if _, err := refreshDerived(root, allowAuthorityChange); err != nil {
			return err
		}
		if _, err := commitAll(ctx, root, "jot: publish local edits"); err != nil {
			return err
		}
	}
	if !hasOrigin(ctx, root) {
		return nil
	}
	if !hasUpstream(ctx, root) {
		if dirty {
			return pushWithRetry(ctx, root)
		}
		return nil
	}
	if _, err := command(ctx, root, "git", "pull", "--rebase"); err != nil {
		if rebaseInProgress(root) {
			return codedf(ExitConflict, "Git synchronization produced conflicts; resolve them and run jot sync --continue")
		}
		return err
	}
	if dirty {
		return pushWithRetry(ctx, root)
	}
	return nil
}

func syncAfter(ctx context.Context, root, message string, allowAuthorityChange bool) error {
	if _, err := refreshDerived(root, allowAuthorityChange); err != nil {
		return err
	}
	if os.Getenv("JOT_NO_SYNC") == "1" || !isGitRepo(ctx, root) {
		return nil
	}
	committed, err := commitAll(ctx, root, message)
	if err != nil {
		return err
	}
	if committed && hasOrigin(ctx, root) {
		if hasUpstream(ctx, root) {
			if _, err := command(ctx, root, "git", "pull", "--rebase"); err != nil {
				if rebaseInProgress(root) {
					return codedf(ExitConflict, "saved locally; Git synchronization produced conflicts—resolve them and run jot sync --continue")
				}
				return fmt.Errorf("saved locally but GitHub pull failed: %w", err)
			}
		}
		if err := pushWithRetry(ctx, root); err != nil {
			return fmt.Errorf("saved locally but GitHub push failed: %w", err)
		}
	}
	return nil
}

func continueSync(ctx context.Context, root string, allowAuthorityChange bool) error {
	if !rebaseInProgress(root) {
		return errors.New("no Git rebase is in progress")
	}
	if _, err := refreshDerived(root, allowAuthorityChange); err != nil {
		return err
	}
	if _, err := command(ctx, root, "git", "add", "--all"); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "rebase", "--continue")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git rebase --continue: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if hasOrigin(ctx, root) {
		return pushWithRetry(ctx, root)
	}
	return nil
}
