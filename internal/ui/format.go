package ui

import (
	"fmt"
	"strings"
	"time"
)

// Tildify replaces a leading home directory in path with "~".
func Tildify(path, home string) string {
	if home == "" || home == "/" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return path
}

// FormatUptime renders a duration compactly with at most two units:
// "42s", "7m", "3h12m", "5d4h".
func FormatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return twoUnits(int(d.Hours()), "h", int(d.Minutes())%60, "m")
	default:
		days := int(d.Hours()) / 24
		return twoUnits(days, "d", int(d.Hours())%24, "h")
	}
}

func twoUnits(major int, majorUnit string, minor int, minorUnit string) string {
	if minor == 0 {
		return fmt.Sprintf("%d%s", major, majorUnit)
	}
	return fmt.Sprintf("%d%s%d%s", major, majorUnit, minor, minorUnit)
}

// truncateLeft shortens s to at most width runes, keeping the end, which is
// the most specific part of a path.
func truncateLeft(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return "…" + string(r[len(r)-width+1:])
}

// truncateRight shortens s to at most width runes, keeping the start.
func truncateRight(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}
