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
  pb run -brief <file>    spec, race, gate, select, report
  pb bench                run the arms and print the comparison
  pb doctor [--live]      check the configured agents work
                          --live actually runs each one (costs a little)

flags:
  -c <path>   config file (default .pb.toml)
`

func main() {
	cfgPath := flag.String("c", config.FileName, "config file")
	briefPath := flag.String("brief", "", "path to a brief file")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	if err := dispatch(flag.Arg(0), flag.Args()[1:], *cfgPath, *briefPath); err != nil {
		fmt.Fprintln(os.Stderr, "pb:", err)
		os.Exit(1)
	}
}

func dispatch(cmd string, args []string, cfgPath, briefPath string) error {
	switch cmd {
	case "init":
		return cmdInit(cfgPath)
	case "run":
		return cmdRun(cfgPath, briefPath)
	case "bench":
		return cmdBench(cfgPath)
	case "doctor":
		live := len(args) > 0 && args[0] == "--live"
		return cmdDoctor(cfgPath, live)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func cmdBench(cfgPath string) error {
	return fmt.Errorf("bench: not implemented")
}
