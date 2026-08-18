// Package agent runs one coding agent inside one worktree.
//
// The output contract is the filesystem. Agents are asked to edit files; what
// they printed while doing it is logged and never parsed. Agent stdout is
// chatty, differs per harness and drifts between releases — the working tree
// does not.
package agent

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// Runner executes a prompt in a directory and returns when the agent is done.
type Runner interface {
	Name() string
	Run(ctx context.Context, dir, prompt string) (Result, error)
}

// Result is deliberately thin: what it cost and how long it took. What
// *changed* is read from git afterwards, not from here.
type Result struct {
	Log      string
	ExitCode int
	Wall     time.Duration
}

// Exec runs a real agent CLI. Cmd is the executable name from config
// ("claude", "opencode", ...) and Args are any flags before the prompt.
type Exec struct {
	Cmd     string
	Args    []string
	Timeout time.Duration
}

func (e Exec) Name() string { return e.Cmd }

func (e Exec) Run(ctx context.Context, dir, prompt string) (Result, error) {
	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, e.Cmd, append(e.Args, prompt)...)
	cmd.Dir = dir

	// Close stdin explicitly. Agent CLIs run in "print" mode still block
	// waiting for piped input when it is left open — claude -p stalls ~3s
	// before giving up — and pb always passes the prompt as an argument, so
	// there is never anything to read.
	cmd.Stdin = nil

	// Put the agent in its own process group. Agents start dev servers,
	// file watchers and test runners; killing only the agent orphans them,
	// and they go on holding ports in a worktree we are about to hand to
	// someone else.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Negative PID signals the whole group, not just the leader.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	start := time.Now()
	out, err := cmd.CombinedOutput()
	res := Result{Log: string(out), Wall: time.Since(start)}

	var ee *exec.ExitError
	if err != nil && asExitError(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, nil // a non-zero agent is not a tool error; the gate judges
	}
	return res, err
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}
