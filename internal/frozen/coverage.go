package frozen

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Every generated test names the criterion it satisfies:
//
//	func TestRateLimit_Burst(t *testing.T) { // pb:C1
//
// That one convention makes two failure modes mechanically detectable rather
// than matters of opinion. A criterion with no test means you approved
// something nothing verifies — the dangerous one, because the run goes green.
// A test with no criterion means the architect invented a requirement you
// never agreed to.
var markerRE = regexp.MustCompile(`\bpb:(C\d+)\b`)

// Markers returns the criterion ids referenced anywhere in a file.
func Markers(content string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range markerRE.FindAllStringSubmatch(content, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// CoverageReport is the two-directional check between a brief and its tests.
type CoverageReport struct {
	// Uncovered are criteria no test claims. You approved these and nothing
	// verifies them.
	Uncovered []string
	// Orphaned are ids referenced by tests that the brief does not contain —
	// an invented or stale requirement.
	Orphaned []string
}

func (r CoverageReport) OK() bool { return len(r.Uncovered) == 0 && len(r.Orphaned) == 0 }

func (r CoverageReport) Error() error {
	if r.OK() {
		return nil
	}
	var parts []string
	if len(r.Uncovered) > 0 {
		parts = append(parts, fmt.Sprintf(
			"no test claims %s — you approved these and nothing verifies them",
			strings.Join(r.Uncovered, ", ")))
	}
	if len(r.Orphaned) > 0 {
		parts = append(parts, fmt.Sprintf(
			"tests reference %s, which the brief does not contain — an invented requirement",
			strings.Join(r.Orphaned, ", ")))
	}
	return fmt.Errorf("coverage: %s", strings.Join(parts, "; "))
}

// Coverage compares criterion ids against the markers found in test files.
// files maps path to contents.
func Coverage(files map[string]string, criterionIDs []string) CoverageReport {
	claimed := map[string]bool{}
	for _, content := range files {
		for _, id := range Markers(content) {
			claimed[id] = true
		}
	}

	want := map[string]bool{}
	for _, id := range criterionIDs {
		want[id] = true
	}

	var rep CoverageReport
	for _, id := range criterionIDs {
		if !claimed[id] {
			rep.Uncovered = append(rep.Uncovered, id)
		}
	}
	for id := range claimed {
		if !want[id] {
			rep.Orphaned = append(rep.Orphaned, id)
		}
	}
	sort.Strings(rep.Uncovered)
	sort.Strings(rep.Orphaned)
	return rep
}
