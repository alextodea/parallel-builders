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

// Run executes steps in order and stops at the first failure. Order matters:
// cheapest check first, so a candidate that will not compile never costs you a
// test-suite run.
func Run(ctx context.Context, dir string, steps []Step) Report {
	start := time.Now()

	for _, s := range steps {
		if strings.TrimSpace(s.Cmd) == "" {
			continue
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", s.Cmd)
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

// TODO: parse failing test names out of the test step's output so
// escalate.Decide can compare failure sets. This is per-language and is the
// one place the tool has to know something about the toolchain — start with
// `go test` output, add others behind an interface.
func FailingTests(output string) []string { return nil }
