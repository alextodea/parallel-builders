package frozen

import "testing"

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
