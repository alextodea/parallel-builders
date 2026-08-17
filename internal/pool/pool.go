// Package pool hands out reusable git worktrees.
//
// The naive version — create a worktree, run, delete it — is what gives
// worktrees their bad reputation. A fresh worktree starts cold, so every
// builder pays a full dependency install and a cold build before writing a
// line. Recycling them means only the first one ever does.
//
// A worktree is not a clone: they share one object database, so the marginal
// cost is a working copy, not a copy of the history. What actually costs is
// node_modules, target/, virtualenvs — which is exactly what surviving between
// claims is for.
package pool

import (
	"fmt"
	"time"
)

type Worktree struct {
	ID   int
	Path string
}

type Pool struct {
	Root string
	Size int
	Repo string
}

// Claim blocks until a free worktree is available, resets it, and marks it in
// use.
//
// TODO: correctness requirement — two builders must never receive the same
// worktree. A lockfile per worktree holding the owner PID is enough.
func (p *Pool) Claim(timeout time.Duration) (*Worktree, error) {
	return nil, fmt.Errorf("pool.Claim: not implemented")
}

// Release returns a worktree after killing whatever the agent left running.
//
// TODO: agents start dev servers, watchers and test runners that outlive them.
// A worktree handed back while something still holds a port is worse than no
// pool at all. Kill the process group (see internal/agent), then reset tracked
// files hard while preserving dependency directories.
func (p *Pool) Release(w *Worktree) error {
	return fmt.Errorf("pool.Release: not implemented")
}

// Recover reclaims worktrees whose lock has no live owner.
//
// TODO: crashes will happen and you do not want to hand-clean. On startup,
// check each lock's PID and take back the orphans.
func (p *Pool) Recover() error {
	return fmt.Errorf("pool.Recover: not implemented")
}
