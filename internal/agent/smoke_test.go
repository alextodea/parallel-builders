package agent

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSmokeRealAgent verifies the one assumption unit tests cannot: that a real
// agent CLI, launched headlessly in a directory, edits files and exits.
//
// It is skipped unless PB_SMOKE=1, because it spends money and needs an
// authenticated agent. To run it:
//
//	PB_SMOKE_DIR=/path/to/a/git/repo/with/a/failing/test \
//	PB_SMOKE=1 go test ./internal/agent/ -run Smoke -v
//
// The default command is `claude -p --dangerously-skip-permissions`; override
// with PB_SMOKE_CMD / PB_SMOKE_ARGS (space-separated).
func TestSmokeRealAgent(t *testing.T) {
	if os.Getenv("PB_SMOKE") == "" {
		t.Skip("set PB_SMOKE=1 to run the real-agent smoke test (spends money)")
	}
	dir := os.Getenv("PB_SMOKE_DIR")
	if dir == "" {
		t.Fatal("PB_SMOKE_DIR must point at a git repo with a failing test")
	}

	cmd := envOr("PB_SMOKE_CMD", "claude")
	args := []string{"-p", "--dangerously-skip-permissions"}
	if a := os.Getenv("PB_SMOKE_ARGS"); a != "" {
		args = splitFields(a)
	}

	e := Exec{Cmd: cmd, Args: args, Timeout: 90 * time.Second}
	start := time.Now()
	res, err := e.Run(context.Background(), dir,
		"Make the failing test pass by adding the missing function in a new .go file. Do not edit any _test.go file.")

	t.Logf("elapsed=%s exit=%d", time.Since(start).Round(time.Millisecond), res.ExitCode)
	if err != nil {
		t.Fatalf("agent run failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("agent exited %d — check the command and auth", res.ExitCode)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func splitFields(s string) []string {
	var out []string
	for _, f := range []byte(s) {
		_ = f
	}
	// simple whitespace split without importing strings into a test-only file
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
