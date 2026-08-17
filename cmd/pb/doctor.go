package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/alextodea/parallel-builders/internal/config"
	"github.com/alextodea/parallel-builders/internal/setup"
)

// cmdDoctor checks that what the config asks for actually exists.
func cmdDoctor(cfgPath string, live bool) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	ok := checkAgents(os.Stdout, cfg, live)
	if !ok {
		return fmt.Errorf("some agents are not usable — see above")
	}
	return nil
}

// checkAgents prints a line per agent. When live is false it only confirms the
// binary resolves, which proves nothing about the model flags.
func checkAgents(w io.Writer, cfg config.Config, live bool) bool {
	agents := append([]config.Agent{cfg.Architect}, cfg.Builders...)
	roles := make([]string, len(agents))
	roles[0] = "architect"
	for i := 1; i < len(roles); i++ {
		roles[i] = "builder"
	}

	allOK := true
	for i, a := range agents {
		var p setup.Probe
		if live {
			p = setup.CheckLive(a.Label(), a.Cmd, a.Args, 90*time.Second)
		} else {
			p = setup.CheckPath(a.Label(), a.Cmd, a.Args)
		}

		mark := "✓"
		if !p.OK() {
			mark, allOK = "✗", false
		}
		fmt.Fprintf(w, "  %s %-10s %-16s %s\n", mark, roles[i], a.Label(), p.Status())
		if p.Err != nil && p.Output != "" {
			fmt.Fprintf(w, "      %s\n", p.Output)
		}
	}

	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(w, "  ✗ git        not on PATH")
		allOK = false
	}
	return allOK
}
