// Package sandbox confines the processes pb spawns.
//
// pb runs coding agents and, worse, arbitrary build and test commands out of a
// repository — automatically, in parallel, unattended. A git worktree isolates
// files between builders; it is not a security boundary. Nothing in it stops a
// misbehaving or prompt-injected process from overwriting your shell profile,
// reading your keys, or phoning home.
//
// The confinement here has two profiles, because the two kinds of process have
// genuinely different needs:
//
//   - An AGENT must reach its model provider over the network, so network
//     cannot be denied to it. What the sandbox does give is filesystem
//     confinement: it may write inside its worktree and the usual scratch and
//     cache locations, and nowhere else. It cannot rewrite ~/.zshrc or drop a
//     binary in ~/.local/bin.
//
//   - A GATE command (build, lint, test) runs untrusted repository code — the
//     likeliest place for a malicious dependency to act. It gets the same
//     filesystem confinement AND no network at all, because test suites should
//     not be phoning anywhere.
//
// This is not perfect. An agent with network can still, in principle, read a
// file it is allowed to read and send it somewhere. Closing that fully needs a
// network allowlist the sandbox layer here does not attempt. What it does do is
// remove the easy, high-impact failures: tampering with the host, and letting
// test code reach the network at all.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Mode selects the confinement mechanism.
type Mode string

const (
	// None runs the command unconfined. It must be chosen explicitly and is
	// never a silent default — the caller is expected to warn.
	None Mode = "none"
	// Seatbelt uses macOS sandbox-exec.
	Seatbelt Mode = "seatbelt"
	// Bwrap uses Linux bubblewrap.
	Bwrap Mode = "bwrap"
	// Auto detects the best available mechanism for this OS.
	Auto Mode = "auto"
)

// Spec describes one confinement.
type Spec struct {
	Mode     Mode
	Worktree string // the one directory writes are allowed into
	// Network allows outbound network. True for agents (they must reach a
	// model), false for gate commands (repo code should not phone home).
	Network bool
}

// Available reports whether a mechanism can actually be used here.
func Available(m Mode) bool {
	switch m {
	case None, "":
		return true
	case Seatbelt:
		return runtime.GOOS == "darwin" && lookPath("sandbox-exec")
	case Bwrap:
		return runtime.GOOS == "linux" && lookPath("bwrap")
	case Auto:
		return Available(Seatbelt) || Available(Bwrap)
	}
	return false
}

// Resolve turns Auto into a concrete mechanism, or None if nothing is present.
// The empty string resolves to None so that a zero-value Spec is a safe no-op;
// the pb binary chooses Auto as its own default separately.
func Resolve(m Mode) Mode {
	if m == "" {
		return None
	}
	if m != Auto {
		return m
	}
	switch {
	case Available(Seatbelt):
		return Seatbelt
	case Available(Bwrap):
		return Bwrap
	default:
		return None
	}
}

// Wrap prefixes argv with whatever confines it. The returned argv is what to
// exec; the cleanup closure removes any temporary profile file and must be
// called once the command has finished.
//
// An unavailable mechanism is an error, never a silent downgrade to None: a
// user who asked for a sandbox and did not get one must be told, not quietly
// left unprotected.
func (s Spec) Wrap(argv []string) (wrapped []string, cleanup func(), err error) {
	noop := func() {}
	if len(argv) == 0 {
		return nil, noop, fmt.Errorf("sandbox: empty command")
	}

	mode := Resolve(s.Mode)
	if mode == None {
		return argv, noop, nil
	}
	// Verify the resolved mechanism is actually present. Without this a
	// caller on macOS asking for bwrap would get a bwrap command line that
	// fails to exec — or worse, a mechanism that silently does nothing —
	// instead of a clear "not available here". Safety must fail loud.
	if !Available(mode) {
		return nil, noop, fmt.Errorf("sandbox: %q is not available on this system (need %s)", mode, need(mode))
	}
	switch mode {
	case Seatbelt:
		return s.wrapSeatbelt(argv)
	case Bwrap:
		return s.wrapBwrap(argv)
	default:
		return nil, noop, fmt.Errorf("sandbox: unknown mode %q", s.Mode)
	}
}

// wrapSeatbelt writes a profile to a temp file and prefixes with sandbox-exec.
//
// The profile is written to a file rather than passed inline so the worktree
// path is carried as a -D parameter, which sandbox-exec quotes safely — a path
// interpolated straight into profile text could otherwise break out of the
// string literal.
func (s Spec) wrapSeatbelt(argv []string) ([]string, func(), error) {
	if s.Worktree == "" {
		return nil, func() {}, fmt.Errorf("sandbox: seatbelt needs a worktree")
	}
	work, err := filepath.Abs(s.Worktree)
	if err != nil {
		return nil, func() {}, err
	}

	f, err := os.CreateTemp("", "pb-sandbox-*.sb")
	if err != nil {
		return nil, func() {}, err
	}
	if _, err := f.WriteString(seatbeltProfile(s.Network)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, func() {}, err
	}
	f.Close()
	cleanup := func() { os.Remove(f.Name()) }

	home, _ := os.UserHomeDir()
	wrapped := append([]string{
		"sandbox-exec",
		"-D", "WORK=" + work,
		"-D", "HOME_CACHE=" + filepath.Join(home, ".cache"),
		"-f", f.Name(),
	}, argv...)
	return wrapped, cleanup, nil
}

// seatbeltProfile allows everything by default, then subtracts. Starting from
// deny-all breaks the toolchain in a hundred small ways (it reads libraries,
// sysctls, temp dirs); starting from allow-default and removing the two things
// that matter — writes outside the worktree, and (for gate commands) the
// network — is both robust and easy to audit.
func seatbeltProfile(network bool) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	if !network {
		b.WriteString("(deny network*)\n")
	}
	b.WriteString(`(deny file-write*)
(allow file-write*
  (subpath (param "WORK"))
  (subpath (param "HOME_CACHE"))
  (subpath "/private/tmp")
  (subpath "/private/var/folders")
  (subpath "/dev"))
`)
	return b.String()
}

// wrapBwrap confines with bubblewrap on Linux: a fresh namespace, the host
// filesystem mounted read-only, the worktree and scratch dirs read-write, and
// no network for gate commands.
func (s Spec) wrapBwrap(argv []string) ([]string, func(), error) {
	if s.Worktree == "" {
		return nil, func() {}, fmt.Errorf("sandbox: bwrap needs a worktree")
	}
	work, err := filepath.Abs(s.Worktree)
	if err != nil {
		return nil, func() {}, err
	}
	home, _ := os.UserHomeDir()

	args := []string{
		"bwrap",
		"--ro-bind", "/", "/", // whole host readable…
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", work, work, // …but only the worktree writable
		"--bind", filepath.Join(home, ".cache"), filepath.Join(home, ".cache"),
		"--die-with-parent",
	}
	if !s.Network {
		args = append(args, "--unshare-net")
	}
	args = append(args, argv...)
	return args, func() {}, nil
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func need(m Mode) string {
	switch m {
	case Seatbelt:
		return "macOS with sandbox-exec"
	case Bwrap:
		return "Linux with bubblewrap (bwrap)"
	}
	return "a supported sandbox"
}
