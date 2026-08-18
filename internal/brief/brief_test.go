package brief

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sample() Brief {
	b := Brief{Feature: "rate limiting on public API routes", Slug: "rate-limit"}
	b.AddCriterion("returns 429 with Retry-After once the limit is exceeded", Asked)
	b.AddCriterion("counts per API key, not per IP", Asked)
	b.AddCriterion("applies to all 14 routes under /api/v1/*", Derived)
	return b
}

func TestIDsAreNeverReissued(t *testing.T) {
	b := sample() // C1 C2 C3

	if got := b.IDs(); len(got) != 3 || got[0] != "C1" || got[2] != "C3" {
		t.Fatalf("initial ids = %v", got)
	}

	// Delete the middle one. The gap must persist: a test somewhere may
	// still carry `// pb:C2`, and handing that id to a different
	// requirement would silently repoint it.
	if !b.RemoveCriterion("C2") {
		t.Fatal("RemoveCriterion(C2) returned false")
	}
	next := b.AddCriterion("fails closed when the store is unreachable", Asked)
	if next.ID != "C4" {
		t.Fatalf("after deleting C2 the next id must be C4, got %s", next.ID)
	}

	// And again, to be sure the high-water mark is not recomputed from the
	// surviving set.
	b.RemoveCriterion("C4")
	if got := b.AddCriterion("window resets after 60s", Asked); got.ID != "C5" {
		t.Fatalf("expected C5, got %s", got.ID)
	}
}

func TestRoundTripPreservesEverything(t *testing.T) {
	b := sample()
	b.Base = "9f8e7d6a5b4c3d2e1f"
	b.OutOfScope = []string{"admin routes", "cross-instance coordination"}
	b.Constraints = []string{"must not block on I/O"}
	b.Assumptions = []Assumption{{Text: "single instance, in-process counter", Source: Assumed}}
	b.Inputs = []Input{{Name: "X-API-Key", From: "header", Required: true}}
	b.Surface = Surface{Kind: "http-middleware", Routes: []string{"/api/v1/*"}}

	// Awkward text: newlines, a colon, a leading dash, unicode, quotes.
	b.AddCriterion("rejects a request whose body exceeds 1 MiB:\n  - responds 413\n  - logs “too large” once", Asked)

	dir := t.TempDir()
	path := filepath.Join(dir, "brief.toml")
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Feature != b.Feature || got.Base != b.Base || got.NextID != b.NextID {
		t.Errorf("scalars differ:\n got %+v\nwant %+v", got, b)
	}
	if len(got.Criteria) != len(b.Criteria) {
		t.Fatalf("criteria count = %d, want %d", len(got.Criteria), len(b.Criteria))
	}
	for i := range b.Criteria {
		if got.Criteria[i] != b.Criteria[i] {
			t.Errorf("criteria[%d]:\n got %+v\nwant %+v", i, got.Criteria[i], b.Criteria[i])
		}
	}
	if len(got.Constraints) != 1 || got.Constraints[0] != "must not block on I/O" {
		t.Errorf("constraints lost: %v", got.Constraints)
	}
}

func TestProvenanceSurvivesRoundTrip(t *testing.T) {
	// A criterion the user approved must not come back as a guess. That
	// downgrade is what allows a later phase to overrule a settled decision.
	b := Brief{Feature: "f"}
	b.AddCriterion("approved by the user", Asked)
	b.AddCriterion("read from the codebase", Derived)

	path := filepath.Join(t.TempDir(), "b.toml")
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.Criteria[0].Source != Asked || !got.Criteria[0].Source.Binding() {
		t.Errorf("C1 lost its binding provenance: %q", got.Criteria[0].Source)
	}
	if got.Criteria[1].Source != Derived || got.Criteria[1].Source.Binding() {
		t.Errorf("C2 provenance wrong: %q", got.Criteria[1].Source)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Brief)
		want string // substring; "" means valid
	}{
		{"valid", func(b *Brief) {}, ""},
		{"no feature", func(b *Brief) { b.Feature = "  " }, "feature is empty"},
		{"no criteria", func(b *Brief) { b.Criteria = nil }, "no criteria"},
		{"duplicate id", func(b *Brief) {
			b.Criteria = append(b.Criteria, Criterion{ID: "C1", Text: "again", Source: Asked})
		}, "duplicate id"},
		{"malformed id", func(b *Brief) { b.Criteria[0].ID = "first" }, "not of the form"},
		{"empty text", func(b *Brief) { b.Criteria[0].Text = "\n\t " }, "text is empty"},
		{"bad source", func(b *Brief) { b.Criteria[0].Source = "guessed" }, "not one of"},
		{"id beyond next_id", func(b *Brief) {
			// A hand edit that would let the id be reissued.
			b.Criteria = append(b.Criteria, Criterion{ID: "C99", Text: "x", Source: Asked})
		}, "beyond next_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := sample()
			tc.mut(&b)
			err := b.Validate()
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("expected valid, got %v", err)
			case tc.want != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", tc.want)
			case tc.want != "" && !strings.Contains(err.Error(), tc.want):
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestCheckExamplesRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "testdata", "req.json")
	if err := os.WriteFile(inside, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// A secret living outside the repo, and a symlink inside pointing at it.
	outside := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(outside, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "testdata", "innocent.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cases := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"inside the repo", "testdata/req.json", ""},
		{"absolute", outside, "absolute paths"},
		{"dot-dot escape", "../../../etc/passwd", "outside the repository"},
		{"missing file", "testdata/nope.json", "no such file"},
		{"symlink pointing out", "testdata/innocent.json", "outside the repository"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := sample()
			b.Examples = []Example{{Request: tc.path}}

			err := b.CheckExamples(root)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("expected ok, got %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadRepairsMissingNextID(t *testing.T) {
	// A hand-written brief with no next_id must not be able to reissue an id
	// that is already in use.
	path := filepath.Join(t.TempDir(), "hand.toml")
	os.WriteFile(path, []byte(`
feature = "hand written"
[[criteria]]
id = "C1"
text = "one"
source = "asked"
[[criteria]]
id = "C7"
text = "seven"
source = "asked"
`), 0o644)

	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.NextID != 8 {
		t.Fatalf("NextID = %d, want 8 (past the highest existing id)", b.NextID)
	}
	if got := b.AddCriterion("eight", Asked); got.ID != "C8" {
		t.Fatalf("next id = %s, want C8", got.ID)
	}
}

func TestMarkdownIsSafeAndComplete(t *testing.T) {
	b := sample()
	b.AddCriterion("handles a body over 1 MiB:\nresponds 413", Asked)
	b.OutOfScope = []string{"admin routes"}
	b.Assumptions = []Assumption{{Text: "single instance", Source: Assumed}}
	b.Base = "9f8e7d6a5b4c"

	md := b.Markdown()

	for _, want := range []string{"C1", "C2", "C3", "C4", "Out of scope", "admin routes", "Assumptions", "9f8e7d6a"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
	// A multi-line criterion must not break out of its list item.
	for _, line := range strings.Split(md, "\n") {
		if strings.Contains(line, "responds 413") && !strings.HasPrefix(line, "- **C4**") {
			t.Errorf("multi-line criterion escaped its bullet: %q", line)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"add rate limiting to the public API routes": "add-rate-limiting-to-the-public-api-routes",
		"  Fix the /api/v1/* handler!  ":             "fix-the-api-v1-handler",
		"emoji 🎉 and ünïcode":                        "emoji-and-ncode",
		// Never empty: a description in a non-Latin script would otherwise
		// slug to nothing and the caller would build a path ending in a
		// bare separator.
		"!!!":    "feature",
		"添加速率限制": "feature",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
