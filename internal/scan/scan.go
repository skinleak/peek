// Package scan discovers sockets that are listening for connections and the
// processes that own them. The platform-specific work lives behind Scanner so
// callers never need to know which OS they run on.
package scan

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// ErrUnsupported is returned by scanners on platforms that are not implemented yet.
var ErrUnsupported = errors.New("listing listeners is not supported on this platform yet")

// Listener is a single listening socket and, when it can be determined, the
// process that owns it. A socket shared by several processes (for example a
// pre-forking server) yields one Listener per process.
type Listener struct {
	Port        uint16     `json:"port"`
	Protocol    string     `json:"protocol"` // "tcp" or "tcp6"
	Address     netip.Addr `json:"address"`
	PID         int        `json:"pid,omitzero"` // 0 when the owner could not be determined
	ProcessName string     `json:"process,omitzero"`
	Command     []string   `json:"command,omitzero"` // the process's arguments, starting with argv[0]
	Cwd         string     `json:"cwd,omitzero"`
	StartTime   time.Time  `json:"start_time,omitzero"`
	User        string     `json:"user,omitzero"`
	Connections int        `json:"connections,omitzero"`  // established connections to this port that peek can see
	Container   string     `json:"container,omitzero"`    // Docker container publishing this port
	ContainerID string     `json:"container_id,omitzero"` // its full ID
}

// Exposed reports whether the socket accepts connections from other hosts,
// i.e. it is not bound to a loopback address.
func (l Listener) Exposed() bool {
	return !l.Address.IsLoopback()
}

// Scanner lists the sockets currently listening on this machine.
type Scanner interface {
	Scan() ([]Listener, error)
}

// PortRange is an inclusive range of ports. A single port has Lo == Hi.
type PortRange struct {
	Lo, Hi uint16
}

// Contains reports whether port lies within r.
func (r PortRange) Contains(port uint16) bool {
	return port >= r.Lo && port <= r.Hi
}

func (r PortRange) String() string {
	if r.Lo == r.Hi {
		return strconv.Itoa(int(r.Lo))
	}
	return fmt.Sprintf("%d-%d", r.Lo, r.Hi)
}

// ParsePortRange parses "3000" or "3000-3999".
func ParsePortRange(s string) (PortRange, error) {
	loStr, hiStr, isRange := strings.Cut(s, "-")
	lo, err := parsePort(loStr)
	if err != nil {
		return PortRange{}, err
	}
	if !isRange {
		return PortRange{Lo: lo, Hi: lo}, nil
	}
	hi, err := parsePort(hiStr)
	if err != nil {
		return PortRange{}, err
	}
	if lo > hi {
		return PortRange{}, fmt.Errorf("invalid port range %q: start is greater than end", s)
	}
	return PortRange{Lo: lo, Hi: hi}, nil
}

func parsePort(s string) (uint16, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q: not a number", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("invalid port %d: must be between 1 and 65535", n)
	}
	return uint16(n), nil
}

// Filter returns the listeners whose port falls in any of the given ranges.
// With no ranges, all listeners are returned.
func Filter(ls []Listener, ranges []PortRange) []Listener {
	if len(ranges) == 0 {
		return ls
	}
	var out []Listener
	for _, l := range ls {
		for _, r := range ranges {
			if r.Contains(l.Port) {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// DescribePorts renders ranges for messages: "port 3000", "ports 3000-3999".
func DescribePorts(ranges []PortRange) string {
	parts := make([]string, len(ranges))
	for i, r := range ranges {
		parts[i] = r.String()
	}
	if len(ranges) == 1 && ranges[0].Lo == ranges[0].Hi {
		return "port " + parts[0]
	}
	return "ports " + strings.Join(parts, ", ")
}
