// Package config is the answer to "which agents, against which repo, checked
// how".
//
// Agents are named executables plus arguments, never hardcoded providers.
// Adding a harness or swapping a model is a config edit, never a code change —
// and because the same CLI can appear twice with different --model flags, each
// builder carries its own Name for the run records.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// FileName is what `pb init` writes and every other command reads.
const FileName = ".pb.toml"

type Config struct {
	// Repo is "owner/name", recorded so a run can be traced back to where
	// its commits landed.
	Repo string `toml:"repo"`

	Architect Agent   `toml:"architect"`
	Builders  []Agent `toml:"builders"`

	Gate  Gate  `toml:"gate"`
	Tests Tests `toml:"tests"`
	Pool  Pool  `toml:"pool"`
}

// Agent is one runnable coding agent.
type Agent struct {
	// Name distinguishes two builders that share a Cmd but differ by
	// model. It is what appears in run records and in the bench table.
	Name string   `toml:"name,omitempty"`
	Cmd  string   `toml:"cmd"`
	Args []string `toml:"args,omitempty"`

	Timeout string `toml:"timeout,omitempty"`
}

func (a Agent) Label() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Cmd
}

func (a Agent) Duration() time.Duration {
	d, err := time.ParseDuration(a.Timeout)
	if err != nil {
		return 10 * time.Minute
	}
	return d
}

// Gate is the deterministic check, in the order it runs. Cheapest first, so a
// candidate that will not compile never costs a test-suite run.
type Gate struct {
	Build string `toml:"build"`
	Lint  string `toml:"lint"`
	Test  string `toml:"test"`
	Arch  string `toml:"arch,omitempty"`
}

type Tests struct {
	// Paths are globs marking the frozen suite. Builders may not touch
	// anything matching these; without it the whole design is decorative.
	Paths []string `toml:"paths"`
}

type Pool struct {
	Size int    `toml:"size"`
	Root string `toml:"root"`
}

func (c Config) Validate() error {
	if c.Architect.Cmd == "" {
		return fmt.Errorf("architect.cmd is required — something has to write the tests")
	}
	if len(c.Builders) == 0 {
		return fmt.Errorf("no builders configured — nothing would implement anything")
	}
	seen := map[string]bool{}
	for i, b := range c.Builders {
		if b.Cmd == "" {
			return fmt.Errorf("builders[%d].cmd is empty", i)
		}
		l := b.Label()
		if seen[l] {
			return fmt.Errorf("two builders share the name %q — give them distinct names or their records are indistinguishable", l)
		}
		seen[l] = true
	}
	if len(c.Tests.Paths) == 0 {
		return fmt.Errorf("tests.paths is empty — the freeze cannot be enforced and results would be meaningless")
	}
	if c.Gate.Test == "" {
		return fmt.Errorf("gate.test is required — it is the selector")
	}
	if c.Pool.Size < len(c.Builders) {
		return fmt.Errorf("pool.size is %d but there are %d builders", c.Pool.Size, len(c.Builders))
	}
	return nil
}

// Load reads and validates a config file.
func Load(path string) (Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		if os.IsNotExist(err) {
			return c, fmt.Errorf("no %s here — run `pb init`", filepath.Base(path))
		}
		return c, err
	}
	return c, c.Validate()
}

// Save writes a config file, refusing to clobber an existing one.
func Save(path string, c Config) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(path))
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "# %s — written by `pb init`\n\n", filepath.Base(path)); err != nil {
		return err
	}
	return toml.NewEncoder(f).Encode(c)
}

// DetectGate guesses the gate commands from what is in the directory. A guess
// the user corrects beats an empty file they have to fill in from nothing.
func DetectGate(dir string) (Gate, Tests, string) {
	switch {
	case exists(filepath.Join(dir, "go.mod")):
		return Gate{
			Build: "go build ./...",
			Lint:  "go vet ./...",
			Test:  "go test ./...",
		}, Tests{Paths: []string{"**/*_test.go"}}, "go"

	case exists(filepath.Join(dir, "package.json")):
		return Gate{
			Build: "npm run build --if-present",
			Lint:  "npm run lint --if-present",
			Test:  "npm test",
		}, Tests{Paths: []string{"**/*.test.ts", "**/*.test.js", "**/*.spec.ts"}}, "node"

	case exists(filepath.Join(dir, "pyproject.toml")), exists(filepath.Join(dir, "requirements.txt")):
		return Gate{
			Lint: "ruff check .",
			Test: "pytest",
		}, Tests{Paths: []string{"**/test_*.py", "**/*_test.py", "tests/**"}}, "python"

	case exists(filepath.Join(dir, "Cargo.toml")):
		return Gate{
			Build: "cargo build",
			Lint:  "cargo clippy",
			Test:  "cargo test",
		}, Tests{Paths: []string{"**/tests/**", "**/*_test.rs"}}, "rust"
	}
	return Gate{}, Tests{}, ""
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ExpandHome turns a leading ~ into the user's home directory.
func ExpandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
