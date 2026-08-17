// Package setup is the interactive half of `pb init`.
//
// It is deliberately separate from package config: config is data and can be
// tested without a terminal, this reads from one.
package setup

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// KnownAgents are the CLIs pb looks for on PATH. Absence is not an error —
// they are offered as defaults, and anything runnable can be typed instead.
var KnownAgents = []string{"claude", "codex", "opencode", "pi", "copilot", "cursor-agent"}

// Installed returns the subset of KnownAgents present on PATH.
func Installed() []string {
	var found []string
	for _, a := range KnownAgents {
		if _, err := exec.LookPath(a); err == nil {
			found = append(found, a)
		}
	}
	return found
}

// GitRemote returns "owner/name" parsed from origin, or "" if there isn't one.
// Both SSH and HTTPS remotes are handled.
func GitRemote(dir string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	url = strings.TrimSuffix(url, ".git")

	if _, after, ok := strings.Cut(url, "github.com:"); ok { // git@github.com:owner/name
		return after
	}
	if _, after, ok := strings.Cut(url, "github.com/"); ok { // https://github.com/owner/name
		return after
	}
	return ""
}

// Prompter asks questions on a terminal.
type Prompter struct {
	In  *bufio.Reader
	Out io.Writer
}

func New(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{In: bufio.NewReader(in), Out: out}
}

func (p *Prompter) Say(format string, a ...any) {
	fmt.Fprintf(p.Out, format+"\n", a...)
}

// Ask returns the typed answer, or def if the user just pressed enter.
func (p *Prompter) Ask(question, def string) string {
	if def != "" {
		fmt.Fprintf(p.Out, "  %s [%s]: ", question, def)
	} else {
		fmt.Fprintf(p.Out, "  %s: ", question)
	}
	line, err := p.In.ReadString('\n')
	if err != nil && line == "" {
		return def
	}
	if v := strings.TrimSpace(line); v != "" {
		return v
	}
	return def
}

// AskInt reprompts until the answer parses and sits within [min, max].
func (p *Prompter) AskInt(question string, def, min, max int) int {
	for {
		raw := p.Ask(question, strconv.Itoa(def))
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			p.Say("    not a number")
			continue
		}
		if n < min || n > max {
			p.Say("    must be between %d and %d", min, max)
			continue
		}
		return n
	}
}

// AskChoice offers a numbered list. The user may pick a number, or type
// something else entirely — models change faster than any hardcoded list.
func (p *Prompter) AskChoice(question string, options []string, def string) string {
	if len(options) > 0 {
		for i, o := range options {
			p.Say("    %d) %s", i+1, o)
		}
	}
	for {
		raw := p.Ask(question, def)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return def
		}
		if n, err := strconv.Atoi(raw); err == nil {
			if n >= 1 && n <= len(options) {
				return options[n-1]
			}
			p.Say("    no option %d", n)
			continue
		}
		return raw // typed a command name directly
	}
}

// AskRepo reprompts until the answer looks like "owner/name". Without this a
// stray keystroke becomes your repo field and nothing complains until much
// later, when a run record points at nothing.
func (p *Prompter) AskRepo(question, def string) string {
	for {
		v := strings.TrimSpace(p.Ask(question, def))
		if ValidRepo(v) {
			return v
		}
		p.Say("    expected owner/name, e.g. alextodea/parallel-builders")
	}
}

// ValidRepo reports whether s is a plausible "owner/name".
func ValidRepo(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	if !ok || owner == "" || name == "" {
		return false
	}
	if strings.Contains(name, "/") {
		return false
	}
	for _, part := range []string{owner, name} {
		for _, r := range part {
			isOK := r == '-' || r == '_' || r == '.' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !isOK {
				return false
			}
		}
	}
	return true
}

// Confirm defaults to yes. It writes its own prompt rather than going through
// Ask, which would render the default twice: "Correct? [Y/n] [y]:".
func (p *Prompter) Confirm(question string) bool {
	fmt.Fprintf(p.Out, "  %s [Y/n]: ", question)
	line, err := p.In.ReadString('\n')
	if err != nil && line == "" {
		return true
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "" || strings.HasPrefix(ans, "y")
}
