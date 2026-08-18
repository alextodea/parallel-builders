// Package pool gives each builder its own reusable git worktree.
//
// Two facts shape everything here.
//
// A worktree is not a clone: all worktrees share one object database, so the
// marginal cost of another is a working copy, not a copy of the history. What
// actually costs is node_modules, target/, .venv — which is why slots are
// reused rather than created and destroyed. Only the first run pays a cold
// dependency install.
//
// And a slot is claimed by a fixed index, not from a queue. Every run needs
// exactly N slots for N builders, so a queue with spares and backoff would be
// machinery serving a pattern that never occurs. Builder i always gets slot i.
package pool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Worktree is one checked-out working copy, held for the life of a run.
type Worktree struct {
	Index int
	Path  string

	lock *os.File
}

// Pool owns a directory of reusable worktrees for one repository.
type Pool struct {
	root string // where slots live; must be outside the repo
	repo string // the repository they are worktrees of
	n    int

	held map[int]*Worktree
}

// DefaultExcludes are the directories preserved across a reset. Anything here
// survives `git clean`, which is the entire reason a slot is worth reusing.
//
// Callers should extend this from config rather than relying on the defaults:
// getting it wrong does not fail, it just makes every run slow.
var DefaultExcludes = []string{
	"node_modules", "vendor", "target", "build", "dist",
	".venv", "venv", ".gradle", ".next", ".turbo", "__pycache__",
}

// Open prepares a pool of n slots. It does not create any worktrees; Get does
// that lazily, so an unused slot costs nothing.
func Open(root, repo string, n int) (*Pool, error) {
	if n < 1 {
		return nil, fmt.Errorf("pool: size %d is not usable", n)
	}

	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	// A pool inside the repository would make worktrees of worktrees, and
	// `git clean` in a slot would start deleting its siblings.
	if rootAbs == repoAbs || strings.HasPrefix(rootAbs+string(filepath.Separator), repoAbs+string(filepath.Separator)) {
		return nil, fmt.Errorf("pool: root %s is inside the repository %s — choose a location outside it", rootAbs, repoAbs)
	}

	if err := os.MkdirAll(rootAbs, 0o755); err != nil {
		return nil, err
	}
	return &Pool{root: rootAbs, repo: repoAbs, n: n, held: map[int]*Worktree{}}, nil
}

// Get locks slot i, creates its worktree if absent, and resets it to commit.
//
// It does not queue. If another pb process holds the slot, that is a fact the
// caller needs immediately — waiting would mean two runs silently interleaving
// on one machine.
func (p *Pool) Get(ctx context.Context, i int, commit string) (*Worktree, error) {
	if i < 0 || i >= p.n {
		return nil, fmt.Errorf("pool: slot %d out of range (size %d)", i, p.n)
	}
	if w, ok := p.held[i]; ok {
		return w, nil
	}
	if commit == "" {
		return nil, fmt.Errorf("pool: slot %d needs an explicit commit — resetting to whatever HEAD is now would let two builders start from different code", i)
	}

	dir := p.slotDir(i)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	lock, err := p.lockSlot(i)
	if err != nil {
		return nil, err
	}

	w := &Worktree{Index: i, Path: filepath.Join(dir, "tree"), lock: lock}
	if err := p.ensureWorktree(ctx, w, commit); err != nil {
		lock.Close()
		return nil, err
	}
	p.held[i] = w
	return w, nil
}

// Release resets the slot and unlocks it. Safe to call twice.
func (p *Pool) Release(i int) error {
	w, ok := p.held[i]
	if !ok {
		return nil // already released, or never held by this process
	}
	delete(p.held, i)

	// Kill anything the agent left running before the reset. An agent that
	// started a dev server leaves it holding a port, and a slot handed back
	// with a live process in it is worse than no pool at all.
	killErr := KillTree(w.Path)

	resetErr := p.reset(context.Background(), w, "")

	if w.lock != nil {
		// Closing the fd releases the flock. The kernel would do this at
		// exit anyway, which is exactly why a crash needs no recovery pass.
		w.lock.Close()
		w.lock = nil
	}

	if killErr != nil {
		return killErr
	}
	return resetErr
}

// ReleaseAll releases every slot this process holds.
func (p *Pool) ReleaseAll() error {
	var first error
	for i := range p.held {
		if err := p.Release(i); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (p *Pool) slotDir(i int) string { return filepath.Join(p.root, fmt.Sprintf("%d", i)) }

// lockSlot takes an exclusive, non-blocking flock.
//
// flock rather than a pid file because the kernel drops it when the holder
// dies — crash recovery, stale locks and pid reuse all stop being problems
// this package has to reason about.
func (p *Pool) lockSlot(i int) (*os.File, error) {
	path := filepath.Join(p.slotDir(i), ".pb-lock")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		owner := strings.TrimSpace(readFile(path))
		f.Close()
		if owner == "" {
			owner = "another pb process"
		}
		return nil, fmt.Errorf("pool: slot %d is in use by %s — wait for it to finish, or use a different pool root", i, owner)
	}

	// Record who holds it, for the error message the *next* process prints.
	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "pid %d since %s", os.Getpid(), time.Now().Format(time.RFC3339))
	f.Sync()
	return f, nil
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// ensureWorktree creates the worktree if missing, repairs it if the directory
// was deleted behind git's back, then resets it.
func (p *Pool) ensureWorktree(ctx context.Context, w *Worktree, commit string) error {
	if _, err := os.Stat(filepath.Join(w.Path, ".git")); err != nil {
		// Either it never existed or someone ran rm -rf. Prune first so git
		// forgets a registration whose directory is gone, then add.
		if _, err := p.git(ctx, p.repo, "worktree", "prune"); err != nil {
			return err
		}
		os.RemoveAll(w.Path)

		// --detach is not optional: git refuses to check out the same branch
		// in two worktrees, which N>1 would hit immediately.
		if out, err := p.git(ctx, p.repo, "worktree", "add", "--detach", w.Path, commit); err != nil {
			return fmt.Errorf("pool: creating slot %d: %w\n%s", w.Index, err, out)
		}
		return nil
	}
	return p.reset(ctx, w, commit)
}

// reset returns a slot to a clean checkout of commit, preserving the
// dependency directories that make reuse worthwhile. Passing an empty commit
// cleans without moving HEAD, which is what Release wants.
func (p *Pool) reset(ctx context.Context, w *Worktree, commit string) error {
	if commit != "" {
		if out, err := p.git(ctx, w.Path, "checkout", "--detach", "--force", commit); err != nil {
			return fmt.Errorf("pool: slot %d checkout %s: %w\n%s", w.Index, commit, err, out)
		}
	}
	if out, err := p.git(ctx, w.Path, "reset", "--hard"); err != nil {
		return fmt.Errorf("pool: slot %d reset: %w\n%s", w.Index, err, out)
	}

	// -fd, never -fdx. The x is what would delete node_modules and turn every
	// run back into a cold install.
	args := []string{"clean", "-fd"}
	for _, e := range DefaultExcludes {
		args = append(args, "-e", e)
	}
	if out, err := p.git(ctx, w.Path, args...); err != nil {
		return fmt.Errorf("pool: slot %d clean: %w\n%s", w.Index, err, out)
	}
	return nil
}

func (p *Pool) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// KillTree terminates processes still running under a worktree.
//
// Best effort by design: it uses lsof to find who holds the directory open,
// signals their process groups, and reports nothing if lsof is unavailable.
// The authoritative kill is the one agent.Exec performs on its own process
// group; this is the safety net for anything that escaped it.
func KillTree(dir string) error {
	out, err := exec.Command("lsof", "-t", "+D", dir).Output()
	if err != nil {
		return nil // no lsof, or nothing open — either way, nothing to do
	}
	self := os.Getpid()
	for _, line := range strings.Fields(string(out)) {
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil || pid == self {
			continue
		}
		pgid, err := syscall.Getpgid(pid)
		if err != nil {
			continue
		}
		syscall.Kill(-pgid, syscall.SIGTERM)
	}
	time.Sleep(150 * time.Millisecond)
	for _, line := range strings.Fields(string(out)) {
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil || pid == self {
			continue
		}
		if pgid, err := syscall.Getpgid(pid); err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}
	return nil
}
