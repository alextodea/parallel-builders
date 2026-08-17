package escalate

import "testing"

func TestDecide(t *testing.T) {
	cases := []struct {
		name     string
		round    int
		failures map[string][]string
		want     Route
	}{
		{
			name:  "different mistakes stay in the cheap tier",
			round: 1,
			failures: map[string][]string{
				"a": {"TestBurst", "TestReset"},
				"b": {"TestConcurrent"},
			},
			want: SelfRepair,
		},
		{
			name:  "both stuck on the same test — the spec is suspect",
			round: 1,
			failures: map[string][]string{
				"a": {"TestBurst"},
				"b": {"TestBurst"},
			},
			want: Architect,
		},
		{
			name:  "same set, different order, still agreement",
			round: 1,
			failures: map[string][]string{
				"a": {"TestBurst", "TestReset"},
				"b": {"TestReset", "TestBurst"},
			},
			want: Architect,
		},
		{
			name:  "overlapping but not identical is not agreement",
			round: 1,
			failures: map[string][]string{
				"a": {"TestBurst", "TestReset"},
				"b": {"TestBurst"},
			},
			want: SelfRepair,
		},
		{
			name:  "the cap wins over everything",
			round: MaxRounds,
			failures: map[string][]string{
				"a": {"TestBurst"},
				"b": {"TestBurst"},
			},
			want: Human,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, why := Decide(tc.round, tc.failures)
			if got != tc.want {
				t.Fatalf("route = %s (%s), want %s", got, why, tc.want)
			}
		})
	}
}
