// Package selector decides which candidate wins.
//
// Everything here is arithmetic on facts already gathered — no model is
// consulted, and the developer is not in this loop. That is only possible
// because every signal below is computable: diff size, dependency count, files
// touched. A criterion like "which is more elegant" would need a human or a
// frontier call on every single run, and the economics of the whole tool would
// collapse.
package selector

// Candidate is one builder's attempt, after the gate has run.
type Candidate struct {
	Builder string

	Passed          bool
	FailedAt        string   // "build" | "lint" | "test" | "" if it passed
	FailingTests    []string
	TouchedFrozen   bool
	ViolatedArch    bool

	DiffLines    int
	FilesTouched int
	DepsAdded    int
}

// Result explains the outcome, so the run record can say *why* rather than
// just naming a winner.
type Result struct {
	Winner *Candidate
	Reason string
	// Rung is the tie-breaker index that decided it, or 0 if no tie-break
	// was needed. Useful for learning which rungs ever actually fire.
	Rung int
}

// Select applies disqualifiers, then walks the tie-break ladder, stopping at
// the first rung that separates the field.
func Select(cands []Candidate) Result {
	eligible := make([]*Candidate, 0, len(cands))
	for i := range cands {
		c := &cands[i]
		// Disqualifiers remove a candidate from consideration entirely.
		// They are not tie-breakers: a candidate that edited the tests
		// has not "done worse", it has invalidated its own result.
		if !c.Passed || c.TouchedFrozen || c.ViolatedArch {
			continue
		}
		eligible = append(eligible, c)
	}

	switch len(eligible) {
	case 0:
		return Result{Reason: "no candidate passed the gate"}
	case 1:
		return Result{Winner: eligible[0], Reason: "only passing candidate"}
	}

	// The ladder. Each rung returns a sole leader or nothing.
	rungs := []struct {
		name string
		less func(a, b *Candidate) int
	}{
		{"smallest diff", func(a, b *Candidate) int { return a.DiffLines - b.DiffLines }},
		{"fewest files touched", func(a, b *Candidate) int { return a.FilesTouched - b.FilesTouched }},
		{"fewest new dependencies", func(a, b *Candidate) int { return a.DepsAdded - b.DepsAdded }},
	}

	for i, r := range rungs {
		if w := soleMinimum(eligible, r.less); w != nil {
			return Result{Winner: w, Reason: r.name, Rung: i + 1}
		}
	}

	// Genuinely indistinguishable on every computable signal. Paying a
	// frontier model to choose between them would cost more than the
	// decision is worth — take the first and move on.
	return Result{Winner: eligible[0], Reason: "indistinguishable; took the first"}
}

// soleMinimum returns the single best candidate by cmp, or nil if two or more
// tie for best.
func soleMinimum(cands []*Candidate, cmp func(a, b *Candidate) int) *Candidate {
	best := cands[0]
	ties := 1
	for _, c := range cands[1:] {
		switch d := cmp(c, best); {
		case d < 0:
			best, ties = c, 1
		case d == 0:
			ties++
		}
	}
	if ties > 1 {
		return nil
	}
	return best
}
