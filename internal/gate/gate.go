// Package gate runs the deterministic checks that decide whether a candidate
// passed. No model is involved, which is what makes it free, repeatable, and
// trustworthy enough to select on.
package gate

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Step is one command in the gate, run in the candidate's worktree.
type Step struct {
	Name string // "build" | "lint" | "test" | "arch"
	Cmd  string // shell command from config
}

// Report is the outcome for one candidate.
type Report struct {
	Passed   bool
	FailedAt string // the Step.Name that failed, empty if all passed
	Output   string
	Wall     time.Duration
}

// WrapFunc optionally confines a command. It takes the argv and returns a
// possibly-wrapped argv plus a cleanup to run afterwards. nil means no
// confinement. Gate commands run untrusted repository code, so in the pipeline
// this is always the no-network sandbox.
type WrapFunc func(argv []string) (wrapped []string, cleanup func(), err error)

// Run executes steps in order and stops at the first failure. Order matters:
// cheapest check first, so a candidate that will not compile never costs you a
// test-suite run.
func Run(ctx context.Context, dir string, steps []Step, wrap WrapFunc) Report {
	start := time.Now()

	for _, s := range steps {
		if strings.TrimSpace(s.Cmd) == "" {
			continue
		}
		argv := []string{"sh", "-c", s.Cmd}
		if wrap != nil {
			wrapped, cleanup, err := wrap(argv)
			if err != nil {
				return Report{FailedAt: s.Name, Output: "sandbox: " + err.Error(), Wall: time.Since(start)}
			}
			argv = wrapped
			defer cleanup()
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return Report{
				FailedAt: s.Name,
				Output:   string(out),
				Wall:     time.Since(start),
			}
		}
	}

	return Report{Passed: true, Wall: time.Since(start)}
}
