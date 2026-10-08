package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/peek/internal/ui"
)

const (
	maxPanelWidth  = 100
	labelWidth     = 10
	maxCommandRows = 4 // rows for the command line before it's cut
)

// detailPanel renders everything peek knows about a row in a rounded box,
// at most width columns wide.
func (m *model) detailPanel(e entry, width int) []string {
	l := e.row[0]
	width = min(width, maxPanelWidth)
	inner := width - 4 // border and padding
	valueWidth := max(10, inner-labelWidth)

	var lines []string
	field := func(label string, values ...string) {
		for i, v := range values {
			if i == 0 {
				lines = append(lines, m.st.dim.Render(fmt.Sprintf("%-*s", labelWidth, label))+v)
			} else {
				lines = append(lines, strings.Repeat(" ", labelWidth)+v)
			}
		}
	}

	if e.gone {
		lines = append(lines, m.st.err.Render("This port just closed."), "")
	}
	field("port", m.portLine(e.row))
	if l.ContainerID != "" {
		field("container", m.st.text.Render(l.Container)+" "+m.st.dim.Render(shortID(l.ContainerID)))
	}
	if len(l.Command) > 0 {
		field("command", m.texts(wrap(strings.Join(l.Command, " "), valueWidth, maxCommandRows))...)
	}
	if l.Cwd != "" {
		field("cwd", m.texts(wrap(ui.Tildify(l.Cwd, m.cfg.Home), valueWidth, 2))...)
	}
	if !l.StartTime.IsZero() {
		now := m.now()
		field("started", m.st.text.Render(formatStart(l.StartTime, now))+m.st.dim.Render(" · "+ui.FormatUptime(now.Sub(l.StartTime))+" ago"))
	}
	if l.User != "" {
		field("user", m.st.text.Render(l.User))
	}
	field("clients", m.clientsLine(l.Connections))
	if l.PID == 0 && l.ContainerID == "" {
		hint := "This process belongs to another user. Run peek with sudo to see it."
		if m.cfg.Root {
			// Root can still be denied, e.g. in a container without CAP_SYS_PTRACE.
			hint = "peek isn't allowed to see which process owns this port."
		}
		lines = append(lines, "", m.st.dim.Render(hint))
	}

	return box(m.st, m.panelTitle(e), lines, width)
}

func (m *model) panelTitle(e entry) string {
	l := e.row[0]
	switch {
	case l.ContainerID != "":
		return m.st.title.Render(l.Container) + m.st.dim.Render(" · docker container")
	case l.PID == 0:
		return m.st.title.Render("port " + strconv.Itoa(int(l.Port)))
	}
	return m.st.title.Render(l.ProcessName) + m.st.dim.Render(" · PID "+strconv.Itoa(l.PID))
}

// portLine lists the port and its bind addresses, colored by exposure.
func (m *model) portLine(r ui.Row) string {
	var addrs []string
	exposed := false
	for _, l := range r {
		style := m.st.ok
		if l.Exposed() {
			style, exposed = m.st.warn, true
		}
		addrs = append(addrs, style.Render(l.Address.String()))
	}
	note := "local only"
	if exposed {
		note = "exposed to the network"
	}
	return m.st.title.Render(strconv.Itoa(int(r[0].Port))) + m.st.dim.Render(" on ") +
		strings.Join(addrs, m.st.dim.Render(", ")) + m.st.dim.Render(" · "+note)
}

func (m *model) clientsLine(n int) string {
	if n == 0 {
		return m.st.dim.Render("none connected")
	}
	return m.st.text.Render(strconv.Itoa(n) + " connected")
}

// texts renders each line in the normal text color.
func (m *model) texts(lines []string) []string {
	for i, l := range lines {
		lines[i] = m.st.text.Render(l)
	}
	return lines
}

// formatStart renders a start time relative to now: "today 09:14",
// "yesterday 18:02", "Oct 3 11:20", or "Dec 24 2025" for older years.
func formatStart(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	yesterday := now.AddDate(0, 0, -1)
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "today " + t.Format("15:04")
	case t.Year() == yesterday.Year() && t.YearDay() == yesterday.YearDay():
		return "yesterday " + t.Format("15:04")
	case y1 == y2:
		return t.Format("Jan 2 15:04")
	}
	return t.Format("Jan 2 2006")
}

// wrap breaks s into at most maxRows lines of width columns at spaces. A
// word longer than a line, such as a path, is split, after a slash where
// possible. The last line ends with "…" if s had to be cut.
func wrap(s string, width, maxRows int) []string {
	var lines []string
	var line []rune
	flush := func() {
		if len(line) > 0 {
			lines = append(lines, string(line))
			line = nil
		}
	}
	for _, word := range strings.Fields(s) {
		w := []rune(word)
		for len(w) > width {
			flush()
			cut := width
			if i := lastIndex(w[:width], '/'); i > 0 {
				cut = i + 1
			}
			lines = append(lines, string(w[:cut]))
			w = w[cut:]
		}
		switch {
		case len(w) == 0:
		case len(line) == 0:
			line = w
		case len(line)+1+len(w) <= width:
			line = append(append(line, ' '), w...)
		default:
			flush()
			line = w
		}
	}
	flush()
	if len(lines) > maxRows {
		lines = lines[:maxRows]
		last := []rune(lines[maxRows-1])
		lines[maxRows-1] = string(last[:min(len(last), width-1)]) + "…"
	}
	return lines
}

func lastIndex(r []rune, c rune) int {
	for i := len(r) - 1; i >= 0; i-- {
		if r[i] == c {
			return i
		}
	}
	return -1
}

// box draws lines inside a rounded border of the given width with title
// set into the top edge.
func box(st styles, title string, lines []string, width int) []string {
	border := st.border
	inner := width - 4
	top := border.Render("╭─ ") + ansi.Truncate(title, inner-2, "…") + " "
	top += border.Render(strings.Repeat("─", max(0, width-lipgloss.Width(top)-1)) + "╮")

	out := []string{top}
	for _, l := range lines {
		l = ansi.Truncate(l, inner, "…")
		pad := strings.Repeat(" ", max(0, inner-lipgloss.Width(l)))
		out = append(out, border.Render("│")+" "+l+pad+" "+border.Render("│"))
	}
	return append(out, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
}
