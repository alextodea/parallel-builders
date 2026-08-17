package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Fake is a stand-in for a real agent: it waits briefly, then applies a
// prepared patch to the worktree.
//
// Build the whole tool against this. A real agent takes minutes and costs
// money per invocation, so debugging the harness against one gives you maybe
// fifteen attempts in an evening. Against Fake the cycle is two seconds and
// free, which is the difference between finishing this and abandoning it.
type Fake struct {
	ID string

	// Patch is a unified diff applied with `git apply`. Empty means the
	// agent "did nothing" — useful for exercising the failure paths.
	Patch string

	// Files written directly, as path -> contents. Simpler than a patch
	// when you just want a candidate to exist.
	Files map[string]string

	Delay time.Duration
	Exit  int
}

func (f Fake) Name() string {
	if f.ID == "" {
		return "fake"
	}
	return f.ID
}

func (f Fake) Run(ctx context.Context, dir, prompt string) (Result, error) {
	start := time.Now()

	delay := f.Delay
	if delay == 0 {
		delay = 50 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return Result{Wall: time.Since(start)}, ctx.Err()
	case <-time.After(delay):
	}

	for rel, content := range f.Files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Result{}, fmt.Errorf("fake agent %s: %w", f.Name(), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return Result{}, fmt.Errorf("fake agent %s: %w", f.Name(), err)
		}
	}

	if f.Patch != "" {
		cmd := exec.CommandContext(ctx, "git", "apply", "-")
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(f.Patch)
		if out, err := cmd.CombinedOutput(); err != nil {
			return Result{Log: string(out)}, fmt.Errorf("fake agent %s: git apply: %w", f.Name(), err)
		}
	}

	return Result{
		Log:      fmt.Sprintf("fake agent %s handled: %s", f.Name(), prompt),
		ExitCode: f.Exit,
		Wall:     time.Since(start),
	}, nil
}
