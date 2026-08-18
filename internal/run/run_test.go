package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alextodea/parallel-builders/internal/agent"
	"github.com/alextodea/parallel-builders/internal/brief"
	"github.com/alextodea/parallel-builders/internal/config"
	"github.com/alextodea/parallel-builders/internal/pool"
)

// ---------------------------------------------------------------------------
// scripted agent — the whole pipeline is exercised without a model
// ---------------------------------------------------------------------------

// scripted writes a fixed set of files per round. Round indexing is 1-based
// and the last entry repeats, so a two-round test needs only what differs.
type scripted struct {
	id    string
	files []map[string]string
	calls int
	fail  error
}

func (s *scripted) Name() string { return s.id }

func (s *scripted) Run(ctx context.Context, dir, prompt string) (agent.Result, error) {
	if s.fail != nil {
		return agent.Result{}, s.fail
	}
	i := s.calls
	s.calls++
	if i >= len(s.files) {
		i = len(s.files) - 1
	}
	for rel, content := range s.files[i] {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return agent.Result{}, err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return agent.Result{}, err
		}
	}
	return agent.Result{Wall: time.Millisecond}, nil
}

// ---------------------------------------------------------------------------
// a real Go repo to operate on
// ---------------------------------------------------------------------------

const mathGo = `package mathx

func Add(a, b int) int { return a + b }
`

// The spec the architect "writes": two criteria, both marked, both failing at
// base because Double does not exist yet — so the package will not even build.
const specTests = `package mathx

import "testing"

func TestDoubleReturnsTwiceTheInput(t *testing.T) { // pb:C1
	if got := Double(3); got != 6 {
		t.Fatalf("Double(3) = %d, want 6", got)
	}
}

func TestDoubleHandlesZero(t *testing.T) { // pb:C2
	if got := Double(0); got != 0 {
		t.Fatalf("Double(0) = %d, want 0", got)
	}
}
`

const implGood = `package mathx

func Double(n int) int { return n * 2 }
`

// Passes C2 but not C1 — a plausible near-miss rather than a total failure.
const implHalf = `package mathx

func Double(n int) int { return n }
`

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module mathx\n\ngo 1.24\n")
	write("math.go", mathGo)

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", "-A")
	run("commit", "-qm", "base")
	return dir
}

func newBrief() brief.Brief {
	b := brief.Brief{Feature: "a Double function"}
	b.AddCriterion("Double(n) returns twice n", brief.Asked)
	b.AddCriterion("Double(0) is 0", brief.Asked)
	return b
}

func newConfig() config.Config {
	return config.Config{
		Gate: config.Gate{
			Build: "go build ./...",
			Test:  "go test ./...",
		},
		Tests: config.Tests{Paths: []string{"**/*_test.go"}},
	}
}

func setup(t *testing.T, builders ...agent.Runner) (Options, func()) {
	t.Helper()
	repo := newRepo(t)
	p, err := pool.Open(t.TempDir(), repo, len(builders))
	if err != nil {
		t.Fatal(err)
	}
	arch := &scripted{id: "architect", files: []map[string]string{
		{"math_test.go": specTests},
	}}
	return Options{
		Repo:      repo,
		Config:    newConfig(),
		Brief:     newBrief(),
		Architect: arch,
		Builders:  builders,
		Pool:      p,
		Out:       testWriter{t},
	}, func() { p.ReleaseAll() }
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// ---------------------------------------------------------------------------

func TestOneBuilderPasses(t *testing.T) {
	o, done := setup(t,
		&scripted{id: "loser", files: []map[string]string{{"impl.go": implHalf}}},
		&scripted{id: "winner", files: []map[string]string{{"impl.go": implGood}}},
	)
	defer done()

	out, err := Execute(context.Background(), o)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Winner == nil {
		t.Fatalf("expected a winner; candidates=%+v", out.Candidates)
	}
	if out.Winner.Builder != "winner" {
		t.Errorf("winner = %s, want winner", out.Winner.Builder)
	}
	if out.Reason != "only passing candidate" {
		t.Errorf("reason = %q", out.Reason)
	}
	if out.SpecSHA == "" || out.SpecSHA == out.Base {
		t.Error("the spec must be its own commit, distinct from base")
	}
}

// The bug this guards: builders reset to base rather than to the spec commit
// would race against tests that are not in their worktree, and every candidate
// would pass a suite that does not exist.
func TestBuildersSeeTheFrozenTests(t *testing.T) {
	o, done := setup(t,
		&scripted{id: "b", files: []map[string]string{{"impl.go": implGood}}},
	)
	defer done()

	out, err := Execute(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if out.Winner == nil {
		t.Fatal("expected a winner")
	}
	if _, err := os.Stat(filepath.Join(out.Winner.Worktree, "math_test.go")); err != nil {
		t.Fatalf("the builder's worktree is missing the frozen tests: %v", err)
	}
	if out.Winner.Tests.Total != 2 {
		t.Errorf("ran %d tests, want 2 — the builder was graded against the wrong suite", out.Winner.Tests.Total)
	}
}

func TestBothPassAndSmallestDiffWins(t *testing.T) {
	fat := implGood + "\n// padding\n// padding\n// padding\n// padding\n"
	o, done := setup(t,
		&scripted{id: "fat", files: []map[string]string{{"impl.go": fat}}},
		&scripted{id: "lean", files: []map[string]string{{"impl.go": implGood}}},
	)
	defer done()

	out, err := Execute(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if out.Winner == nil {
		t.Fatal("expected a winner")
	}
	if out.Winner.Builder != "lean" {
		t.Errorf("winner = %s, want lean (smaller diff)", out.Winner.Builder)
	}
	if out.Reason != "smallest diff" {
		t.Errorf("reason = %q, want smallest diff", out.Reason)
	}
}

// Nobody passes round one; each builder repairs its own work and one succeeds.
func TestRepairRoundRecovers(t *testing.T) {
	o, done := setup(t,
		&scripted{id: "stubborn", files: []map[string]string{
			{"impl.go": implHalf},
			{"impl.go": implHalf}, // still wrong on round two
		}},
		&scripted{id: "learner", files: []map[string]string{
			{"impl.go": implHalf},
			{"impl.go": implGood}, // patched
		}},
	)
	defer done()

	out, err := Execute(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if out.Rounds != 2 {
		t.Errorf("rounds = %d, want 2", out.Rounds)
	}
	if out.Winner == nil {
		t.Fatalf("expected the repaired candidate to win; route=%s", out.Route)
	}
	if out.Winner.Builder != "learner" {
		t.Errorf("winner = %s, want learner", out.Winner.Builder)
	}
}

func TestEditingFrozenTestsDisqualifies(t *testing.T) {
	// The classic cheat: make the failing test pass by weakening it.
	weakened := `package mathx

import "testing"

func TestDoubleReturnsTwiceTheInput(t *testing.T) { // pb:C1
	_ = Double(3)
}

func TestDoubleHandlesZero(t *testing.T) { // pb:C2
	_ = Double(0)
}
`
	o, done := setup(t,
		&scripted{id: "cheat", files: []map[string]string{
			{"impl.go": implHalf, "math_test.go": weakened},
		}},
	)
	defer done()

	out, err := Execute(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if out.Winner != nil {
		t.Fatal("a candidate that edited the tests must never win, however green it looks")
	}
	if len(out.Candidates[0].TouchedFrozen) == 0 {
		t.Error("the freeze violation was not detected")
	}
}

func TestBuilderThatDoesNothingIsNotAWinner(t *testing.T) {
	o, done := setup(t,
		&scripted{id: "idle", files: []map[string]string{{}}},
	)
	defer done()

	out, err := Execute(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if out.Winner != nil {
		t.Fatal("an empty diff is not a solution")
	}
	if !out.Candidates[0].NoDiff {
		t.Error("expected NoDiff to be set")
	}
}

func TestAgentFailureIsNotATestFailure(t *testing.T) {
	o, done := setup(t,
		&scripted{id: "broken", fail: context.DeadlineExceeded},
		&scripted{id: "fine", files: []map[string]string{{"impl.go": implGood}}},
	)
	defer done()

	out, err := Execute(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	// One builder's process dying must not abort the run.
	if out.Winner == nil || out.Winner.Builder != "fine" {
		t.Fatalf("the healthy builder should still win, got %+v", out.Winner)
	}
	if out.Candidates[0].AgentErr == nil {
		t.Error("the failed agent should record an AgentErr")
	}
}

func TestVacuousTestsAreRejected(t *testing.T) {
	// A test that passes without the feature proves nothing. This one
	// compiles at base and passes, which is exactly the case to catch.
	o, done := setup(t,
		&scripted{id: "b", files: []map[string]string{{"impl.go": implGood}}},
	)
	defer done()

	o.Architect = &scripted{id: "lazy", files: []map[string]string{
		{"math_test.go": `package mathx

import "testing"

func TestAddStillWorks(t *testing.T) { // pb:C1
	if Add(1, 1) != 2 { t.Fatal("no") }
}

func TestAlsoNothing(t *testing.T) { // pb:C2
	if Add(0, 0) != 0 { t.Fatal("no") }
}
`},
	}}

	_, err := Execute(context.Background(), o)
	if err == nil {
		t.Fatal("expected the vacuity check to reject tests that already pass")
	}
	if !strings.Contains(err.Error(), "already pass") {
		t.Errorf("error = %v, want it to mention passing at base", err)
	}
}

func TestUncoveredCriterionIsRejected(t *testing.T) {
	o, done := setup(t,
		&scripted{id: "b", files: []map[string]string{{"impl.go": implGood}}},
	)
	defer done()

	// Only C1 is marked; C2 was approved and nothing verifies it.
	o.Architect = &scripted{id: "partial", files: []map[string]string{
		{"math_test.go": `package mathx

import "testing"

func TestOnlyOne(t *testing.T) { // pb:C1
	if Double(3) != 6 { t.Fatal("no") }
}
`},
	}}

	_, err := Execute(context.Background(), o)
	if err == nil {
		t.Fatal("expected coverage to reject an unverified criterion")
	}
	if !strings.Contains(err.Error(), "C2") {
		t.Errorf("error should name the uncovered criterion, got %v", err)
	}
}

func TestArchitectWritingImplementationIsRejected(t *testing.T) {
	o, done := setup(t,
		&scripted{id: "b", files: []map[string]string{{"impl.go": implGood}}},
	)
	defer done()

	o.Architect = &scripted{id: "overreach", files: []map[string]string{
		{"math_test.go": specTests, "impl.go": implGood},
	}}

	_, err := Execute(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "non-test files") {
		t.Fatalf("the architect must not write implementation, got %v", err)
	}
}

func TestCancellationReleasesEverything(t *testing.T) {
	o, done := setup(t,
		&scripted{id: "slow", files: []map[string]string{{"impl.go": implGood}}},
	)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already dead before we start

	_, err := Execute(ctx, o)
	if err == nil {
		t.Fatal("expected a cancelled context to surface an error")
	}

	// The pool must be usable afterwards: a leaked lock would block every
	// future run on this repo.
	p2, err := pool.Open(filepath.Dir(o.Pool.Root()), o.Repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = p2
}
