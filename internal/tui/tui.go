// Package tui is peek's interactive mode: a live, full-screen view of the
// listening ports where the selected process can be stopped with a key.
package tui

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/peek/internal/kill"
	"github.com/skinleak/peek/internal/scan"
	"github.com/skinleak/peek/internal/ui"
)

const (
	// highlightFor is how long new rows are marked and closed rows linger.
	highlightFor = 3 * time.Second
	// statusFor is how long a status message stays in the footer.
	statusFor = 6 * time.Second
	// chromeLines is the number of lines around the table: title, blank,
	// header, blank, details and footer.
	chromeLines = 6
)

// Config configures the interactive view.
type Config struct {
	Ranges   []scan.PortRange // ports being watched, for the title; empty means all
	Interval time.Duration    // how often to rescan
	Home     string           // home directory to abbreviate as "~"
	Root     bool             // running as root, so no owners are hidden

	// Scan lists the listeners to show, already filtered to Ranges.
	Scan func() ([]scan.Listener, error)
	// Stop terminates a target without asking, as `peek kill --yes` does.
	Stop func(t kill.Target, force bool) error
}

// Run shows the interactive view until the user quits.
func Run(cfg Config) error {
	m := newModel(cfg, ui.NewRenderer(os.Stdout, ui.ColorEnabled(os.Stdout)))
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type (
	tickMsg time.Time
	scanMsg struct {
		ls  []scan.Listener
		err error
	}
	stopMsg struct {
		target kill.Target
		err    error
	}
)

// entry is a row as displayed: live, or closed recently and fading out.
type entry struct {
	row  ui.Row
	key  string
	gone bool
	new  bool
}

type closedRow struct {
	row ui.Row
	at  time.Time
}

// pendingStop is a stop waiting for the user to confirm it.
type pendingStop struct {
	target kill.Target
	force  bool
}

type model struct {
	cfg    Config
	render *lipgloss.Renderer
	st     styles
	now    func() time.Time

	width, height int

	rows      []ui.Row             // from the latest scan
	firstSeen map[string]time.Time // when each live row appeared; zero for the first scan
	closed    map[string]closedRow // rows that disappeared recently
	scanned   bool                 // a scan has completed
	scanning  bool

	cursorKey string // the selected row, tracked by key across rescans
	cursorIdx int    // its last position, for when the row goes away
	offset    int    // first visible row

	filter    string
	filtering bool // typing a filter

	confirm  *pendingStop
	stopping bool

	status    string
	statusErr bool
	statusAt  time.Time
}

func newModel(cfg Config, r *lipgloss.Renderer) *model {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	return &model{
		cfg:       cfg,
		render:    r,
		st:        newStyles(r),
		now:       time.Now,
		firstSeen: make(map[string]time.Time),
		closed:    make(map[string]closedRow),
	}
}

func (m *model) Init() tea.Cmd {
	m.scanning = true
	return tea.Batch(m.scanCmd(), m.tickCmd())
}

func (m *model) scanCmd() tea.Cmd {
	return func() tea.Msg {
		ls, err := m.cfg.Scan()
		return scanMsg{ls, err}
	}
}

func (m *model) tickCmd() tea.Cmd {
	return tea.Tick(m.cfg.Interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		cmds := []tea.Cmd{m.tickCmd()}
		if !m.scanning {
			m.scanning = true
			cmds = append(cmds, m.scanCmd())
		}
		return m, tea.Batch(cmds...)
	case scanMsg:
		m.scanning = false
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return m, nil
		}
		m.applyScan(msg.ls)
	case stopMsg:
		m.stopping = false
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		} else {
			m.setStatus("Stopped "+msg.target.String(), false)
		}
		return m, m.refresh()
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// applyScan replaces the rows, remembering which ones are new and which
// just closed so they can be highlighted.
func (m *model) applyScan(ls []scan.Listener) {
	now := m.now()
	rows := ui.GroupRows(ls)
	live := make(map[string]bool, len(rows))
	for _, r := range rows {
		k := r.Key()
		live[k] = true
		if _, ok := m.firstSeen[k]; ok {
			continue
		}
		if m.scanned {
			m.firstSeen[k] = now
		} else {
			m.firstSeen[k] = time.Time{} // present from the start, not new
		}
		delete(m.closed, k)
	}
	for _, r := range m.rows {
		if k := r.Key(); !live[k] {
			m.closed[k] = closedRow{row: r, at: now}
			delete(m.firstSeen, k)
		}
	}
	m.rows = rows
	m.scanned = true
}

// refresh starts a scan now unless one is already running.
func (m *model) refresh() tea.Cmd {
	if m.scanning {
		return nil
	}
	m.scanning = true
	return m.scanCmd()
}

func (m *model) setStatus(s string, isErr bool) {
	m.status, m.statusErr, m.statusAt = s, isErr, m.now()
}

func (m *model) handleKey(k tea.KeyMsg) tea.Cmd {
	if k.Type == tea.KeyCtrlC {
		return tea.Quit
	}
	switch {
	case m.confirm != nil:
		return m.handleConfirmKey(k)
	case m.filtering:
		m.handleFilterKey(k)
		return nil
	}

	entries := m.entries()
	switch k.String() {
	case "q":
		return tea.Quit
	case "up", "k":
		m.moveCursor(entries, -1)
	case "down", "j":
		m.moveCursor(entries, 1)
	case "pgup":
		m.moveCursor(entries, -m.bodyHeight())
	case "pgdown":
		m.moveCursor(entries, m.bodyHeight())
	case "home", "g":
		m.moveCursor(entries, -len(entries))
	case "end", "G":
		m.moveCursor(entries, len(entries))
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
	case "r":
		return m.refresh()
	case "x", "delete":
		m.askStop(entries, false)
	case "X":
		m.askStop(entries, true)
	}
	return nil
}

func (m *model) handleConfirmKey(k tea.KeyMsg) tea.Cmd {
	p := m.confirm
	m.confirm = nil
	if k.String() != "y" && k.String() != "Y" {
		m.setStatus("Cancelled", false)
		return nil
	}
	m.stopping = true
	m.setStatus(fmt.Sprintf("Stopping %s…", p.target), false)
	return func() tea.Msg {
		return stopMsg{p.target, m.cfg.Stop(p.target, p.force)}
	}
}

func (m *model) handleFilterKey(k tea.KeyMsg) {
	switch k.Type {
	case tea.KeyEnter:
		m.filtering = false
	case tea.KeyEsc:
		m.filtering, m.filter = false, ""
	case tea.KeyBackspace:
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.filter += string(k.Runes)
	}
}

// askStop asks to confirm stopping the selected row's owner.
func (m *model) askStop(entries []entry, force bool) {
	if m.stopping {
		return
	}
	i := m.cursor(entries)
	if i < 0 {
		return
	}
	e := entries[i]
	if e.gone {
		m.setStatus(fmt.Sprintf("Port %d is already closed", e.row[0].Port), false)
		return
	}
	targets, _ := kill.Targets(e.row)
	if len(targets) == 0 {
		m.setStatus(fmt.Sprintf("Can't see which process owns port %d; run peek with sudo", e.row[0].Port), true)
		return
	}
	m.confirm = &pendingStop{target: targets[0], force: force}
}

// entries returns the rows to display: live and recently closed ones that
// match the filter, ordered by port.
func (m *model) entries() []entry {
	now := m.now()
	var out []entry
	for _, r := range m.rows {
		k := r.Key()
		seen := m.firstSeen[k]
		out = append(out, entry{row: r, key: k, new: !seen.IsZero() && now.Sub(seen) < highlightFor})
	}
	for k, c := range m.closed {
		if now.Sub(c.at) >= highlightFor {
			delete(m.closed, k)
			continue
		}
		out = append(out, entry{row: c.row, key: k, gone: true})
	}
	out = slices.DeleteFunc(out, func(e entry) bool { return !matches(e.row, m.filter) })
	slices.SortStableFunc(out, func(a, b entry) int {
		return cmp.Or(cmp.Compare(a.row[0].Port, b.row[0].Port), strings.Compare(a.key, b.key))
	})
	return out
}

// matches reports whether any of the row's fields contains filter, ignoring case.
func matches(r ui.Row, filter string) bool {
	if filter == "" {
		return true
	}
	l := r[0]
	fields := []string{strconv.Itoa(int(l.Port)), l.ProcessName, l.Container, l.Cwd, l.User, strings.Join(l.Command, " ")}
	if l.PID != 0 {
		fields = append(fields, strconv.Itoa(l.PID))
	}
	for _, x := range r {
		fields = append(fields, x.Address.String())
	}
	filter = strings.ToLower(filter)
	return slices.ContainsFunc(fields, func(f string) bool {
		return strings.Contains(strings.ToLower(f), filter)
	})
}

// cursor returns the index of the selected entry, following the selected
// row by key and falling back to its last position when it's gone. It
// returns -1 when there are no entries.
func (m *model) cursor(entries []entry) int {
	if len(entries) == 0 {
		return -1
	}
	i := slices.IndexFunc(entries, func(e entry) bool { return e.key == m.cursorKey })
	if i < 0 {
		i = min(m.cursorIdx, len(entries)-1)
	}
	m.cursorKey, m.cursorIdx = entries[i].key, i
	return i
}

func (m *model) moveCursor(entries []entry, delta int) {
	i := m.cursor(entries)
	if i < 0 {
		return
	}
	i = max(0, min(len(entries)-1, i+delta))
	m.cursorKey, m.cursorIdx = entries[i].key, i
}

func (m *model) bodyHeight() int {
	if m.height == 0 {
		return 20 // size not known yet
	}
	return max(1, m.height-chromeLines)
}

func (m *model) View() string {
	entries := m.entries()
	cur := m.cursor(entries)

	var b strings.Builder
	b.WriteString(m.titleLine(len(m.rows)) + "\n\n")

	body := m.bodyHeight()
	switch {
	case !m.scanned:
		b.WriteString(m.st.dim.Render("  Scanning…") + "\n")
		body--
	case len(entries) == 0:
		b.WriteString("  " + m.emptyMessage() + "\n")
		body--
	default:
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
		b.WriteString("  " + lines[0] + "\n")
		m.offset = scrollOffset(m.offset, cur, body, len(entries))
		end := min(len(entries), m.offset+body)
		for i := m.offset; i < end; i++ {
			b.WriteString(m.gutter(entries[i], i == cur) + lines[i+1] + "\n")
		}
		body -= end - m.offset
	}
	b.WriteString(strings.Repeat("\n", max(0, body)+1))

	if cur >= 0 {
		b.WriteString(m.truncate(m.details(entries[cur])))
	}
	b.WriteString("\n" + m.truncate(m.footer()))
	return b.String()
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

func (m *model) titleLine(n int) string {
	parts := []string{fmt.Sprintf("%d listening", n)}
	if len(m.cfg.Ranges) > 0 {
		parts = append(parts, scan.DescribePorts(m.cfg.Ranges))
	}
	if m.filter != "" && !m.filtering {
		parts = append(parts, fmt.Sprintf("filter %q", m.filter))
	}
	if !m.cfg.Root && m.hasHiddenOwners() {
		parts = append(parts, "some owners hidden, run with sudo to see them")
	}
	return m.truncate(" " + m.st.title.Render("peek") + "  " + m.st.dim.Render(strings.Join(parts, " · ")))
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
	if e.gone {
		parts = append([]string{"closed"}, parts...)
	}
	return " " + m.st.dim.Render(strings.Join(parts, " · "))
}

func (m *model) footer() string {
	switch {
	case m.confirm != nil:
		return " " + m.st.prompt.Render(m.confirm.target.Action(m.confirm.force)+"?") + m.st.dim.Render(" [y/N]")
	case m.filtering:
		return " /" + m.filter + m.st.cursor.Render(" ") + m.st.dim.Render("  enter apply · esc clear")
	case m.status != "" && (m.stopping || m.now().Sub(m.statusAt) < statusFor):
		if m.statusErr {
			return " " + m.st.err.Render(m.status)
		}
		return " " + m.st.ok.Render(m.status)
	}
	keys := []string{"↑↓ move", "x stop", "X force kill", "/ filter", "r refresh", "q quit"}
	if m.filter != "" {
		keys = slices.Insert(keys, 4, "esc clear filter")
	}
	return " " + m.st.dim.Render(strings.Join(keys, "  "))
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

type styles struct {
	title, dim, added, prompt, ok, err, cursor lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) styles {
	return styles{
		title:  r.NewStyle().Bold(true).Foreground(ui.Accent),
		dim:    r.NewStyle().Foreground(ui.Muted),
		added:  r.NewStyle().Bold(true).Foreground(ui.Success),
		prompt: r.NewStyle().Bold(true).Foreground(ui.Warning),
		ok:     r.NewStyle().Foreground(ui.Success),
		err:    r.NewStyle().Foreground(ui.Danger),
		cursor: r.NewStyle().Reverse(true),
	}
}
