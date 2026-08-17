package setup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Probe reports whether an agent is actually runnable as configured.
//
// There are two things that can be wrong and they need different checks. A
// missing binary is free to detect. A model name that is misspelled, retired,
// or outside your plan cannot be detected by inspection at all — pb has no
// list of your models, and any list it hardcoded would be wrong within weeks.
// The only way to know is to run the thing once.
//
// Doing that at setup costs a fraction of a cent. Not doing it means finding
// out several minutes into a run, after worktrees have been claimed and the
// other builders have already been paid for.
type Probe struct {
	Label string
	Cmd   string
	Args  []string

	OnPath  bool
	Ran     bool // a live invocation was attempted
	Err     error
	Output  string
	Elapsed time.Duration
}

func (p Probe) OK() bool { return p.OnPath && (!p.Ran || p.Err == nil) }

func (p Probe) Status() string {
	switch {
	case !p.OnPath:
		return "not on PATH"
	case p.Ran && p.Err != nil:
		return "flags rejected"
	case p.Ran:
		return "ok"
	default:
		return "on PATH (flags unverified)"
	}
}

// CheckPath is the free half: does the executable resolve?
func CheckPath(label, cmd string, args []string) Probe {
	_, err := exec.LookPath(cmd)
	return Probe{Label: label, Cmd: cmd, Args: args, OnPath: err == nil}
}

// CheckLive additionally runs the agent with its configured flags and a
// throwaway prompt. This is what catches a bad model name.
//
// A non-zero exit is treated as failure because that is how every CLI reports
// "I do not know that model" — pb deliberately does not try to interpret the
// message, since each agent words it differently.
func CheckLive(label, cmd string, args []string, timeout time.Duration) Probe {
	p := CheckPath(label, cmd, args)
	if !p.OnPath {
		return p
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := exec.CommandContext(ctx, cmd, append(append([]string{}, args...), probePrompt)...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }

	start := time.Now()
	out, err := c.CombinedOutput()

	p.Ran = true
	p.Elapsed = time.Since(start)
	p.Output = tail(string(out), 400)

	switch {
	case ctx.Err() == context.DeadlineExceeded:
		p.Err = fmt.Errorf("timed out after %s", timeout)
	case err != nil:
		p.Err = err
	}
	return p
}

// probePrompt is deliberately trivial: the point is whether the agent starts
// and accepts its flags, not what it says.
const probePrompt = "Reply with the single word OK."

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
