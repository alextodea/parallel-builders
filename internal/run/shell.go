package run

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"

	"github.com/alextodea/parallel-builders/internal/gate"
)

// runShell executes a gate or test command in a worktree.
//
// Its own process group, so a test suite that spawns a server does not leave
// it holding a port in a worktree about to be handed to someone else.
func runShell(ctx context.Context, dir, cmdline string) (stdout, stderr string, exitCode int) {
	if strings.TrimSpace(cmdline) == "" {
		return "", "", 0
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdline)
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
