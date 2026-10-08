package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/skinleak/peek/internal/scan"
)

const usage = `peek - see what's listening on your ports, and free them up

Usage:
  peek [flags]                    list everything listening
  peek <port|range>... [flags]    show listeners on ports, e.g. 3000 or 3000-3999
  peek kill <port|range>...       terminate the processes on those ports
  peek -i [<port|range>...]       live view to browse ports and stop processes
  peek wait <port|range>...       wait until something listens on each, or with
                                  --free until nothing does

Flags:
  -i, --interactive  live view: select a row and press x to stop it
  --json             machine-readable output
  --free             wait: wait until the ports are free instead
  -t, --timeout D    wait: give up after D, e.g. 30s or 2m (default: never)
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

	wait    bool          // peek wait
	free    bool          // wait until the ports are free
	timeout time.Duration // how long to wait; 0 means forever
}

// parseArgs parses the command line (without the program name). Flags may
// appear before or after positional arguments; "--" ends flag parsing.
func parseArgs(args []string) (config, error) {
	var cfg config
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		if name, value, ok := valueFlag(args, &i); ok {
			if err := cfg.setValue(name, value); err != nil {
				return config{}, err
			}
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
		positional = positional[1:]
		if len(positional) == 0 {
			return config{}, errors.New("kill needs a port, e.g. 'peek kill 3000'")
		}
	}
	if len(positional) > 0 && positional[0] == "wait" {
		cfg.wait = true
		positional = positional[1:]
		if len(positional) == 0 {
			return config{}, errors.New("wait needs a port, e.g. 'peek wait 5432'")
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

// valueFlags are the flags that take a value, as "--timeout 30s" or
// "--timeout=30s".
var valueFlags = map[string]string{"-t": "--timeout", "--timeout": "--timeout"}

// valueFlag reports whether args[*i] is a flag that takes a value and
// returns its canonical name and value, advancing *i past a separate value.
// A missing value is returned as "" for setValue to reject.
func valueFlag(args []string, i *int) (name, value string, ok bool) {
	arg, value, inline := strings.Cut(args[*i], "=")
	name, ok = valueFlags[arg]
	if !ok || inline {
		return name, value, ok
	}
	if *i+1 < len(args) {
		*i++
		value = args[*i]
	}
	return name, value, true
}

func (c *config) setValue(name, value string) error {
	switch name {
	case "--timeout":
		d, err := parseTimeout(value)
		if err != nil {
			return err
		}
		c.timeout = d
	}
	return nil
}

// parseTimeout accepts a Go duration like "30s" or "2m", or a plain number
// of seconds.
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("--timeout needs a duration, e.g. --timeout 30s")
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		secs, numErr := strconv.ParseFloat(s, 64)
		if numErr != nil {
			return 0, fmt.Errorf("invalid timeout %q: use a duration like 30s or 2m", s)
		}
		d = time.Duration(secs * float64(time.Second))
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid timeout %q: must be more than zero", s)
	}
	return d, nil
}

func (c *config) setFlag(arg string) error {
	switch arg {
	case "--json":
		c.json = true
	case "-i", "--interactive":
		c.interactive = true
	case "--free":
		c.free = true
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
	case c.wait && (c.interactive || c.json || c.force || c.yes):
		return errors.New("wait can't be combined with --interactive, --json, --force or --yes")
	case !c.wait && c.free:
		return errors.New("--free only applies to 'peek wait'")
	case !c.wait && c.timeout > 0:
		return errors.New("--timeout only applies to 'peek wait'")
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
