package run

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"

	"github.com/alextodea/parallel-builders/internal/gate"
	"github.com/alextodea/parallel-builders/internal/sandbox"
)

// wrapFunc confines a command; see sandbox.Spec.Wrap.
type wrapFunc func(argv []string) (wrapped []string, cleanup func(), err error)

// gateWrap builds the no-network confinement for gate and test commands run in
// dir. A None mode returns nil, meaning unconfined. An unavailable mode is a
// hard error surfaced at command time, never a silent downgrade.
func gateWrap(mode sandbox.Mode, dir string) wrapFunc {
	if sandbox.Resolve(mode) == sandbox.None {
		return nil
	}
	return func(argv []string) ([]string, func(), error) {
		return sandbox.Spec{Mode: mode, Worktree: dir, Network: false}.Wrap(argv)
	}
}

// runShell executes a gate or test command in a worktree.
//
// Its own process group, so a test suite that spawns a server does not leave
// it holding a port in a worktree about to be handed to someone else. wrap, if
// set, confines it — for gate commands that means no network, because build
// and test code should not phone home.
func runShell(ctx context.Context, dir, cmdline string, wrap wrapFunc) (stdout, stderr string, exitCode int) {
	if strings.TrimSpace(cmdline) == "" {
		return "", "", 0
	}

	argv := []string{"sh", "-c", cmdline}
	if wrap != nil {
		wrapped, cleanup, err := wrap(argv)
		if err != nil {
			return "", "sandbox: " + err.Error(), 1
		}
		defer cleanup()
		argv = wrapped
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		exitCode = 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		}
	}

	so, _ := gate.Clamp(outBuf.String())
	se, _ := gate.Clamp(errBuf.String())
	return so, se, exitCode
}
