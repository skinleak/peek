// Command peek shows which processes are listening on which ports and lets
// you free them up.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/skinleak/peek/internal/docker"
	"github.com/skinleak/peek/internal/kill"
	"github.com/skinleak/peek/internal/scan"
	"github.com/skinleak/peek/internal/tui"
	"github.com/skinleak/peek/internal/ui"
)

const (
	exitOK       = 0
	exitNotFound = 1 // nothing listening, or a runtime failure
	exitUsage    = 2
)

const (
	// killWait is how long to wait for a signalled process to exit.
	killWait = 3 * time.Second
	// containerStopTimeout covers Docker's default 10s stop grace period.
	containerStopTimeout = 30 * time.Second
	// refreshInterval is how often the interactive view rescans.
	refreshInterval = time.Second
)

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
	if cfg.version {
		fmt.Println("peek", versionString())
		return exitOK
	}

	if cfg.wait {
		return runWait(cfg)
	}
	if cfg.interactive {
		return runInteractive(cfg)
	}
	interactive := isTerminal(os.Stdin) && isTerminal(os.Stdout)
	if cfg.kill && len(cfg.ranges) == 0 && !interactive {
		fmt.Fprintln(os.Stderr, "peek: kill needs a port, e.g. 'peek kill 3000'\nRun 'peek --help' for usage.")
		return exitUsage
	}

	matched, dockerClient, err := listen(cfg.ranges)
	if err != nil {
		fmt.Fprintf(os.Stderr, "peek: %v\n", err)
		return exitNotFound
	}

	if cfg.kill {
		return runKill(cfg, matched, dockerClient, interactive)
	}
	return runList(cfg, matched)
}

// listen scans for listeners on the given ports and names the Docker
// containers among them. The client is nil if Docker isn't needed or reachable.
func listen(ranges []scan.PortRange) ([]scan.Listener, *docker.Client, error) {
	all, err := scan.New().Scan()
	if err != nil {
		return nil, nil, err
	}
	matched := scan.Filter(all, ranges)
	return matched, docker.Enrich(matched), nil
}

func runInteractive(cfg config) int {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		fmt.Fprintln(os.Stderr, "peek: --interactive needs a terminal")
		return exitUsage
	}
	home, _ := os.UserHomeDir()
	err := tui.Run(tui.Config{
		Ranges:   cfg.ranges,
		Interval: refreshInterval,
		Home:     home,
		Root:     os.Geteuid() == 0,
		Scan: func() ([]scan.Listener, error) {
			ls, _, err := listen(cfg.ranges)
			return ls, err
		},
		Stop: func(t kill.Target, force bool) error {
			opts := killOptions(force, docker.Connect())
			opts.Yes, opts.Out = true, io.Discard
			opts.ForceHint = "press X to send SIGKILL"
			return kill.Run([]kill.Target{t}, opts)
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "peek: %v\n", err)
		return exitNotFound
	}
	return exitOK
}

func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
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

// runKill stops the owners of ls. In a terminal it lets the user pick when
// no ports were given, or when several processes match and --yes wasn't.
func runKill(cfg config, ls []scan.Listener, dc *docker.Client, interactive bool) int {
	if len(ls) == 0 {
		fmt.Println(nothingListening(cfg.ranges))
		return exitNotFound
	}

	targets, hidden := kill.Targets(ls)
	pick := interactive && len(targets) > 0 && (len(cfg.ranges) == 0 || (len(targets) > 1 && !cfg.yes))
	if !pick {
		for _, l := range hidden {
			fmt.Fprintf(os.Stderr, "peek: can't see which process owns port %d (user %s); try again with sudo\n", l.Port, l.User)
		}
	}
	if len(targets) == 0 {
		return exitNotFound
	}

	opts := killOptions(cfg.force, dc)
	opts.Yes = cfg.yes
	if pick {
		home, _ := os.UserHomeDir()
		chosen, err := tui.Pick(ls, tui.PickOptions{Force: cfg.force, Home: home, Hidden: distinctPorts(hidden)})
		switch {
		case errors.Is(err, tui.ErrCancelled):
			fmt.Println("Aborted.")
			return exitNotFound
		case err != nil:
			fmt.Fprintf(os.Stderr, "peek: %v\n", err)
			return exitNotFound
		}
		targets, opts.Yes = chosen, true // choosing them was the confirmation
	}
	err := kill.Run(targets, opts)
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

// killOptions returns options for kill.Run that prompt on the terminal. dc
// stops containers; without it, stopping one reports that Docker is unreachable.
func killOptions(force bool, dc *docker.Client) kill.Options {
	opts := kill.Options{
		Force: force,
		Wait:  killWait,
		In:    os.Stdin,
		Out:   os.Stdout,
	}
	if dc != nil {
		opts.StopContainer = func(id string, force bool) error {
			ctx, cancel := context.WithTimeout(context.Background(), containerStopTimeout)
			defer cancel()
			return dc.Stop(ctx, id, force)
		}
	}
	return opts
}

// version is set at release build time with -ldflags "-X main.version=...".
var version string

// versionString reports the release version, or the module version when
// installed with `go install ...@vX.Y.Z`.
func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}

func nothingListening(ranges []scan.PortRange) string {
	if len(ranges) == 0 {
		return "Nothing is listening."
	}
	return "Nothing is listening on " + scan.DescribePorts(ranges)
}

// distinctPorts counts the different ports in ls; a port can have one
// listener per address.
func distinctPorts(ls []scan.Listener) int {
	seen := make(map[uint16]bool)
	for _, l := range ls {
		seen[l.Port] = true
	}
	return len(seen)
}

func hasHiddenOwners(ls []scan.Listener) bool {
	for _, l := range ls {
		if l.PID == 0 && l.Container == "" {
			return true
		}
	}
	return false
}
