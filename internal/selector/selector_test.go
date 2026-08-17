package selector

import "testing"

func TestSelect(t *testing.T) {
	cases := []struct {
		name       string
		cands      []Candidate
		wantWinner string
		wantReason string
		wantRung   int
	}{
		{
			name: "nobody passed",
			cands: []Candidate{
				{Builder: "a", Passed: false},
				{Builder: "b", Passed: false},
			},
			wantWinner: "",
			wantReason: "no candidate passed the gate",
		},
		{
			name: "one passed — no ladder needed",
			cands: []Candidate{
				{Builder: "a", Passed: false},
				{Builder: "b", Passed: true, DiffLines: 400},
			},
			wantWinner: "b",
			wantReason: "only passing candidate",
		},
		{
			name: "editing the frozen tests disqualifies, however small the diff",
			cands: []Candidate{
				{Builder: "a", Passed: true, TouchedFrozen: true, DiffLines: 3},
				{Builder: "b", Passed: true, DiffLines: 300},
			},
			wantWinner: "b",
			wantReason: "only passing candidate",
		},
		{
			name: "both clean — smallest diff wins at rung one",
			cands: []Candidate{
				{Builder: "a", Passed: true, DiffLines: 47, FilesTouched: 3},
				{Builder: "b", Passed: true, DiffLines: 29, FilesTouched: 2},
			},
			wantWinner: "b",
			wantReason: "smallest diff",
			wantRung:   1,
		},
		{
			name: "diff ties, so the ladder advances to files touched",
			cands: []Candidate{
				{Builder: "a", Passed: true, DiffLines: 40, FilesTouched: 5},
				{Builder: "b", Passed: true, DiffLines: 40, FilesTouched: 2},
			},
			wantWinner: "b",
			wantReason: "fewest files touched",
			wantRung:   2,
		},
		{
			name: "identical on everything computable — pick one, don't pay a judge",
			cands: []Candidate{
				{Builder: "a", Passed: true, DiffLines: 40, FilesTouched: 2},
				{Builder: "b", Passed: true, DiffLines: 40, FilesTouched: 2},
			},
			wantWinner: "a",
			wantReason: "indistinguishable; took the first",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Select(tc.cands)

			name := ""
			if got.Winner != nil {
				name = got.Winner.Builder
			}
			if name != tc.wantWinner {
				t.Errorf("winner = %q, want %q", name, tc.wantWinner)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if got.Rung != tc.wantRung {
				t.Errorf("rung = %d, want %d", got.Rung, tc.wantRung)
			}
		})
	}
}
