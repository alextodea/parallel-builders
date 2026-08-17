package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/alextodea/parallel-builders/internal/config"
	"github.com/alextodea/parallel-builders/internal/setup"
)

// errWrongRepo is returned when the user says the detected repository is not
// the one they meant. The config lives beside the code it configures, so
// letting them type a different owner/name here would only produce a file that
// disagrees with the directory it sits in.
var errWrongRepo = fmt.Errorf("run `pb init` from inside the repository you want to build the feature in")

// cmdInit walks the user through creating a .pb.toml.
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

	// ---- 1. repository -------------------------------------------------
	p.Say("\n─── repository ───────────────────────────────────────────")
	p.Say("")

	remote := setup.GitRemote(wd)
	if remote == "" {
		p.Say("  No GitHub remote found in this directory.")
		p.Say("")
		p.Say("  If this is not where you want the feature built, quit and")
		p.Say("  run `pb init` from inside that repository instead.")
		p.Say("")
		cfg.Repo = p.AskRepo("owner/name", "")
	} else {
		p.Say("  Branches and commits will be created here:")
		p.Say("")
		p.Say("      %s", remote)
		p.Say("")
		p.Say("  If that is not the right repository, answer n — then run")
		p.Say("  `pb init` again from inside the one you want.")
		p.Say("")
		if !p.Confirm("Correct?") {
			return errWrongRepo
		}
		cfg.Repo = remote
	}

	gate, tests, lang := config.DetectGate(wd)
	if lang != "" {
		p.Say("\n  Detected a %s project — gate commands are pre-filled below.", lang)
	}

	installed := setup.Installed()

	// ---- 2. architect --------------------------------------------------
	p.Say("\n─── architect  ·  writes the tests ───────────────────────")
	p.Say("")
	p.Say("  Reads your feature description and writes the test suite that")
	p.Say("  defines \"done\". It runs once per feature, and everything")
	p.Say("  afterwards is graded against what it writes.")
	p.Say("")
	p.Say("  Use your best model here. This is the one call worth paying for —")
	p.Say("  if these tests are wrong, everything downstream is wrong too.")
	p.Say("")

	if len(installed) == 0 {
		p.Say("  No known agent CLIs found on PATH. Type a command anyway.")
		p.Say("")
	}
	archCmd := p.AskChoice("agent", installed, first(installed, "claude"))
	p.Say("")
	p.Say("  Model and reasoning flags are passed straight to that agent —")
	p.Say("  pb does not know which models you have access to, so use the")
	p.Say("  same flags you would type yourself.")
	p.Say("")
	p.Say("      e.g.  --model claude-fable-5 --reasoning high")
	p.Say("")
	cfg.Architect = config.Agent{
		Cmd:     archCmd,
		Args:    splitArgs(p.Ask("flags (blank for the agent's default)", "")),
		Timeout: "10m",
	}

	// ---- 3. builders ---------------------------------------------------
	p.Say("\n─── builders  ·  make the tests pass ─────────────────────")
	p.Say("")
	p.Say("  Cheaper models, run at the same time. Each one gets the")
	p.Say("  architect's tests and its own copy of the repository, and")
	p.Say("  writes an implementation. They cannot edit the tests.")
	p.Say("")
	p.Say("  Whichever implementation passes wins. The rest are thrown away.")
	p.Say("")
	p.Say("  Two different models beats two runs of the same one — you want")
	p.Say("  them to make different mistakes, not the same mistake twice.")
	p.Say("")

	n := p.AskInt("how many", 2, 1, 10)
	if n > 5 {
		p.Say("")
		p.Say("  Note: past about 5 the success rate stops climbing but you")
		p.Say("  still pay for every discarded attempt. `pb bench` can show")
		p.Say("  you where the returns actually stop.")
	}

	for i := range n {
		p.Say("")
		p.Say("  builder %d of %d", i+1, n)
		cmd := p.AskChoice("agent", installed, first(installed, "opencode"))
		args := splitArgs(p.Ask("flags", ""))
		name := p.Ask("short name (shown in results)", suggestName(cmd, args, i))
		cfg.Builders = append(cfg.Builders, config.Agent{
			Name: name, Cmd: cmd, Args: args, Timeout: "10m",
		})
	}

	// ---- 4. gate -------------------------------------------------------
	p.Say("\n─── gate  ·  decides who passed ──────────────────────────")
	p.Say("")
	p.Say("  Ordinary commands, run in order, stopping at the first failure.")
	p.Say("  No model is involved — this is what makes the result trustworthy.")
	p.Say("")
	cfg.Gate = config.Gate{
		Build: p.Ask("build", gate.Build),
		Lint:  p.Ask("lint", gate.Lint),
		Test:  p.Ask("test", gate.Test),
	}

	p.Say("")
	p.Say("  Which files are the tests? Builders are blocked from editing")
	p.Say("  anything matching these.")
	p.Say("")
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

	// ---- summary -------------------------------------------------------
	p.Say("\n─── wrote %s ────────────────────────────────────────", path)
	p.Say("")
	p.Say("  repository   %s", cfg.Repo)
	p.Say("  architect    %s", describe(cfg.Architect))
	for i, b := range cfg.Builders {
		label := "builders"
		if i > 0 {
			label = ""
		}
		p.Say("  %-12s %-14s %s", label, b.Label(), describe(b))
	}
	p.Say("")
	p.Say("  Next:  pb run \"describe the feature you want\"")
	p.Say("")
	return nil
}

func describe(a config.Agent) string {
	return strings.TrimSpace(a.Cmd + " " + strings.Join(a.Args, " "))
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
		if (a == "--model" || a == "-m") && j+1 < len(args) {
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
