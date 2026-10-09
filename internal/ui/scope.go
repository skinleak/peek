package ui

import (
	"strings"

	"github.com/skinleak/peek/internal/scan"
)

// DescribeDirs renders project directories for messages, with home as "~".
func DescribeDirs(dirs []string, home string) string {
	parts := make([]string, len(dirs))
	for i, d := range dirs {
		parts[i] = Tildify(d, home)
	}
	return strings.Join(parts, ", ")
}

// NothingListening says that nothing matched the given ports and directories,
// such as "Nothing is listening on port 3000 in ~/code/webapp".
func NothingListening(ranges []scan.PortRange, dirs []string, home string) string {
	msg := "Nothing is listening"
	if len(ranges) > 0 {
		msg += " on " + scan.DescribePorts(ranges)
	}
	if len(dirs) > 0 {
		msg += " in " + DescribeDirs(dirs, home)
	}
	return msg
}

// ProcessLabel names what holds a listener the way the table does: "node
// (vite)", "webapp-db (docker)", or "-" when the owner is hidden.
func ProcessLabel(l scan.Listener) string {
	switch {
	case l.Container != "":
		return l.Container + " (docker)"
	case l.PID == 0:
		return unknown
	}
	if target := Describe(l.ProcessName, l.Command); target != "" {
		return l.ProcessName + " (" + target + ")"
	}
	return l.ProcessName
}
