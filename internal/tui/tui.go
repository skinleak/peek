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
	// firstRowLine is the screen line of the first table row.
	firstRowLine = 3
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
	// Open opens a URL in the browser; Copy puts text on the clipboard.
	// They default to the system's browser and clipboard.
	Open func(url string) error
	Copy func(text string) error
}

// Run shows the interactive view until the user quits.
func Run(cfg Config) error {
	if cfg.Open == nil {
		cfg.Open = openURL
	}
	if cfg.Copy == nil {
		cfg.Copy = copyText
	}
	m := newModel(cfg, ui.NewRenderer(os.Stdout, ui.ColorEnabled(os.Stdout)))
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
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
	// actionMsg reports the result of opening or copying.
	actionMsg struct {
		done string // status on success
		err  error
	}
	spinMsg struct{}
)

// mode is what fills the screen.
type mode int

const (
	modeList   mode = iota
	modeDetail      // the detail panel for the selected row
	modeHelp        // the key reference
)

// spinInterval paces the spinner shown while a process is being stopped.
const spinInterval = 100 * time.Millisecond

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
	mode          mode

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

	sortBy   sortOrder
	reversed bool

	confirm  *pendingStop
	stopping bool
	spinner  int // current spinner frame while stopping

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
	case spinMsg:
		if !m.stopping {
			return m, nil
		}
		m.spinner = (m.spinner + 1) % len(spinFrames)
		return m, m.spinCmd()
	case actionMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		} else {
			m.setStatus(msg.done, false)
		}
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
	case tea.MouseMsg:
		m.handleMouse(msg)
	}
	return m, nil
}

func (m *model) spinCmd() tea.Cmd {
	return tea.Tick(spinInterval, func(time.Time) tea.Msg { return spinMsg{} })
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
	case m.mode == modeHelp:
		m.mode = modeList // any key closes the help
		if k.String() == "q" {
			return tea.Quit
		}
		return nil
	}

	entries := m.entries()
	switch k.String() {
	case "q":
		return tea.Quit
	case "?":
		m.mode = modeHelp
	case "enter":
		if m.mode == modeDetail {
			m.mode = modeList
		} else if m.cursor(entries) >= 0 {
			m.mode = modeDetail
		}
	case "o":
		return m.act(entries, "open")
	case "c":
		return m.act(entries, "copy")
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
		m.mode, m.filtering = modeList, true
	case "esc":
		if m.mode == modeDetail {
			m.mode = modeList
		} else {
			m.filter = ""
		}
	case "s":
		m.sortBy = (m.sortBy + 1) % numSortOrders
	case "S":
		m.reversed = !m.reversed
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
	stop := func() tea.Msg {
		return stopMsg{p.target, m.cfg.Stop(p.target, p.force)}
	}
	return tea.Batch(stop, m.spinCmd())
}

// act opens the selected row's URL in the browser or copies it.
func (m *model) act(entries []entry, action string) tea.Cmd {
	i := m.cursor(entries)
	if i < 0 {
		return nil
	}
	if entries[i].gone {
		m.setStatus(fmt.Sprintf("Port %d is already closed", entries[i].row[0].Port), false)
		return nil
	}
	url := browseURL(entries[i].row)
	if action == "open" {
		return func() tea.Msg { return actionMsg{"Opened " + url, m.cfg.Open(url)} }
	}
	return func() tea.Msg { return actionMsg{"Copied " + url, m.cfg.Copy(url)} }
}

// handleMouse scrolls with the wheel and selects rows by clicking. Clicking
// the selected row again opens its details.
func (m *model) handleMouse(msg tea.MouseMsg) {
	if m.confirm != nil || m.filtering {
		return
	}
	entries := m.entries()
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		m.moveCursor(entries, -1)
	case msg.Button == tea.MouseButtonWheelDown:
		m.moveCursor(entries, 1)
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && m.mode == modeList:
		i := m.offset + msg.Y - firstRowLine
		if msg.Y < firstRowLine || i >= min(len(entries), m.offset+m.bodyHeight()) {
			return
		}
		if entries[i].key == m.cursorKey {
			m.mode = modeDetail
		}
		m.cursorKey, m.cursorIdx = entries[i].key, i
	case m.mode == modeHelp && msg.Action == tea.MouseActionPress:
		m.mode = modeList
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
	slices.SortStableFunc(out, func(a, b entry) int { return m.compare(a, b) })
	return out
}

// sortOrder is a way of ordering the rows, cycled with s.
type sortOrder int

const (
	byPort    sortOrder = iota
	byProcess           // alphabetically, by process or container name
	byUptime            // newest first
	numSortOrders
)

var sortNames = [numSortOrders]string{"port", "process", "uptime"}

func (m *model) sortDescription() string {
	d := "sorted by " + sortNames[m.sortBy]
	if m.reversed {
		d += ", reversed"
	}
	return d
}

// compare orders entries by the current sort. Rows missing the sort field,
// such as processes owned by other users, stay at the end even when the
// order is reversed. Ties fall back to port, then key, so the order is stable.
func (m *model) compare(a, b entry) int {
	la, lb := a.row[0], b.row[0]
	var c int
	switch m.sortBy {
	case byPort:
		c = cmp.Compare(la.Port, lb.Port)
	case byProcess:
		na, nb := strings.ToLower(displayName(la)), strings.ToLower(displayName(lb))
		if d := cmp.Compare(boolInt(na == ""), boolInt(nb == "")); d != 0 {
			return d
		}
		c = strings.Compare(na, nb)
	case byUptime:
		if d := cmp.Compare(boolInt(la.StartTime.IsZero()), boolInt(lb.StartTime.IsZero())); d != 0 {
			return d
		}
		c = lb.StartTime.Compare(la.StartTime) // later start first
	}
	if m.reversed {
		c = -c
	}
	return cmp.Or(c, cmp.Compare(la.Port, lb.Port), strings.Compare(a.key, b.key))
}

// displayName is the name the table shows for a row's owner.
func displayName(l scan.Listener) string {
	if l.Container != "" {
		return l.Container
	}
	return l.ProcessName
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
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

type styles struct {
	title, bold, dim, added, prompt, ok, warn, err, cursor, border, key lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) styles {
	return styles{
		title:  r.NewStyle().Bold(true).Foreground(ui.Accent),
		bold:   r.NewStyle().Bold(true),
		dim:    r.NewStyle().Foreground(ui.Muted),
		added:  r.NewStyle().Bold(true).Foreground(ui.Success),
		prompt: r.NewStyle().Bold(true).Foreground(ui.Warning),
		ok:     r.NewStyle().Foreground(ui.Success),
		warn:   r.NewStyle().Foreground(ui.Warning),
		err:    r.NewStyle().Foreground(ui.Danger),
		cursor: r.NewStyle().Reverse(true),
		border: r.NewStyle().Foreground(ui.Muted),
		key:    r.NewStyle().Bold(true).Foreground(ui.Accent),
	}
}
