// Package escalate decides what to do when every candidate failed.
//
// The decision is free: compare the sets of failing test names. If the builders
// failed on the same things, two independent attempts got stuck in the same
// place, which is evidence about the specification rather than about either
// builder's code. If they failed on different things, they simply made
// different mistakes and the cheap tier can fix its own work.
//
// No model is involved in making this call.
package escalate

import "sort"

// Route is what should happen next.
type Route int

const (
	// SelfRepair hands each builder its own diff and its own failures and
	// asks for a patch. No expensive call.
	SelfRepair Route = iota
	// Architect means both builders got stuck in the same place, so the
	// spec is the likely problem — re-specify, or cut the slice smaller.
	Architect
	// Human means the round cap is spent.
	Human
)

func (r Route) String() string {
	switch r {
	case SelfRepair:
		return "self-repair"
	case Architect:
		return "architect"
	default:
		return "human"
	}
}

// MaxRounds is the cap. It is the feature, not a limitation: a task still
// failing after this is telling you something a third round will not fix.
const MaxRounds = 2

// Decide routes a failed round. failures maps builder name to the test names
// that builder failed.
func Decide(round int, failures map[string][]string) (Route, string) {
	if round >= MaxRounds {
		return Human, "round cap reached"
	}
	if len(failures) < 2 {
		return SelfRepair, "single candidate"
	}
	if agree(failures) {
		return Architect, "all builders failed identically — spec is the likely problem"
	}
	return SelfRepair, "failure sets differ — separate mistakes"
}

// agree reports whether every builder failed on exactly the same set of tests.
func agree(failures map[string][]string) bool {
	var first []string
	for _, tests := range failures {
		norm := normalise(tests)
		if first == nil {
			first = norm
			continue
		}
		if !equal(first, norm) {
			return false
		}
	}
	// Nobody failing anything is not agreement about a spec problem.
	return len(first) > 0
}

func normalise(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
