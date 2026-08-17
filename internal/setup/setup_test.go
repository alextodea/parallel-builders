package setup

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidRepo(t *testing.T) {
	ok := []string{
		"alextodea/parallel-builders",
		"a/b",
		"Some-Org/repo.name_2",
	}
	bad := []string{
		"",
		"y",                  // the stray keystroke this exists to catch
		"alextodea",          // no name
		"/parallel-builders", // no owner
		"alextodea/",         // empty name
		"github.com/a/b",     // a URL, not owner/name
		"alex todea/repo",    // space
	}

	for _, s := range ok {
		if !ValidRepo(s) {
			t.Errorf("ValidRepo(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if ValidRepo(s) {
			t.Errorf("ValidRepo(%q) = true, want false", s)
		}
	}
}

func TestAskUsesDefaultOnEmptyInput(t *testing.T) {
	var out bytes.Buffer
	p := New(strings.NewReader("\n"), &out)

	if got := p.Ask("command", "claude"); got != "claude" {
		t.Errorf("pressing enter should take the default; got %q", got)
	}
}

func TestAskIntRejectsOutOfRange(t *testing.T) {
	var out bytes.Buffer
	// 99 is out of range, "abc" is not a number, 3 is accepted.
	p := New(strings.NewReader("99\nabc\n3\n"), &out)

	if got := p.AskInt("how many", 2, 1, 10); got != 3 {
		t.Errorf("AskInt = %d, want 3", got)
	}
	if !strings.Contains(out.String(), "between 1 and 10") {
		t.Error("expected an out-of-range message")
	}
	if !strings.Contains(out.String(), "not a number") {
		t.Error("expected a not-a-number message")
	}
}

func TestAskChoiceAcceptsNumberOrTypedName(t *testing.T) {
	opts := []string{"claude", "opencode"}

	var out bytes.Buffer
	p := New(strings.NewReader("2\n"), &out)
	if got := p.AskChoice("command", opts, "claude"); got != "opencode" {
		t.Errorf("picking 2 should give opencode, got %q", got)
	}

	// Models and CLIs change faster than any hardcoded list, so typing
	// something not on the menu has to work.
	out.Reset()
	p = New(strings.NewReader("some-new-agent\n"), &out)
	if got := p.AskChoice("command", opts, "claude"); got != "some-new-agent" {
		t.Errorf("typing a name should be accepted, got %q", got)
	}
}
