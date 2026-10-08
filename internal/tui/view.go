package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/peek/internal/scan"
	"github.com/skinleak/peek/internal/ui"
)

func (m *model) bodyHeight() int {
	if m.height == 0 {
		return 20 // size not known yet
	}
	return max(1, m.height-chromeLines)
}

func (m *model) View() string {
	entries := m.entries()
	cur := m.cursor(entries)
	if cur < 0 && m.mode == modeDetail {
		m.mode = modeList
	}

	var b strings.Builder
	b.WriteString(m.titleLine() + "\n\n")

	var body []string
	switch {
	case m.mode == modeHelp:
		body = m.help()
	case !m.scanned:
		body = []string{m.st.dim.Render("  Scanning…")}
	case len(entries) == 0:
		body = []string{"  " + m.emptyMessage()}
	case m.mode == modeDetail:
		for _, l := range m.detailPanel(entries[cur], max(40, m.width-2)) {
			body = append(body, " "+l)
		}
	default:
		body = m.list(entries, cur)
	}
	// The table header sits on the line chromeLines reserves for it; the
	// other screens use that line too.
	height := m.bodyHeight() + 1
	if len(body) > height {
		body = body[:height]
	}
	for _, l := range body {
		b.WriteString(m.truncate(l) + "\n")
	}
	b.WriteString(strings.Repeat("\n", height-len(body)+1))

	if cur >= 0 && m.mode == modeList {
		b.WriteString(m.truncate(m.details(entries[cur])))
	}
	b.WriteString("\n" + m.truncate(m.footer()))
	return b.String()
}

// list renders the table header and the visible rows.
func (m *model) list(entries []entry, cur int) []string {
	// Render every entry, not just the visible ones, so column widths
	// don't change while scrolling.
	rows := make([]ui.Row, len(entries))
	for i, e := range entries {
		rows[i] = e.row
	}
	o := ui.Options{Width: max(0, m.width-2), Home: m.cfg.Home, Now: m.now()}
	lines := ui.Lines(m.render, rows, o, func(i int) ui.RowStyle {
		return ui.RowStyle{Selected: i == cur, Faded: entries[i].gone}
	})
	out := []string{"  " + lines[0]}
	m.offset = scrollOffset(m.offset, cur, m.bodyHeight(), len(entries))
	for i := m.offset; i < min(len(entries), m.offset+m.bodyHeight()); i++ {
		out = append(out, m.gutter(entries[i], i == cur)+lines[i+1])
	}
	return out
}

// scrollOffset keeps the cursor within the visible window of height rows.
func scrollOffset(offset, cursor, height, total int) int {
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+height {
		offset = cursor - height + 1
	}
	return max(0, min(offset, total-height))
}

// titleLine summarizes what's on screen: how many ports are listening, how
// many of them are reachable from other machines, and the active view
// settings.
func (m *model) titleLine() string {
	sep := m.st.dim.Render(" · ")
	parts := []string{m.st.bold.Render(strconv.Itoa(len(m.rows))) + m.st.dim.Render(" listening")}
	exposed, containers := 0, 0
	for _, r := range m.rows {
		if slices.ContainsFunc(r, scan.Listener.Exposed) {
			exposed++
		}
		if r[0].ContainerID != "" {
			containers++
		}
	}
	if exposed > 0 {
		parts = append(parts, m.st.warn.Render(fmt.Sprintf("%d exposed", exposed)))
	}
	if containers > 0 {
		parts = append(parts, m.st.dim.Render(fmt.Sprintf("%d docker", containers)))
	}
	var extra []string
	if len(m.cfg.Ranges) > 0 {
		extra = append(extra, scan.DescribePorts(m.cfg.Ranges))
	}
	if m.sortBy != byPort || m.reversed {
		extra = append(extra, m.sortDescription())
	}
	if m.filter != "" && !m.filtering {
		extra = append(extra, fmt.Sprintf("filter %q", m.filter))
	}
	if !m.cfg.Root && m.hasHiddenOwners() {
		extra = append(extra, "some owners hidden, run with sudo to see them")
	}
	for _, e := range extra {
		parts = append(parts, m.st.dim.Render(e))
	}
	return m.truncate(" " + m.st.title.Render("peek") + "  " + strings.Join(parts, sep))
}

func (m *model) hasHiddenOwners() bool {
	return slices.ContainsFunc(m.rows, func(r ui.Row) bool { return r[0].PID == 0 && r[0].ContainerID == "" })
}

func (m *model) emptyMessage() string {
	switch {
	case m.filter != "":
		return m.st.dim.Render(fmt.Sprintf("Nothing matches %q. Press esc to clear the filter.", m.filter))
	case len(m.cfg.Ranges) > 0:
		return m.st.dim.Render("Nothing is listening on " + scan.DescribePorts(m.cfg.Ranges) + ".")
	default:
		return m.st.dim.Render("Nothing is listening.")
	}
}

func (m *model) gutter(e entry, selected bool) string {
	switch {
	case selected:
		return m.st.title.Render("›") + " "
	case e.new:
		return m.st.added.Render("+") + " "
	case e.gone:
		return m.st.dim.Render("-") + " "
	default:
		return "  "
	}
}

// details describes the selected row beyond what fits in the table.
func (m *model) details(e entry) string {
	l := e.row[0]
	var parts []string
	switch {
	case l.ContainerID != "":
		parts = append(parts, fmt.Sprintf("container %s (%s)", l.Container, shortID(l.ContainerID)))
	case len(l.Command) > 0:
		parts = append(parts, strings.Join(l.Command, " "))
	}
	if l.User != "" {
		parts = append(parts, "user "+l.User)
	}
	if l.Connections > 0 {
		parts = append(parts, pluralize(l.Connections, "client", "clients"))
	}
	if e.gone {
		parts = append([]string{"closed"}, parts...)
	}
	return " " + m.st.dim.Render(strings.Join(parts, " · "))
}

// keyHelp is the reference shown by ?.
var keyHelp = []struct{ keys, action string }{
	{"↑ ↓  j k", "move the selection"},
	{"PgUp PgDn  g G", "jump a page, or to the top or bottom"},
	{"enter", "show details for the selected port"},
	{"o", "open the port in your browser"},
	{"c", "copy its URL"},
	{"x", "stop the process (SIGTERM) or Docker container"},
	{"X", "force kill it (SIGKILL / docker kill)"},
	{"/", "filter by port, process, command, directory or user"},
	{"s  S", "change the sort order, reverse it"},
	{"r", "rescan now"},
	{"esc", "go back, or clear the filter"},
	{"q", "quit"},
}

func (m *model) help() []string {
	width := 0
	for _, k := range keyHelp {
		width = max(width, lipgloss.Width(k.keys))
	}
	var lines []string
	for _, k := range keyHelp {
		pad := strings.Repeat(" ", width-lipgloss.Width(k.keys)+3)
		lines = append(lines, m.st.key.Render(k.keys)+pad+k.action)
	}
	lines = append(lines, "", m.st.dim.Render("Click a row to select it, click it again for details."))
	var out []string
	for _, l := range box(m.st, m.st.title.Render("keys"), lines, min(max(40, m.width-2), 72)) {
		out = append(out, " "+l)
	}
	return out
}

func (m *model) footer() string {
	switch {
	case m.confirm != nil:
		return " " + m.st.prompt.Render(m.confirm.target.Action(m.confirm.force)+"?") + m.st.dim.Render(" [y/N]")
	case m.filtering:
		return " /" + m.filter + m.st.cursor.Render(" ") + m.st.dim.Render("  enter apply · esc clear")
	case m.stopping:
		return " " + m.st.title.Render(spinFrames[m.spinner]) + " " + m.status
	case m.status != "" && m.now().Sub(m.statusAt) < statusFor:
		if m.statusErr {
			return " " + m.st.err.Render("✗ "+m.status)
		}
		return " " + m.st.ok.Render("✓ "+m.status)
	}
	var keys [][2]string
	switch m.mode {
	case modeHelp:
		keys = [][2]string{{"any key", "close"}}
	case modeDetail:
		keys = [][2]string{{"esc", "back"}, {"o", "open in browser"}, {"c", "copy URL"}, {"x", "stop"}, {"X", "force kill"}, {"q", "quit"}}
	default:
		keys = [][2]string{{"↑↓", "move"}, {"enter", "details"}, {"o", "open"}, {"x", "stop"}, {"/", "filter"}, {"s", "sort"}, {"?", "help"}, {"q", "quit"}}
		if m.filter != "" {
			keys = slices.Insert(keys, 5, [2]string{"esc", "clear filter"})
		}
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = m.st.key.Render(k[0]) + " " + m.st.dim.Render(k[1])
	}
	return " " + strings.Join(parts, "   ")
}

func (m *model) truncate(s string) string {
	if m.width <= 0 {
		return s
	}
	return ansi.Truncate(s, m.width, "…")
}

func shortID(id string) string {
	return id[:min(12, len(id))]
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
