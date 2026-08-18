package pool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo builds a throwaway git repo with two commits, and returns its path
// plus both shas. No agent is involved anywhere in this package's tests.
func newRepo(t *testing.T) (dir, first, second string) {
	t.Helper()
	dir = t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	run("add", "-A")
	run("commit", "-qm", "first")
	first = run("rev-parse", "HEAD")

	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two\n"), 0o644)
	run("add", "-A")
	run("commit", "-qm", "second")
	second = run("rev-parse", "HEAD")

	return dir, first, second
}

func TestGetCreatesAndResetsToTheGivenCommit(t *testing.T) {
	repo, first, second := newRepo(t)
	p, err := Open(t.TempDir(), repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer p.ReleaseAll()

	w, err := p.Get(context.Background(), 0, first)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// The commit is explicit, so the slot holds `first` even though the repo
	// is on `second`. Two builders resetting at different moments must still
	// start from identical code.
	if _, err := os.Stat(filepath.Join(w.Path, "a.txt")); err != nil {
		t.Errorf("a.txt should exist at the first commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(w.Path, "b.txt")); !os.IsNotExist(err) {
		t.Errorf("b.txt must NOT exist at the first commit, got err=%v", err)
	}

	if err := p.Release(0); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Same slot, different commit.
	w2, err := p.Get(context.Background(), 0, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w2.Path, "b.txt")); err != nil {
		t.Errorf("b.txt should exist at the second commit: %v", err)
	}
}

func TestReleasePreservesCachesButDiscardsWork(t *testing.T) {
	repo, first, _ := newRepo(t)
	p, err := Open(t.TempDir(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer p.ReleaseAll()

	w, err := p.Get(context.Background(), 0, first)
	if err != nil {
		t.Fatal(err)
	}

	// A dependency directory — the whole reason slots are reused.
	deps := filepath.Join(w.Path, "node_modules", "left-pad")
	os.MkdirAll(deps, 0o755)
	os.WriteFile(filepath.Join(deps, "index.js"), []byte("//"), 0o644)

	// And a candidate's работа, which must not survive.
	os.WriteFile(filepath.Join(w.Path, "scratch.go"), []byte("package x"), 0o644)
	os.WriteFile(filepath.Join(w.Path, "a.txt"), []byte("MODIFIED\n"), 0o644)

	if err := p.Release(0); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := p.Get(context.Background(), 0, first); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(deps, "index.js")); err != nil {
		t.Error("node_modules must survive a release — losing it makes every run a cold install")
	}
	if _, err := os.Stat(filepath.Join(w.Path, "scratch.go")); !os.IsNotExist(err) {
		t.Error("an untracked file from the previous candidate leaked into the next run")
	}
	if b, _ := os.ReadFile(filepath.Join(w.Path, "a.txt")); string(b) != "one\n" {
		t.Errorf("a tracked file was not restored: %q", b)
	}
}

func TestSlotIsExclusiveAcrossProcesses(t *testing.T) {
	// flock is owner-scoped, so a same-process test can pass while the real
	// two-process case fails. This asserts the case that matters by running
	// a second binary.
	repo, first, _ := newRepo(t)
	root := t.TempDir()

	p, err := Open(root, repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), 0, first); err != nil {
		t.Fatal(err)
	}
	defer p.ReleaseAll()

	// A separate process attempting the same flock must fail immediately.
	prog := `
package main

import ("fmt";"os";"syscall")

func main() {
	f, err := os.OpenFile(os.Args[1], os.O_CREATE|os.O_RDWR, 0644)
	if err != nil { fmt.Println("open-error"); return }
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Println("locked"); return
	}
	fmt.Println("acquired")
}`
	src := filepath.Join(t.TempDir(), "probe.go")
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(root, "0", ".pb-lock")
	out, err := exec.Command("go", "run", src, lockPath).CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "locked" {
		t.Fatalf("a second process got the slot (%q) — two runs would interleave on one worktree", got)
	}

	// And the error a caller sees should name the holder, not just say busy.
	p2, _ := Open(root, repo, 1)
	_, err = p2.Get(context.Background(), 0, first)
	if err == nil {
		t.Fatal("expected Get to fail on a held slot")
	}
	if !strings.Contains(err.Error(), "pid") {
		t.Errorf("error should identify the holder, got: %v", err)
	}
}

func TestReleaseIsIdempotentAndSurvivesDeletedWorktree(t *testing.T) {
	repo, first, _ := newRepo(t)
	p, err := Open(t.TempDir(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}

	w, err := p.Get(context.Background(), 0, first)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Release(0); err != nil {
		t.Fatal(err)
	}
	if err := p.Release(0); err != nil {
		t.Errorf("second Release should be a no-op, got %v", err)
	}

	// Someone ran rm -rf on the slot. Get must repair rather than fail.
	os.RemoveAll(w.Path)
	if _, err := p.Get(context.Background(), 0, first); err != nil {
		t.Fatalf("Get should recreate a deleted worktree: %v", err)
	}
}

func TestOpenRejectsPoolInsideRepo(t *testing.T) {
	repo, _, _ := newRepo(t)

	for _, inside := range []string{repo, filepath.Join(repo, ".pb", "pool")} {
		if _, err := Open(inside, repo, 1); err == nil {
			t.Errorf("Open(%s) should fail: worktrees of worktrees, and git clean would eat the siblings", inside)
		} else if !strings.Contains(err.Error(), "inside the repository") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func TestGetRefusesEmptyCommit(t *testing.T) {
	repo, _, _ := newRepo(t)
	p, err := Open(t.TempDir(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Get(context.Background(), 0, "")
	if err == nil || !strings.Contains(err.Error(), "explicit commit") {
		t.Fatalf("an empty commit must be refused, got %v", err)
	}
}

func TestGetRejectsOutOfRangeSlot(t *testing.T) {
	repo, first, _ := newRepo(t)
	p, _ := Open(t.TempDir(), repo, 2)
	for _, i := range []int{-1, 2, 99} {
		if _, err := p.Get(context.Background(), i, first); err == nil {
			t.Errorf("slot %d should be out of range", i)
		}
	}
}
