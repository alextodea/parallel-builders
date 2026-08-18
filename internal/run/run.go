// Package run wires the phases together.
//
// Everything it calls is already built and tested in isolation; this is the
// code that decides what happens in which order, and it is where the two
// subtle ordering facts live:
//
//   - Builders start from the SPEC commit, never from base. The frozen tests
//     are a commit, so a worktree reset to base does not contain them — every
//     builder would race against a suite it cannot see.
//   - Worktrees are held across repair rounds. A repair hands a builder its
//     own diff back; releasing between rounds would delete the thing being
//     repaired.
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alextodea/parallel-builders/internal/agent"
	"github.com/alextodea/parallel-builders/internal/brief"
	"github.com/alextodea/parallel-builders/internal/config"
	"github.com/alextodea/parallel-builders/internal/escalate"
	"github.com/alextodea/parallel-builders/internal/frozen"
	"github.com/alextodea/parallel-builders/internal/gate"
	"github.com/alextodea/parallel-builders/internal/pool"
	"github.com/alextodea/parallel-builders/internal/prompt"
	"github.com/alextodea/parallel-builders/internal/selector"
)

// Options is everything a run needs. Agents are injected so the whole pipeline
// can be exercised against fakes — no tokens, no network, a two-second cycle.
type Options struct {
	Repo   string
	Config config.Config
	Brief  brief.Brief

	Architect agent.Runner
	Builders  []agent.Runner

	Pool *pool.Pool
	Out  io.Writer

	// prompts accumulates every prompt actually sent, so the run record can
	// carry the version and total redaction count. Shared across the
	// concurrent builders, so guarded.
	prompts *promptLedger
}

// promptLedger records what was sent, for the run record. A changed prompt
// version silently invalidates benchmark comparisons, so it is tracked rather
// than assumed.
type promptLedger struct {
	mu       sync.Mutex
	version  string
	hash     string
	redacted int
}

func (o Options) stampPrompt(s prompt.Set) {
	if o.prompts == nil {
		return
	}
	o.prompts.mu.Lock()
	defer o.prompts.mu.Unlock()
	o.prompts.version = s.Version
	o.prompts.redacted += s.Redacted
	o.prompts.hash = prompt.Hash(s)
}

// Outcome is the result of a whole run.
type Outcome struct {
	Base    string
	SpecSHA string
	Rounds  int

	Candidates []Candidate
	Winner     *Candidate
	Reason     string

	// Escalated is set when no candidate passed and the round cap was hit.
	Escalated bool
	Route     string

	// PromptVersion and Redacted go into the run record. A run whose prompt
	// version differs from another's is not comparable to it.
	PromptVersion string
	Redacted      int
}

// Candidate is one builder's attempt after gating.
type Candidate struct {
	Builder  string
	Slot     int
	Worktree string

	AgentErr error // the process failed, which is not the same as tests failing
	NoDiff   bool

	TouchedFrozen []frozen.Violation
	Gate          gate.Report
	Tests         gate.Result

	DiffLines, FilesTouched, DepsAdded int
	Wall                               time.Duration
}

// Passed is true only when the candidate cleared every check. A frozen-file
// violation disqualifies regardless of the tests, because a candidate that
// edited the exam has made its own result meaningless.
func (c Candidate) Passed() bool {
	return c.AgentErr == nil && !c.NoDiff && len(c.TouchedFrozen) == 0 &&
		c.Gate.Passed && c.Tests.OK()
}

func (c Candidate) failedAt() string {
	switch {
	case c.AgentErr != nil:
		return "agent"
	case c.NoDiff:
		return "no-diff"
	case len(c.TouchedFrozen) > 0:
		return "frozen"
	case !c.Gate.Passed:
		return c.Gate.FailedAt
	case c.Tests.Crashed:
		return "crash"
	case len(c.Tests.Failed) > 0:
		return "test"
	}
	return ""
}

var (
	// ErrNoTests means the architect produced nothing to grade against.
	ErrNoTests = errors.New("the architect wrote no tests")
	// ErrVacuous means at least one test already passes without the feature.
	ErrVacuous = errors.New("tests pass before the feature exists")
)

// Execute runs the pipeline. Named returns so the deferred prompt-ledger flush
// lands on the value actually returned.
func Execute(ctx context.Context, o Options) (out Outcome, err error) {
	if len(o.Builders) == 0 {
		return out, errors.New("no builders configured")
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	o.prompts = &promptLedger{}
	defer func() {
		out.PromptVersion = o.prompts.version
		out.Redacted = o.prompts.redacted
	}()

	// Pin the base once. Re-reading HEAD later could hand two builders
	// different starting code if the user commits mid-run.
	base, err := head(ctx, o.Repo)
	if err != nil {
		return out, err
	}
	out.Base = base

	specSHA, err := o.spec(ctx, base)
	if err != nil {
		return out, err
	}
	out.SpecSHA = specSHA

	// Slots are held for the whole run, not per round: a repair hands a
	// builder back its own diff, which only exists while the worktree does.
	defer o.Pool.ReleaseAll()

	var lastFailures map[string][]string

	for round := 1; round <= escalate.MaxRounds; round++ {
		out.Rounds = round

		cands, err := o.race(ctx, specSHA, round, lastFailures)
		if err != nil {
			return out, err
		}
		out.Candidates = cands

		sel := make([]selector.Candidate, 0, len(cands))
		for _, c := range cands {
			sel = append(sel, selector.Candidate{
				Builder:       c.Builder,
				Passed:        c.Passed(),
				FailedAt:      c.failedAt(),
				FailingTests:  c.Tests.Failed,
				TouchedFrozen: len(c.TouchedFrozen) > 0,
				DiffLines:     c.DiffLines,
				FilesTouched:  c.FilesTouched,
				DepsAdded:     c.DepsAdded,
			})
		}

		res := selector.Select(sel)
		if res.Winner != nil {
			for i := range cands {
				if cands[i].Builder == res.Winner.Builder {
					out.Winner = &cands[i]
					break
				}
			}
			out.Reason = res.Reason
			return out, nil
		}

		// Nobody passed. How they failed decides what happens next, and
		// working that out costs nothing.
		lastFailures = map[string][]string{}
		for _, c := range cands {
			lastFailures[c.Builder] = c.Tests.Failed
		}

		route, why := escalate.Decide(round, lastFailures)
		out.Route = route.String()
		fmt.Fprintf(o.Out, "  compare  %s → %s\n", why, route)

		if route == escalate.Human {
			out.Escalated = true
			return out, nil
		}
		// SelfRepair and Architect both re-enter the loop; the difference is
		// which prompt the next round builds, handled in race().
	}

	out.Escalated = true
	return out, nil
}

// spec has the architect write the tests, then verifies them two ways before
// anything is raced against them.
func (o Options) spec(ctx context.Context, base string) (string, error) {
	w, err := o.Pool.Get(ctx, 0, base)
	if err != nil {
		return "", fmt.Errorf("spec: %w", err)
	}
	// Released so the race can claim slot 0 like any other.
	defer o.Pool.Release(0)

	fmt.Fprintf(o.Out, "  spec     %s\n", o.Architect.Name())

	set := prompt.Spec(o.Brief, o.Config.Tests.Paths)
	o.stampPrompt(set)
	if _, err := o.Architect.Run(ctx, w.Path, set.Text); err != nil {
		return "", fmt.Errorf("spec: architect: %w", err)
	}

	changed, err := changedFiles(ctx, w.Path)
	if err != nil {
		return "", err
	}
	if len(changed) == 0 {
		return "", ErrNoTests
	}

	// The architect was told to write only tests. Anything else is scope it
	// invented, and it would be racing against its own implementation.
	guard := frozen.Guard{Patterns: o.Config.Tests.Paths}
	var stray []string
	testFiles := map[string]string{}
	for _, f := range changed {
		if !guard.Owns(f) {
			stray = append(stray, f)
			continue
		}
		b, err := os.ReadFile(filepath.Join(w.Path, f))
		if err == nil {
			testFiles[f] = string(b)
		}
	}
	if len(stray) > 0 {
		return "", fmt.Errorf("spec: the architect wrote non-test files (%s) — it was asked for tests only",
			strings.Join(stray, ", "))
	}
	if len(testFiles) == 0 {
		return "", ErrNoTests
	}

	if rep := frozen.Coverage(testFiles, o.Brief.IDs()); !rep.OK() {
		return "", fmt.Errorf("spec: %w", rep.Error())
	}
	fmt.Fprintf(o.Out, "           %d test files · %d criteria covered\n", len(testFiles), len(o.Brief.IDs()))

	if err := o.vacuity(ctx, w.Path); err != nil {
		return "", err
	}

	sha, err := commitAll(ctx, w.Path, "pb: spec for "+o.Brief.Feature)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(o.Out, "           frozen at %s\n", short(sha))
	return sha, nil
}

// vacuity runs the new tests against the code as it was before the feature
// existed. Anything that passes there is testing nothing.
//
// The subtlety: in real TDD the new tests reference functions that do not
// exist yet, so the package does not compile at base. That is the tests
// failing in the strongest possible way — a build error here is a pass, and
// treating it as an error would reject every properly written spec.
func (o Options) vacuity(ctx context.Context, dir string) error {
	parser, _ := gate.ParserFor(o.Config.Gate.Test)
	cmd := parser.Command(o.Config.Gate.Test)

	stdout, stderr, code := runShell(ctx, dir, cmd)
	res := parser.Parse(stdout, stderr, code)

	if res.Crashed {
		fmt.Fprintf(o.Out, "           vacuity  ✓ (does not build without the feature)\n")
		return nil
	}
	if res.Passed > 0 {
		return fmt.Errorf("%w: %d of %d already pass at the base commit — they do not test the new behaviour",
			ErrVacuous, res.Passed, res.Total)
	}
	fmt.Fprintf(o.Out, "           vacuity  ✓ (%d/%d fail as expected)\n", len(res.Failed), res.Total)
	return nil
}

// race runs every builder concurrently and gates each result.
func (o Options) race(ctx context.Context, specSHA string, round int, prev map[string][]string) ([]Candidate, error) {
	cands := make([]Candidate, len(o.Builders))
	var wg sync.WaitGroup

	for i, b := range o.Builders {
		wg.Add(1)
		go func(i int, b agent.Runner) {
			defer wg.Done()
			cands[i] = o.oneBuilder(ctx, i, b, specSHA, round, prev)
		}(i, b)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return cands, err
	}
	return cands, nil
}

func (o Options) oneBuilder(ctx context.Context, i int, b agent.Runner, specSHA string, round int, prev map[string][]string) Candidate {
	c := Candidate{Builder: b.Name(), Slot: i}
	start := time.Now()
	defer func() { c.Wall = time.Since(start) }()

	// Round 1 resets to the spec commit. Later rounds must NOT reset — the
	// builder's own work is what a repair operates on.
	commit := specSHA
	if round > 1 {
		commit = ""
	}

	var w *pool.Worktree
	var err error
	if round == 1 {
		w, err = o.Pool.Get(ctx, i, commit)
	} else {
		w, err = o.Pool.Get(ctx, i, specSHA) // already held; returns as-is
	}
	if err != nil {
		c.AgentErr = err
		return c
	}
	c.Worktree = w.Path

	var set prompt.Set
	if round == 1 {
		set = prompt.Build(o.Brief, o.Config.Tests.Paths)
	} else {
		diff, _ := unifiedDiff(ctx, w.Path)
		set = prompt.Repair(o.Brief, diff, prev[b.Name()], "")
	}
	o.stampPrompt(set)

	if _, err := b.Run(ctx, w.Path, set.Text); err != nil {
		c.AgentErr = err
		return c
	}

	return o.gateOne(ctx, c, w.Path)
}

// gateOne applies the freeze guard and then the configured commands.
func (o Options) gateOne(ctx context.Context, c Candidate, dir string) Candidate {
	changed, err := changedFiles(ctx, dir)
	if err != nil {
		c.AgentErr = err
		return c
	}
	if len(changed) == 0 {
		c.NoDiff = true
		return c
	}

	guard := frozen.Guard{Patterns: o.Config.Tests.Paths}
	if v := guard.Check(changed); len(v) > 0 {
		// Disqualified outright. Its tests passing would mean nothing, so
		// there is no point running them.
		c.TouchedFrozen = v
		return c
	}

	c.DiffLines, c.FilesTouched, _ = diffStat(ctx, dir)
	c.DepsAdded, _ = depsAdded(ctx, dir)

	steps := []gate.Step{
		{Name: "build", Cmd: o.Config.Gate.Build},
		{Name: "lint", Cmd: o.Config.Gate.Lint},
	}
	c.Gate = gate.Run(ctx, dir, steps)
	if !c.Gate.Passed {
		return c
	}

	parser, _ := gate.ParserFor(o.Config.Gate.Test)
	stdout, stderr, code := runShell(ctx, dir, parser.Command(o.Config.Gate.Test))
	c.Tests = parser.Parse(stdout, stderr, code)
	c.Gate.Passed = c.Tests.OK()
	if !c.Tests.OK() {
		c.Gate.FailedAt = "test"
	}
	return c
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
