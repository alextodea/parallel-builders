package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/alextodea/parallel-builders/internal/config"
	"github.com/alextodea/parallel-builders/internal/setup"
)

// cmdInit walks the user through creating a .pb.toml.
//
// Run it inside the repo you want pb to work on — like `git init`. The repo it
// found is shown for confirmation rather than picked from a list, which keeps
// pb out of the business of cloning things.
func cmdInit(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists — delete it to start over", path)
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	p := setup.New(os.Stdin, os.Stdout)
	cfg := config.Config{}

	// ---- 1. which repo -------------------------------------------------
	p.Say("\nrepository")
	remote := setup.GitRemote(wd)
	if remote == "" {
		p.Say("    no github origin found here")
		cfg.Repo = p.AskRepo("owner/name", "")
	} else {
		cfg.Repo = p.AskRepo("owner/name", remote)
	}

	gate, tests, lang := config.DetectGate(wd)
	if lang != "" {
		p.Say("    detected a %s project", lang)
	}

	// ---- 2. the architect ----------------------------------------------
	installed := setup.Installed()
	p.Say("\narchitect — writes the tests, one expensive call per feature")
	if len(installed) == 0 {
		p.Say("    no known agent CLIs found on PATH; type a command anyway")
	}
	arch := p.AskChoice("command", installed, first(installed, "claude"))
	cfg.Architect = config.Agent{
		Cmd:     arch,
		Args:    splitArgs(p.Ask("extra args (e.g. --model opus-5)", "")),
		Timeout: "10m",
	}

	// ---- 3. the builders -----------------------------------------------
	p.Say("\nbuilders — implement against the frozen tests, in parallel")
	p.Say("    two different model families beats two runs of one: the value")
	p.Say("    of racing comes from mistakes that are independent")
	n := p.AskInt("how many", 2, 1, 10)
	if n > 5 {
		p.Say("    note: past ~5 the success curve flattens and you pay for")
		p.Say("    every discarded candidate. `pb bench` can tell you where")
		p.Say("    the returns stop.")
	}

	for i := range n {
		p.Say("\n  builder %d", i+1)
		cmd := p.AskChoice("command", installed, first(installed, "opencode"))
		args := splitArgs(p.Ask("extra args (e.g. --model gpt-5.3)", ""))
		name := p.Ask("short name for records", suggestName(cmd, args, i))
		cfg.Builders = append(cfg.Builders, config.Agent{
			Name: name, Cmd: cmd, Args: args, Timeout: "10m",
		})
	}

	// ---- 4. the gate ---------------------------------------------------
	p.Say("\ngate — deterministic, runs in order, stops at the first failure")
	cfg.Gate = config.Gate{
		Build: p.Ask("build", gate.Build),
		Lint:  p.Ask("lint", gate.Lint),
		Test:  p.Ask("test", gate.Test),
	}

	p.Say("\nfrozen tests — builders may not edit anything matching these")
	cfg.Tests = config.Tests{
		Paths: splitArgs(p.Ask("globs (space separated)", strings.Join(tests.Paths, " "))),
	}

	// One spare beyond the builder count so claiming never blocks while a
	// release is still cleaning up.
	cfg.Pool = config.Pool{Size: n + 1, Root: "~/.pb/pool"}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("that configuration is not usable: %w", err)
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}

	p.Say("\n  wrote %s", path)
	p.Say("    repo       %s", cfg.Repo)
	p.Say("    architect  %s", strings.TrimSpace(cfg.Architect.Cmd+" "+strings.Join(cfg.Architect.Args, " ")))
	for _, b := range cfg.Builders {
		p.Say("    builder    %-10s %s", b.Label(), strings.TrimSpace(b.Cmd+" "+strings.Join(b.Args, " ")))
	}
	p.Say("\n  next: pb run \"describe the feature you want\"")
	return nil
}

func splitArgs(s string) []string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return nil
	}
	return f
}

func first(list []string, fallback string) string {
	if len(list) > 0 {
		return list[0]
	}
	return fallback
}

// suggestName prefers the model over the CLI, since two builders often share a
// CLI and differ only by --model.
func suggestName(cmd string, args []string, i int) string {
	for j, a := range args {
		if a == "--model" && j+1 < len(args) {
			m := args[j+1]
			if _, after, ok := strings.Cut(m, "/"); ok {
				m = after
			}
			return m
		}
	}
	if i == 0 {
		return cmd
	}
	return fmt.Sprintf("%s-%d", cmd, i+1)
}
