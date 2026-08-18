package frozen

import (
	"strings"
	"testing"
)

func TestGuardCatchesTestEdits(t *testing.T) {
	g := Guard{Patterns: []string{"**/*_test.go", "testdata/**"}}

	cases := []struct {
		name    string
		changed []string
		want    int
	}{
		{
			name:    "implementation only is allowed",
			changed: []string{"internal/api/handler.go", "go.mod"},
			want:    0,
		},
		{
			name:    "test file at depth is caught",
			changed: []string{"internal/api/handler_test.go"},
			want:    1,
		},
		{
			name:    "test file at root is caught",
			changed: []string{"main_test.go"},
			want:    1,
		},
		{
			name:    "directory pattern is caught",
			changed: []string{"testdata/fixtures/one.json"},
			want:    1,
		},
		{
			name:    "the sneaky one — real fix plus a weakened assertion",
			changed: []string{"internal/api/handler.go", "internal/api/handler_test.go"},
			want:    1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(g.Check(tc.changed)); got != tc.want {
				t.Fatalf("got %d violations, want %d", got, tc.want)
			}
		})
	}
}

func TestOwnsIdentifiesTheSuite(t *testing.T) {
	g := Guard{Patterns: []string{"**/*_test.go"}}

	if !g.Owns("internal/pool/pool_test.go") {
		t.Error("expected a _test.go file to be part of the frozen suite")
	}
	if g.Owns("internal/pool/pool.go") {
		t.Error("implementation must not be counted as part of the suite")
	}
}

func TestCoverageBothDirections(t *testing.T) {
	files := map[string]string{
		"a_test.go": "func TestX(t *testing.T) { // pb:C1\n}\nfunc TestY(t *testing.T) { // pb:C2\n}",
	}

	t.Run("complete", func(t *testing.T) {
		if rep := Coverage(files, []string{"C1", "C2"}); !rep.OK() {
			t.Errorf("expected OK, got %+v", rep)
		}
	})

	t.Run("a criterion nothing verifies", func(t *testing.T) {
		// The dangerous direction: you approved C3 and the run goes green
		// without ever checking it.
		rep := Coverage(files, []string{"C1", "C2", "C3"})
		if rep.OK() || len(rep.Uncovered) != 1 || rep.Uncovered[0] != "C3" {
			t.Fatalf("expected C3 uncovered, got %+v", rep)
		}
		if !strings.Contains(rep.Error().Error(), "nothing verifies") {
			t.Errorf("error should explain the risk: %v", rep.Error())
		}
	})

	t.Run("a test claiming an invented requirement", func(t *testing.T) {
		rep := Coverage(files, []string{"C1"})
		if rep.OK() || len(rep.Orphaned) != 1 || rep.Orphaned[0] != "C2" {
			t.Fatalf("expected C2 orphaned, got %+v", rep)
		}
	})
}

func TestMarkersAreDeduplicatedAndSorted(t *testing.T) {
	got := Markers("// pb:C10\n// pb:C2\n// pb:C2\nnot pb:CX or pbC3")
	if len(got) != 2 || got[0] != "C10" || got[1] != "C2" {
		t.Fatalf("Markers = %v", got)
	}
}
