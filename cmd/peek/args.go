package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/skinleak/peek/internal/scan"
)

const usage = `peek - see what's listening on your ports, and free them up

Usage:
  peek [flags]                    list everything listening
  peek <port|range>... [flags]    show listeners on ports, e.g. 3000 or 3000-3999
  peek kill [<port|range>...]     terminate the processes on those ports; in a
                                  terminal, pick from a list if there are several
  peek -i [<port|range>...]       live view to browse ports and stop processes

Flags:
  -i, --interactive  live view: select a row and press x to stop it
  --json             machine-readable output
  -y, --yes          kill: don't ask for confirmation
  -f, --force        kill: send SIGKILL instead of SIGTERM
  -h, --help         show this help
  --version          print the version

Exit codes: 0 success, 1 nothing listening or operation failed, 2 usage error.
`

// config is the parsed command line.
type config struct {
	kill        bool
	ranges      []scan.PortRange
	json        bool
	interactive bool
	force       bool
	yes         bool
	help        bool
	version     bool
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
	if cfg.help || cfg.version {
		return cfg, nil
	}

	if len(positional) > 0 && positional[0] == "kill" {
		cfg.kill = true
		positional = positional[1:] // without ports, run decides: picker or error
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
	case "-i", "--interactive":
		c.interactive = true
	case "-f", "--force":
		c.force = true
	case "-y", "--yes":
		c.yes = true
	case "-h", "--help":
		c.help = true
	case "--version":
		c.version = true
	default:
		return fmt.Errorf("unknown flag %q", arg)
	}
	return nil
}

func (c config) validate() error {
	switch {
	case c.interactive && c.kill:
		return errors.New("--interactive cannot be used with kill; press x in the interactive view instead")
	case c.interactive && c.json:
		return errors.New("--interactive cannot be used with --json")
	case c.kill && c.json:
		return errors.New("--json cannot be used with kill")
	case !c.kill && c.force:
		return errors.New("--force only applies to 'peek kill'")
	case !c.kill && c.yes:
		return errors.New("--yes only applies to 'peek kill'")
	}
	return nil
}
