// Command pb races cheap coding agents against a frozen test suite.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/alextodea/parallel-builders/internal/config"
)

const usage = `pb — race cheap agents against a frozen test suite

usage:
  pb init                 write a .pb.toml with sensible defaults
  pb run <task>           spec, race, gate, select, report
  pb bench                run the arms and print the comparison
  pb doctor               check agents and toolchain are present

flags:
  -c <path>   config file (default .pb.toml)
`

func main() {
	cfgPath := flag.String("c", config.FileName, "config file")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	if err := dispatch(flag.Arg(0), flag.Args()[1:], *cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "pb:", err)
		os.Exit(1)
	}
}

func dispatch(cmd string, args []string, cfgPath string) error {
	switch cmd {
	case "init":
		return cmdInit(cfgPath)
	case "run":
		if len(args) == 0 {
			return fmt.Errorf("run needs a task description")
		}
		return cmdRun(cfgPath, args[0])
	case "bench":
		return cmdBench(cfgPath)
	case "doctor":
		return cmdDoctor(cfgPath)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func cmdRun(cfgPath, task string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// The five phases. Each is a package that can be tested on its own.
	//
	// TODO 1  spec     architect writes tests; reject any that pass at base
	//                  HEAD, because a test that passes before the feature
	//                  exists is proving nothing
	// TODO 2  race     claim a worktree per builder, run them concurrently
	// TODO 3  gate     run the configured commands in each worktree
	// TODO 4  select   disqualify, then walk the tie-break ladder
	// TODO 5  report   append the run record, print the summary
	_ = cfg
	return fmt.Errorf("run: not implemented — task was %q", task)
}

func cmdBench(cfgPath string) error {
	return fmt.Errorf("bench: not implemented")
}

func cmdDoctor(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	fmt.Println("repo:      ", cfg.Repo)
	fmt.Println("architect: ", cfg.Architect.Label())
	for _, b := range cfg.Builders {
		fmt.Println("builder:   ", b.Label())
	}
	// TODO: check each agent resolves on PATH, that git works, and that the
	// gate commands run green in a clean worktree before anything else.
	return nil
}
