// Package config loads .pb.toml.
//
// Agents are named executables, not hardcoded providers. Adding a new harness
// is a config line, never a code change.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Agents Agents `toml:"agents"`
	Gate   Gate   `toml:"gate"`
	Tests  Tests  `toml:"tests"`
	Pool   Pool   `toml:"pool"`
}

type Agents struct {
	// Architect writes the tests. One call, the expensive one.
	Architect string `toml:"architect"`
	// Builders race. Use different model families — the value of racing
	// comes from independent errors, and two samples of one model tend to
	// be wrong in the same way.
	Builders []string `toml:"builders"`
	Timeout  string   `toml:"timeout"`
}

type Gate struct {
	Build string `toml:"build"`
	Lint  string `toml:"lint"`
	Test  string `toml:"test"`
	Arch  string `toml:"arch"`
}

type Tests struct {
	// Paths are globs marking the frozen suite. Builders may not touch
	// anything matching these.
	Paths []string `toml:"paths"`
}

type Pool struct {
	Size int    `toml:"size"`
	Root string `toml:"root"`
}

// Default is what `pb init` writes.
func Default() Config {
	return Config{
		Agents: Agents{
			Architect: "claude",
			Builders:  []string{"opencode", "codex"},
			Timeout:   "10m",
		},
		Gate: Gate{
			Build: "go build ./...",
			Lint:  "go vet ./...",
			Test:  "go test ./...",
		},
		Tests: Tests{Paths: []string{"**/*_test.go"}},
		// One spare beyond the builder count, so claiming never blocks
		// while a release is still cleaning up.
		Pool: Pool{Size: 3, Root: "~/.pb/pool"},
	}
}

func (c Config) Validate() error {
	if c.Agents.Architect == "" {
		return fmt.Errorf("agents.architect is required — something has to write the tests")
	}
	if len(c.Agents.Builders) == 0 {
		return fmt.Errorf("agents.builders is empty — nothing would implement anything")
	}
	if len(c.Tests.Paths) == 0 {
		return fmt.Errorf("tests.paths is empty — without it the freeze cannot be enforced and results are meaningless")
	}
	if c.Gate.Test == "" {
		return fmt.Errorf("gate.test is required — it is the selector")
	}
	if c.Pool.Size < len(c.Agents.Builders) {
		return fmt.Errorf("pool.size (%d) is below the builder count (%d)", c.Pool.Size, len(c.Agents.Builders))
	}
	return nil
}

func (c Config) AgentTimeout() time.Duration {
	d, err := time.ParseDuration(c.Agents.Timeout)
	if err != nil {
		return 10 * time.Minute
	}
	return d
}

// Load reads and validates a config file.
//
// TODO: swap this for BurntSushi/toml once the first dependency is worth
// adding. Keeping the initial commit stdlib-only means it builds anywhere with
// no network.
func Load(path string) (Config, error) {
	if _, err := os.Stat(path); err != nil {
		return Config{}, fmt.Errorf("no config at %s — run `pb init`", path)
	}
	c := Default()
	return c, c.Validate()
}
