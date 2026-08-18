package gate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Result is what the gate learned from a test run.
//
// Crashed is deliberately separate from Failed. A suite that will not compile,
// or that panics, produces no per-test results at all — so a parser that only
// counted FAIL lines would report zero failures for a completely broken
// candidate, and the selector would happily crown it. Distinguishing "nothing
// failed" from "nothing ran" is the entire job of this type.
type Result struct {
	Failed  []string
	Passed  int
	Total   int
	Crashed bool
	// Note explains a crash in one line, for the run record.
	Note string
	// Truncated records how many bytes of output were dropped.
	Truncated int
}

// OK reports whether every test ran and passed.
func (r Result) OK() bool { return !r.Crashed && len(r.Failed) == 0 && r.Total > 0 }

// Parser turns a test command's output into a Result. One per language.
type Parser interface {
	// Command adapts the configured test command — the Go parser adds
	// -json, because scraping human output cannot survive subtests,
	// parallelism or panics.
	Command(base string) string
	Parse(stdout, stderr string, exitCode int) Result
}

// MaxOutput caps retained output. Test suites can emit megabytes, and every
// byte kept is a byte that may end up in a repair prompt.
const MaxOutput = 64 << 10

// ParserFor returns a parser for a configured test command, and whether one
// was recognised. An unrecognised command still works — the caller falls back
// to pass/fail on exit code — it just cannot compare failure sets.
func ParserFor(testCmd string) (Parser, bool) {
	if strings.Contains(testCmd, "go test") {
		return GoParser{}, true
	}
	return ExitCodeParser{}, false
}

// GoParser reads `go test -json`.
type GoParser struct{}

func (GoParser) Command(base string) string {
	if strings.Contains(base, "-json") {
		return base
	}
	// Insert immediately after `go test` so it lands before any package
	// arguments, which `go test` requires.
	if i := strings.Index(base, "go test"); i >= 0 {
		const n = len("go test")
		return base[:i+n] + " -json" + base[i+n:]
	}
	return base
}

// goEvent is the subset of `go test -json` records that matter here.
type goEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

func (GoParser) Parse(stdout, stderr string, exitCode int) Result {
	var r Result

	failed := map[string]bool{}
	passed := map[string]bool{}
	var buildErrs []string
	sawEvent := false
	panicked := false

	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var e goEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// A killed suite truncates its final line mid-JSON. Skipping it
			// is correct; failing the parse would turn a timeout into an
			// unexplained error.
			continue
		}
		sawEvent = true

		switch e.Action {
		case "build-fail":
			r.Crashed = true
		case "build-output":
			if s := strings.TrimSpace(e.Output); s != "" {
				buildErrs = append(buildErrs, s)
			}
		case "output":
			if strings.Contains(e.Output, "panic:") || strings.Contains(e.Output, "fatal error:") {
				panicked = true
			}
		case "fail":
			if e.Test != "" {
				failed[e.Test] = true
			}
		case "pass":
			if e.Test != "" {
				passed[e.Test] = true
			}
		}
	}

	switch {
	case r.Crashed:
		r.Note = "the package does not compile"
		if len(buildErrs) > 0 {
			r.Note += ": " + firstLine(strings.Join(buildErrs, " "))
		}
	case panicked:
		r.Crashed = true
		r.Note = "the test binary panicked — results are incomplete"
	case !sawEvent && exitCode != 0:
		// Non-zero exit with no JSON at all: the command itself failed,
		// e.g. a bad flag or a missing toolchain.
		r.Crashed = true
		r.Note = "test command produced no output; " + firstLine(stderr)
	}

	// Count leaves only, on both sides. `go test` reports a parent alongside
	// its subtests — TestTwo *and* TestTwo/beta — so counting raw events
	// inflates the totals and, worse for escalation, makes two builders that
	// failed the same subtest look like they failed different things.
	all := make(map[string]bool, len(failed)+len(passed))
	for n := range failed {
		all[n] = true
	}
	for n := range passed {
		all[n] = true
	}

	r.Failed = leaves(failed, all)
	r.Passed = len(leaves(passed, all))
	r.Total = r.Passed + len(r.Failed)
	return r
}

// leaves returns the members of set that have no descendant anywhere in all.
func leaves(set, all map[string]bool) []string {
	var out []string
	for name := range set {
		isParent := false
		for other := range all {
			if other != name && strings.HasPrefix(other, name+"/") {
				isParent = true
				break
			}
		}
		if !isParent {
			out = append(out, name)
		}
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// ExitCodeParser is the fallback for languages without a parser yet. It can
// tell pass from fail, which is enough to gate — it just cannot say *which*
// tests failed, so escalate cannot compare failure sets and every failure
// routes to self-repair.
type ExitCodeParser struct{}

func (ExitCodeParser) Command(base string) string { return base }

func (ExitCodeParser) Parse(stdout, stderr string, exitCode int) Result {
	if exitCode == 0 {
		return Result{Passed: 1, Total: 1}
	}
	return Result{
		Failed: []string{"(unknown)"},
		Total:  1,
		Note:   fmt.Sprintf("exit %d; no parser for this test command, so failing test names are unavailable", exitCode),
	}
}

// Clamp truncates output to MaxOutput, keeping the head and tail — the head
// carries the first failure, the tail carries the summary, and the middle is
// usually repetition.
func Clamp(s string) (string, int) {
	if len(s) <= MaxOutput {
		return s, 0
	}
	half := MaxOutput / 2
	dropped := len(s) - MaxOutput
	return s[:half] + fmt.Sprintf("\n\n… %d bytes omitted …\n\n", dropped) + s[len(s)-half:], dropped
}
