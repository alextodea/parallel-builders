package prompt

import (
	"strings"
	"testing"

	"github.com/alextodea/parallel-builders/internal/brief"
)

func hostileBrief() brief.Brief {
	b := brief.Brief{Feature: "handle uploads"}
	// A criterion carrying an injected secret and an injected instruction —
	// the exact thing that must not reach an agent verbatim.
	b.AddCriterion(`store the key gho_16CharactersOfNonsenseHere123456 in the header`, brief.Asked)
	b.AddCriterion("normal criterion", brief.Asked)
	return b
}

func TestEveryBuilderSanitisesTheBrief(t *testing.T) {
	b := hostileBrief()
	globs := []string{"**/*_test.go"}

	sets := map[string]Set{
		"spec":     Spec(b, globs),
		"build":    Build(b, globs),
		"repair":   Repair(b, "diff", []string{"TestX"}, ""),
		"escalate": Escalate(b, []string{"TestX"}, "a:1 b:2"),
	}

	for name, s := range sets {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(s.Text, "gho_16Char") {
				t.Error("a secret in the brief reached the prompt — sanitisation was bypassed")
			}
			if s.Redacted == 0 {
				t.Error("the redaction count should reflect the secret in the brief")
			}
			if s.Version != Version {
				t.Errorf("version = %q, want %q", s.Version, Version)
			}
			if !strings.Contains(s.Text, "BEGIN BRIEF") {
				t.Error("the brief was not fenced")
			}
		})
	}
}

func TestRepairSanitisesTheDiffToo(t *testing.T) {
	// A builder can copy a secret into its own output; the diff is not
	// trusted just because pb generated the round.
	diff := `+ apiKey := "sk-proj-abcdefghijklmnopqrstuvwxyz012345"`
	s := Repair(brief.Brief{Feature: "f", Criteria: []brief.Criterion{{ID: "C1", Text: "x", Source: brief.Asked}}}, diff, nil, "")

	if strings.Contains(s.Text, "sk-proj-abcdef") {
		t.Error("a secret in the returned diff reached the prompt")
	}
}

func TestHashIsStableAndSensitive(t *testing.T) {
	b := brief.Brief{Feature: "f", Criteria: []brief.Criterion{{ID: "C1", Text: "x", Source: brief.Asked}}}
	globs := []string{"**/*_test.go"}

	a := Hash(Spec(b, globs))
	again := Hash(Spec(b, globs))
	if a != again {
		t.Error("identical inputs must hash identically")
	}

	b2 := brief.Brief{Feature: "different", Criteria: b.Criteria}
	if Hash(Spec(b2, globs)) == a {
		t.Error("a different brief must change the hash")
	}
}

func TestFinishCannotBeBypassed(t *testing.T) {
	// Every exported builder must route through finish, so a fence and a
	// version are always present. This is the structural guarantee that no
	// caller can assemble a prompt that skips sanitisation.
	b := brief.Brief{Feature: "f", Criteria: []brief.Criterion{{ID: "C1", Text: "x", Source: brief.Asked}}}
	globs := []string{"**/*_test.go"}

	for _, s := range []Set{Spec(b, globs), Build(b, globs), Repair(b, "", nil, ""), Escalate(b, nil, "")} {
		if !strings.Contains(s.Text, "END BRIEF") || s.Version == "" {
			t.Error("a builder skipped finish()")
		}
	}
}
