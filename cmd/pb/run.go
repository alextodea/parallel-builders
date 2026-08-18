package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alextodea/parallel-builders/internal/agent"
	"github.com/alextodea/parallel-builders/internal/brief"
	"github.com/alextodea/parallel-builders/internal/config"
	"github.com/alextodea/parallel-builders/internal/pool"
	"github.com/alextodea/parallel-builders/internal/run"
)

// cmdRun executes one feature end to end.
//
// Until order 6 lands there is no intake, so a brief has to be supplied. That
// is also the non-interactive path bench will use, so building it first costs
// nothing and settles the interface early.
func cmdRun(cfgPath, briefPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	if briefPath == "" {
		return fmt.Errorf("no brief supplied — pass -brief <file>\n" +
			"       (interactive intake arrives in a later release)")
	}

	b, err := brief.Load(briefPath)
	if err != nil {
		return err
	}

	repo, err := repoRoot()
	if err != nil {
		return err
	}
	if err := b.CheckExamples(repo); err != nil {
		return err
	}

	// Ctrl-C has to release worktrees and kill agent process groups. A leaked
	// lock would block every future run on this repository.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr, "\n  interrupted — releasing worktrees")
		cancel()
	}()

	p, err := pool.Open(config.ExpandHome(cfg.Pool.Root), repo, len(cfg.Builders))
	if err != nil {
		return err
	}
	defer p.ReleaseAll()

	timeout := cfg.Architect.Duration()
	builders := make([]agent.Runner, 0, len(cfg.Builders))
	for _, bc := range cfg.Builders {
		builders = append(builders, agent.Exec{
			Cmd: bc.Cmd, Args: bc.Args, Timeout: bc.Duration(),
		})
	}

	start := time.Now()
	out, err := run.Execute(ctx, run.Options{
		Repo:   repo,
		Config: cfg,
		Brief:  b,
		Architect: agent.Exec{
			Cmd: cfg.Architect.Cmd, Args: cfg.Architect.Args, Timeout: timeout,
		},
		Builders: builders,
		Pool:     p,
		Out:      os.Stdout,
	})
	if err != nil {
		return err
	}

	report(out, time.Since(start))
	if out.Winner == nil {
		return fmt.Errorf("no candidate passed after %d round(s)", out.Rounds)
	}
	return nil
}

func report(out run.Outcome, wall time.Duration) {
	fmt.Println()
	fmt.Println("  gate")
	for _, c := range out.Candidates {
		status := "PASS"
		detail := ""
		if !c.Passed() {
			status = "FAIL"
			switch {
			case c.AgentErr != nil:
				detail = "  agent error: " + firstLine(c.AgentErr.Error())
			case c.NoDiff:
				detail = "  produced no changes"
			case len(c.TouchedFrozen) > 0:
				detail = "  edited a frozen test file — disqualified"
			case c.Tests.Crashed:
				detail = "  " + c.Tests.Note
			case len(c.Tests.Failed) > 0:
				detail = "  " + strings.Join(c.Tests.Failed, ", ")
			default:
				detail = "  failed at " + c.Gate.FailedAt
			}
		}
		fmt.Printf("    %-14s %-4s  %d/%d tests%s\n",
			c.Builder, status, c.Tests.Passed, c.Tests.Total, detail)
	}

	fmt.Println()
	if out.Winner != nil {
		fmt.Printf("  winner   %s — %s\n", out.Winner.Builder, out.Reason)
		fmt.Printf("           %d files, %d lines\n", out.Winner.FilesTouched, out.Winner.DiffLines)
		fmt.Printf("           worktree %s\n", out.Winner.Worktree)
	} else {
		fmt.Printf("  no winner after %d round(s) — %s\n", out.Rounds, out.Route)
	}
	fmt.Printf("\n  %s wall · %d round(s)\n", wall.Round(time.Millisecond), out.Rounds)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}
