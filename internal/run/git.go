package run

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// git helpers. pb shells out rather than using a git library, so its behaviour
// never diverges from what you would see debugging by hand.

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func head(ctx context.Context, dir string) (string, error) {
	return git(ctx, dir, "rev-parse", "HEAD")
}

// stageAll stages everything so the working tree can be diffed as a unit.
// Safe because it only ever runs inside a disposable worktree.
func stageAll(ctx context.Context, dir string) error {
	_, err := git(ctx, dir, "add", "-A")
	return err
}

// changedFiles returns paths touched relative to the worktree's HEAD.
func changedFiles(ctx context.Context, dir string) ([]string, error) {
	if err := stageAll(ctx, dir); err != nil {
		return nil, err
	}
	out, err := git(ctx, dir, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// diffStat counts added+removed lines and files touched.
func diffStat(ctx context.Context, dir string) (lines, files int, err error) {
	if err := stageAll(ctx, dir); err != nil {
		return 0, 0, err
	}
	out, err := git(ctx, dir, "diff", "--cached", "--numstat")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		files++
		// "-" appears for binary files; count them as touched but not sized.
		add, _ := strconv.Atoi(f[0])
		del, _ := strconv.Atoi(f[1])
		lines += add + del
	}
	return lines, files, nil
}

// unifiedDiff is what a repair round hands back to the builder that produced
// it. Handing back the diff is the difference between a patch and a rewrite.
func unifiedDiff(ctx context.Context, dir string) (string, error) {
	if err := stageAll(ctx, dir); err != nil {
		return "", err
	}
	return git(ctx, dir, "diff", "--cached")
}

// commitAll commits the worktree and returns the new sha.
func commitAll(ctx context.Context, dir, message string) (string, error) {
	if err := stageAll(ctx, dir); err != nil {
		return "", err
	}
	// Identity is set per-invocation so pb never depends on, or mutates,
	// the user's global git config.
	args := []string{
		"-c", "user.name=parallel-builders",
		"-c", "user.email=pb@localhost",
		"commit", "--no-verify", "-q", "-m", message,
	}
	if _, err := git(ctx, dir, args...); err != nil {
		return "", err
	}
	return head(ctx, dir)
}

// depsAdded reports how many dependency-manifest lines the change added, as a
// cheap proxy for "did this pull in a new package".
func depsAdded(ctx context.Context, dir string) (int, error) {
	if err := stageAll(ctx, dir); err != nil {
		return 0, err
	}
	out, err := git(ctx, dir, "diff", "--cached", "--",
		"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "requirements.txt")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			if t := strings.TrimSpace(strings.TrimPrefix(line, "+")); t != "" &&
				!strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "#") {
				n++
			}
		}
	}
	return n, nil
}
