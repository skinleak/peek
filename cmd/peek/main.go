// Command peek shows which processes are listening on which ports and lets
// you free them up.
package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"peek/internal/kill"
	"peek/internal/scan"
	"peek/internal/ui"
)

const (
	exitOK       = 0
	exitNotFound = 1 // nothing listening, or a runtime failure
	exitUsage    = 2
)

// killWait is how long to wait for a signalled process to exit.
const killWait = 3 * time.Second

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cfg, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "peek: %v\nRun 'peek --help' for usage.\n", err)
		return exitUsage
	}
	if cfg.help {
		fmt.Print(usage)
		return exitOK
	}

	all, err := scan.New().Scan()
	if err != nil {
		fmt.Fprintf(os.Stderr, "peek: %v\n", err)
		return exitNotFound
	}
	matched := scan.Filter(all, cfg.ranges)

	if cfg.kill {
		return runKill(cfg, matched)
	}
	return runList(cfg, matched)
}

func runList(cfg config, ls []scan.Listener) int {
	code := exitOK
	if len(ls) == 0 && len(cfg.ranges) > 0 {
		code = exitNotFound
	}

	if cfg.json {
		if err := ui.JSON(os.Stdout, ls); err != nil {
			fmt.Fprintf(os.Stderr, "peek: writing JSON: %v\n", err)
			return exitNotFound
		}
		return code
	}

	if len(ls) == 0 {
		fmt.Println(nothingListening(cfg.ranges))
		return code
	}
	if err := ui.Table(os.Stdout, ls, ui.DefaultOptions(os.Stdout)); err != nil {
		fmt.Fprintf(os.Stderr, "peek: %v\n", err)
		return exitNotFound
	}
	if hasHiddenOwners(ls) && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "\nSome processes belong to other users and are hidden. Run with sudo to see them.")
	}
	return code
}

func runKill(cfg config, ls []scan.Listener) int {
	if len(ls) == 0 {
		fmt.Println(nothingListening(cfg.ranges))
		return exitNotFound
	}

	targets, hidden := kill.Targets(ls)
	for _, l := range hidden {
		fmt.Fprintf(os.Stderr, "peek: can't see which process owns port %d (user %s); try again with sudo\n", l.Port, l.User)
	}
	if len(targets) == 0 {
		return exitNotFound
	}

	err := kill.Run(targets, kill.Options{
		Force: cfg.force,
		Yes:   cfg.yes,
		Wait:  killWait,
		In:    os.Stdin,
		Out:   os.Stdout,
	})
	switch {
	case errors.Is(err, kill.ErrAborted):
		fmt.Println("Aborted.")
		return exitNotFound
	case err != nil:
		fmt.Fprintf(os.Stderr, "peek: %v\n", err)
		return exitNotFound
	}
	return exitOK
}

func nothingListening(ranges []scan.PortRange) string {
	if len(ranges) == 0 {
		return "Nothing is listening."
	}
	return "Nothing is listening on " + describePorts(ranges)
}

func hasHiddenOwners(ls []scan.Listener) bool {
	for _, l := range ls {
		if l.PID == 0 {
			return true
		}
	}
	return false
}
