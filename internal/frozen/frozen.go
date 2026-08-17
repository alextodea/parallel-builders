// Package frozen enforces the rule the whole tool rests on: once the architect
// has written the test suite, no builder may modify it.
//
// This is deliberately a check and not a prompt instruction. A builder told
// "make the tests pass" will, given the chance, weaken an assertion or skip a
// case — and a green run looks identical either way. The prompt asks; this
// enforces.
package frozen

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Guard reports whether a set of changed files violates the freeze.
type Guard struct {
	// Patterns are filepath.Match globs identifying test files,
	// e.g. "**/*_test.go" or "tests/**".
	Patterns []string
}

// Violation names a file a builder was not allowed to touch.
type Violation struct {
	File    string
	Pattern string
}

func (v Violation) Error() string {
	return fmt.Sprintf("modified frozen test file %s (matched %q)", v.File, v.Pattern)
}

// Check returns every changed file that matches a frozen pattern. A candidate
// with any violation is disqualified outright — it is not ranked against the
// others, because its passing tests no longer mean anything.
func (g Guard) Check(changed []string) []Violation {
	var out []Violation
	for _, f := range changed {
		if p, ok := g.match(f); ok {
			out = append(out, Violation{File: f, Pattern: p})
		}
	}
	return out
}

// Owns reports whether a path is part of the frozen suite. The architect's own
// diff is checked with this too, inverted: an architect that wrote anything
// outside these paths has written implementation, which it was told not to do.
func (g Guard) Owns(path string) bool {
	_, ok := g.match(path)
	return ok
}

func (g Guard) match(path string) (string, bool) {
	path = filepath.ToSlash(path)
	for _, pat := range g.Patterns {
		if matchGlob(pat, path) {
			return pat, true
		}
	}
	return "", false
}

// matchGlob supports a leading "**/" meaning "at any depth", which
// filepath.Match does not handle on its own.
func matchGlob(pattern, path string) bool {
	if rest, found := strings.CutPrefix(pattern, "**/"); found {
		// Try every suffix of the path so "**/*_test.go" matches
		// "internal/api/handler_test.go".
		for {
			if ok, _ := filepath.Match(rest, path); ok {
				return true
			}
			_, after, found := strings.Cut(path, "/")
			if !found {
				return false
			}
			path = after
		}
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	ok, _ := filepath.Match(pattern, path)
	return ok
}
