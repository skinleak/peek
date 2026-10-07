package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aaron03EM/peek/internal/scan"
)

const usage = `peek - see what's listening on your ports, and free them up

Usage:
  peek [flags]                    list everything listening
  peek <port|range>... [flags]    show listeners on ports, e.g. 3000 or 3000-3999
  peek kill <port|range>...       terminate the processes on those ports

Flags:
  --json        machine-readable output
  -y, --yes     kill: don't ask for confirmation
  -f, --force   kill: send SIGKILL instead of SIGTERM
  -h, --help    show this help

Exit codes: 0 success, 1 nothing listening or operation failed, 2 usage error.
`

// config is the parsed command line.
type config struct {
	kill   bool
	ranges []scan.PortRange
	json   bool
	watch  bool
	force  bool
	yes    bool
	help   bool
}

// parseArgs parses the command line (without the program name). Flags may
// appear before or after positional arguments; "--" ends flag parsing.
func parseArgs(args []string) (config, error) {
	var cfg config
	var positional []string
	for i, arg := range args {
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		if err := cfg.setFlag(arg); err != nil {
			return config{}, err
		}
	}
	if cfg.help {
		return cfg, nil
	}

	if len(positional) > 0 && positional[0] == "kill" {
		cfg.kill = true
		positional = positional[1:]
		if len(positional) == 0 {
			return config{}, errors.New("kill needs a port, e.g. 'peek kill 3000'")
		}
	}
	for _, p := range positional {
		r, err := scan.ParsePortRange(p)
		if err != nil {
			return config{}, err
		}
		cfg.ranges = append(cfg.ranges, r)
	}
	return cfg, cfg.validate()
}

func (c *config) setFlag(arg string) error {
	switch arg {
	case "--json":
		c.json = true
	case "-w", "--watch":
		c.watch = true
	case "-f", "--force":
		c.force = true
	case "-y", "--yes":
		c.yes = true
	case "-h", "--help":
		c.help = true
	default:
		return fmt.Errorf("unknown flag %q", arg)
	}
	return nil
}

func (c config) validate() error {
	switch {
	case c.watch:
		return errors.New("--watch is not available yet")
	case c.kill && c.json:
		return errors.New("--json cannot be used with kill")
	case !c.kill && c.force:
		return errors.New("--force only applies to 'peek kill'")
	case !c.kill && c.yes:
		return errors.New("--yes only applies to 'peek kill'")
	}
	return nil
}

// describePorts renders ranges for messages: "port 3000", "ports 3000-3999".
func describePorts(ranges []scan.PortRange) string {
	parts := make([]string, len(ranges))
	for i, r := range ranges {
		parts[i] = r.String()
	}
	if len(ranges) == 1 && ranges[0].Lo == ranges[0].Hi {
		return "port " + parts[0]
	}
	return "ports " + strings.Join(parts, ", ")
}
