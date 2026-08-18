// Package brief is the specification as data.
//
// A brief is what intake produces, what you approve, what the architect turns
// into tests, and what the pull request is written from. It is deliberately
// pure: no terminal, no model, no network. That is what makes the most
// important rules here testable without spending anything.
//
// The load-bearing property is criterion identity. Every generated test names
// the criterion it satisfies, so an id is a reference that outlives the file
// it was written in. Reusing a retired id silently repoints a test at a
// different requirement — the kind of defect that produces a green run and a
// wrong answer, which is exactly what this whole tool exists to prevent.
package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Source records where a criterion came from. Provenance is not decoration:
// a criterion the user typed and approved is binding, while one derived from
// the codebase is a reasonable guess. Downstream phases are allowed to
// question a guess and are not allowed to quietly overrule an approval.
type Source string

const (
	// Asked came from a question the user answered.
	Asked Source = "asked"
	// Derived was read out of the repository.
	Derived Source = "derived"
	// Assumed was neither asked nor derived — stated so it can be objected to.
	Assumed Source = "assumed"
)

func (s Source) Valid() bool {
	switch s {
	case Asked, Derived, Assumed:
		return true
	}
	return false
}

// Binding reports whether downstream phases must treat this as settled.
func (s Source) Binding() bool { return s == Asked }

// Criterion is one acceptance criterion. Exactly one test satisfies it.
type Criterion struct {
	// ID is stable for the life of the brief. It is never renumbered, and a
	// deleted id is never reissued.
	ID     string `toml:"id"`
	Text   string `toml:"text"`
	Source Source `toml:"source"`
}

type Example struct {
	// Request and Response are repo-relative paths. Absolute paths, and
	// anything resolving outside the repository, are rejected.
	Request  string `toml:"request,omitempty"`
	Response string `toml:"response,omitempty"`
	Note     string `toml:"note,omitempty"`
}

type Assumption struct {
	Text   string `toml:"text"`
	Source Source `toml:"source"`
}

type Surface struct {
	Kind   string   `toml:"kind,omitempty"`
	Routes []string `toml:"routes,omitempty"`
	Entry  string   `toml:"entry,omitempty"`
}

type Input struct {
	Name     string `toml:"name"`
	From     string `toml:"from,omitempty"`
	Type     string `toml:"type,omitempty"`
	Required bool   `toml:"required"`
}

// Brief is the whole specification.
//
// Note what is absent: no prompt version. A brief outlives many runs, each of
// which may use a different template version, so that belongs on the run
// record rather than here.
type Brief struct {
	Feature string `toml:"feature"`
	Slug    string `toml:"slug"`
	// Base is the commit the brief was written against. Recorded so a stale
	// brief can be detected rather than silently applied to moved code.
	Base string `toml:"base,omitempty"`

	Surface Surface `toml:"surface,omitempty"`
	Inputs  []Input `toml:"inputs,omitempty"`

	Criteria    []Criterion  `toml:"criteria"`
	Examples    []Example    `toml:"examples,omitempty"`
	OutOfScope  []string     `toml:"out_of_scope,omitempty"`
	Assumptions []Assumption `toml:"assumptions,omitempty"`
	// Constraints accumulate from disagreement rounds during intake.
	Constraints []string `toml:"constraints,omitempty"`

	// nextID tracks the high-water mark so ids are never reissued even after
	// deletions. Persisted, because the information cannot be recovered from
	// the surviving criteria alone.
	NextID int `toml:"next_id"`
}

// AddCriterion appends a criterion with a fresh id and returns it.
func (b *Brief) AddCriterion(text string, src Source) Criterion {
	if b.NextID < 1 {
		b.NextID = b.highWater() + 1
	}
	c := Criterion{
		ID:     "C" + strconv.Itoa(b.NextID),
		Text:   text,
		Source: src,
	}
	b.NextID++
	b.Criteria = append(b.Criteria, c)
	return c
}

// RemoveCriterion deletes by id. The id is retired, never reissued: NextID is
// untouched, so the sequence keeps a gap where the criterion used to be.
func (b *Brief) RemoveCriterion(id string) bool {
	for i, c := range b.Criteria {
		if c.ID == id {
			b.Criteria = append(b.Criteria[:i], b.Criteria[i+1:]...)
			return true
		}
	}
	return false
}

// Criterion looks one up by id.
func (b Brief) Criterion(id string) (Criterion, bool) {
	for _, c := range b.Criteria {
		if c.ID == id {
			return c, true
		}
	}
	return Criterion{}, false
}

// IDs returns every criterion id in file order.
func (b Brief) IDs() []string {
	out := make([]string, 0, len(b.Criteria))
	for _, c := range b.Criteria {
		out = append(out, c.ID)
	}
	return out
}

// highWater is the largest numeric id ever seen among current criteria. Only
// used to repair a brief whose NextID is missing — a hand-written file, or one
// from before the field existed.
func (b Brief) highWater() int {
	max := 0
	for _, c := range b.Criteria {
		if n, ok := parseID(c.ID); ok && n > max {
			max = n
		}
	}
	return max
}

func parseID(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "C")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// Validate checks everything that does not require the filesystem.
func (b Brief) Validate() error {
	if strings.TrimSpace(b.Feature) == "" {
		return fmt.Errorf("feature is empty — a brief with no description cannot be specified")
	}
	if len(b.Criteria) == 0 {
		return fmt.Errorf("no criteria — a brief nothing can be graded against is not a brief")
	}

	seen := map[string]bool{}
	for i, c := range b.Criteria {
		if !isValidID(c.ID) {
			return fmt.Errorf("criteria[%d]: id %q is not of the form C1, C2, …", i, c.ID)
		}
		if seen[c.ID] {
			// Last-write-wins here would silently drop a requirement, so a
			// hand-edited duplicate has to be an error.
			return fmt.Errorf("criteria[%d]: duplicate id %q", i, c.ID)
		}
		seen[c.ID] = true

		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("criteria[%s]: text is empty", c.ID)
		}
		if !c.Source.Valid() {
			return fmt.Errorf("criteria[%s]: source %q is not one of asked, derived, assumed", c.ID, c.Source)
		}
		if n, _ := parseID(c.ID); n >= b.NextID && b.NextID > 0 {
			return fmt.Errorf("criteria[%s]: id is at or beyond next_id (%d) — the file has been edited in a way that would reissue it", c.ID, b.NextID)
		}
	}

	for i, a := range b.Assumptions {
		if strings.TrimSpace(a.Text) == "" {
			return fmt.Errorf("assumptions[%d]: text is empty", i)
		}
		if !a.Source.Valid() {
			return fmt.Errorf("assumptions[%d]: invalid source %q", i, a.Source)
		}
	}

	for i, e := range b.Examples {
		if e.Request == "" && e.Response == "" && e.Note == "" {
			return fmt.Errorf("examples[%d]: empty", i)
		}
	}
	return nil
}

func isValidID(id string) bool {
	_, ok := parseID(id)
	return ok
}

// CheckExamples verifies every example path stays inside the repository.
//
// Separate from Validate because it needs the filesystem: symlinks are only
// resolvable against a real tree, and a path that looks fine textually can
// still point at ~/.ssh through a link.
func (b Brief) CheckExamples(repoRoot string) error {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return fmt.Errorf("repo root %s: %w", repoRoot, err)
	}
	for i, e := range b.Examples {
		for _, p := range []string{e.Request, e.Response} {
			if p == "" {
				continue
			}
			if err := checkPath(root, p); err != nil {
				return fmt.Errorf("examples[%d]: %w", i, err)
			}
		}
	}
	return nil
}

func checkPath(root, p string) error {
	if filepath.IsAbs(p) {
		return fmt.Errorf("%s: absolute paths are not allowed; use a repo-relative path", p)
	}

	// Lexical containment first, before touching the filesystem. Resolving
	// symlinks up front means a non-existent escape like ../../../etc/shadow
	// fails as "no such file", which reads like a typo rather than the
	// attempted escape it is — and on a machine where the file does exist the
	// two cases would report differently for the same input.
	full := filepath.Join(root, p)
	if !within(root, full) {
		return fmt.Errorf("%s: resolves outside the repository", p)
	}

	// Then the filesystem, which is the only way to catch a link inside the
	// repo pointing out of it.
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: no such file", p)
		}
		return fmt.Errorf("%s: %w", p, err)
	}
	if !within(root, resolved) {
		return fmt.Errorf("%s: resolves outside the repository (%s)", p, resolved)
	}
	return nil
}

// within reports whether path is root or lives under it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Load reads and validates a brief.
func Load(path string) (Brief, error) {
	var b Brief
	if _, err := toml.DecodeFile(path, &b); err != nil {
		if os.IsNotExist(err) {
			return b, fmt.Errorf("no brief at %s", path)
		}
		return b, fmt.Errorf("%s: %w", path, err)
	}
	// Repair a hand-written file that omitted next_id, so it still cannot
	// reissue an id that is currently in use.
	if b.NextID <= b.highWater() {
		b.NextID = b.highWater() + 1
	}
	return b, b.Validate()
}

// Save writes a brief, creating parent directories as needed.
func (b Brief) Save(path string) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "# %s\n# Written by pb intake. Criterion ids are referenced by tests —\n# edit the text freely, never the ids.\n\n", b.Feature); err != nil {
		return err
	}
	return toml.NewEncoder(f).Encode(b)
}

// Slugify turns a feature description into a filename-safe slug.
//
// Non-ASCII is dropped rather than turned into a separator, so "ünïcode"
// becomes "ncode" instead of "n-code". Both are mangled; the point is only
// that the result is deterministic and safe as a filename.
//
// It never returns the empty string. A description written entirely in a
// non-Latin script would otherwise slug to nothing, and the caller would
// happily build a path ending in a bare separator.
func Slugify(s string) string {
	const fallback = "feature"

	var out []rune
	pendingDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if pendingDash && len(out) > 0 {
				out = append(out, '-')
			}
			pendingDash = false
			out = append(out, r)
		case r == ' ', r == '\t', r == '\n', r == '-', r == '_', r == '/', r == '.', r == ',', r == ':':
			pendingDash = true
		default:
			// Any other rune — punctuation, emoji, non-Latin letters — is
			// dropped without introducing a separator.
		}
		if len(out) >= 48 {
			break
		}
	}

	slug := strings.Trim(string(out), "-")
	if slug == "" {
		return fallback
	}
	return slug
}

// Markdown renders the brief for a human: the file you actually read before
// approving, and the body a pull request is written from.
func (b Brief) Markdown() string {
	var s strings.Builder

	fmt.Fprintf(&s, "# %s\n\n", b.Feature)

	if b.Surface.Kind != "" || len(b.Surface.Routes) > 0 {
		s.WriteString("## Surface\n\n")
		if b.Surface.Kind != "" {
			fmt.Fprintf(&s, "- **kind** — %s\n", b.Surface.Kind)
		}
		if b.Surface.Entry != "" {
			fmt.Fprintf(&s, "- **entry** — `%s`\n", b.Surface.Entry)
		}
		for _, r := range b.Surface.Routes {
			fmt.Fprintf(&s, "- `%s`\n", r)
		}
		s.WriteString("\n")
	}

	if len(b.Inputs) > 0 {
		s.WriteString("## Inputs\n\n| name | from | type | required |\n|---|---|---|---|\n")
		for _, i := range b.Inputs {
			fmt.Fprintf(&s, "| `%s` | %s | %s | %t |\n", i.Name, dash(i.From), dash(i.Type), i.Required)
		}
		s.WriteString("\n")
	}

	// The criteria are what gets approved, so they lead.
	s.WriteString("## Acceptance criteria\n\n")
	s.WriteString("Each becomes exactly one test.\n\n")
	for _, c := range b.Criteria {
		mark := ""
		if !c.Source.Binding() {
			mark = fmt.Sprintf(" _(%s)_", c.Source)
		}
		fmt.Fprintf(&s, "- **%s** — %s%s\n", c.ID, oneLine(c.Text), mark)
	}
	s.WriteString("\n")

	if len(b.Examples) > 0 {
		s.WriteString("## Examples\n\n")
		for _, e := range b.Examples {
			switch {
			case e.Request != "" && e.Response != "":
				fmt.Fprintf(&s, "- `%s` → `%s`", e.Request, e.Response)
			case e.Request != "":
				fmt.Fprintf(&s, "- `%s`", e.Request)
			case e.Response != "":
				fmt.Fprintf(&s, "- `%s`", e.Response)
			}
			if e.Note != "" {
				fmt.Fprintf(&s, " — %s", e.Note)
			}
			s.WriteString("\n")
		}
		s.WriteString("\n")
	}

	if len(b.OutOfScope) > 0 {
		s.WriteString("## Out of scope\n\n")
		for _, o := range b.OutOfScope {
			fmt.Fprintf(&s, "- %s\n", o)
		}
		s.WriteString("\n")
	}

	if len(b.Assumptions) > 0 {
		s.WriteString("## Assumptions\n\nNot asked. Object to any of these and they become criteria.\n\n")
		for _, a := range b.Assumptions {
			fmt.Fprintf(&s, "- %s _(%s)_\n", oneLine(a.Text), a.Source)
		}
		s.WriteString("\n")
	}

	if len(b.Constraints) > 0 {
		s.WriteString("## Added constraints\n\nFrom corrections during intake.\n\n")
		for _, c := range b.Constraints {
			fmt.Fprintf(&s, "- %s\n", oneLine(c))
		}
		s.WriteString("\n")
	}

	if b.Base != "" {
		fmt.Fprintf(&s, "---\n\nWritten against `%s`.\n", shortSHA(b.Base))
	}
	return s.String()
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// oneLine flattens embedded newlines so a multi-line criterion cannot break
// out of a markdown list item.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// Sorted returns criteria ordered by numeric id rather than file order.
func (b Brief) Sorted() []Criterion {
	out := append([]Criterion(nil), b.Criteria...)
	sort.Slice(out, func(i, j int) bool {
		a, _ := parseID(out[i].ID)
		c, _ := parseID(out[j].ID)
		return a < c
	})
	return out
}
