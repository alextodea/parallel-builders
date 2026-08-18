package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNoneIsPassthrough(t *testing.T) {
	s := Spec{Mode: None, Worktree: "/tmp"}
	got, cleanup, err := s.Wrap([]string{"echo", "hi"})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "echo hi" {
		t.Errorf("None must not alter the command, got %v", got)
	}
}

func TestUnavailableIsAnErrorNotADowngrade(t *testing.T) {
	// The safety-critical property: asking for a sandbox that is not present
	// must fail loudly, never silently run unprotected.
	var absent Mode = "bwrap"
	if runtime.GOOS == "linux" {
		absent = "seatbelt"
	}
	if Available(absent) {
		t.Skipf("%s unexpectedly available; cannot test the failure path", absent)
	}
	_, _, err := Spec{Mode: absent, Worktree: "/tmp"}.Wrap([]string{"echo", "hi"})
	if err == nil {
		t.Fatal("an unavailable sandbox must return an error, not fall through to running unconfined")
	}
}

func TestResolveNeverPanics(t *testing.T) {
	for _, m := range []Mode{None, Seatbelt, Bwrap, Auto} {
		_ = Resolve(m)
		_ = Available(m)
	}
}

// The real thing: these run on macOS only, because they exercise sandbox-exec.
// They are the tests that actually prove confinement rather than asserting the
// shape of a command line.

func skipUnlessSeatbelt(t *testing.T) {
	t.Helper()
	if !Available(Seatbelt) {
		t.Skip("sandbox-exec not available")
	}
}

func run(t *testing.T, s Spec, script string) (string, error) {
	t.Helper()
	argv, cleanup, err := s.Wrap([]string{"sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	return string(out), err
}

func TestSeatbeltConfinesWrites(t *testing.T) {
	skipUnlessSeatbelt(t)
	work := t.TempDir()
	s := Spec{Mode: Seatbelt, Worktree: work, Network: true}

	// Inside the worktree: allowed.
	if _, err := run(t, s, "echo ok > "+filepath.Join(work, "inside.txt")); err != nil {
		t.Errorf("write inside the worktree should succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "inside.txt")); err != nil {
		t.Error("the in-worktree file was not written")
	}

	// Outside: blocked. t.TempDir() lives under /private/var/folders on
	// macOS, which the profile intentionally allows (the toolchain needs
	// it), so it is NOT a valid "outside" target. Use a real sensitive
	// location — the home directory — and clean up defensively in case
	// confinement failed and the file was actually created.
	home, _ := os.UserHomeDir()
	outside := filepath.Join(home, ".pb-sandbox-leak-test")
	defer os.Remove(outside)
	out, _ := run(t, s, "echo leak > "+outside+" 2>&1; true")
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("a write to the home directory succeeded — confinement failed (%s)", out)
	}
}

func TestSeatbeltDeniesNetworkForGateCommands(t *testing.T) {
	skipUnlessSeatbelt(t)
	if !hasNet(t) {
		t.Skip("no network available to test denial against")
	}
	work := t.TempDir()

	// Network=false is the gate-command profile.
	out, _ := run(t, Spec{Mode: Seatbelt, Worktree: work, Network: false},
		`curl -s --max-time 5 -o /dev/null -w "%{http_code}" https://example.com`)
	if strings.TrimSpace(out) != "000" {
		t.Errorf("gate commands must be denied network; curl reported %q, want 000", strings.TrimSpace(out))
	}
}

func TestSeatbeltAllowsNetworkForAgents(t *testing.T) {
	skipUnlessSeatbelt(t)
	if !hasNet(t) {
		t.Skip("no network")
	}
	work := t.TempDir()

	// Network=true is the agent profile — it must reach its model provider.
	out, _ := run(t, Spec{Mode: Seatbelt, Worktree: work, Network: true},
		`curl -s --max-time 5 -o /dev/null -w "%{http_code}" https://example.com`)
	if strings.TrimSpace(out) == "000" {
		t.Error("agents must keep network access, but it was blocked")
	}
}

func TestSeatbeltLeavesToolchainUsable(t *testing.T) {
	skipUnlessSeatbelt(t)
	work := t.TempDir()
	out, err := run(t, Spec{Mode: Seatbelt, Worktree: work, Network: true}, "go version")
	if err != nil || !strings.Contains(out, "go version") {
		t.Errorf("the go toolchain must still run inside the sandbox: %v\n%s", err, out)
	}
}

func hasNet(t *testing.T) bool {
	t.Helper()
	out, _ := exec.Command("curl", "-s", "--max-time", "5", "-o", "/dev/null",
		"-w", "%{http_code}", "https://example.com").Output()
	return strings.TrimSpace(string(out)) == "200"
}
