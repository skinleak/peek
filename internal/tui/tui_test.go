package tui

import (
	"bytes"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/skinleak/peek/internal/kill"
	"github.com/skinleak/peek/internal/scan"
	"github.com/skinleak/peek/internal/ui"
)

var start = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func listener(port uint16, pid int, name string) scan.Listener {
	return scan.Listener{
		Port: port, Protocol: "tcp", Address: netip.MustParseAddr("127.0.0.1"),
		PID: pid, ProcessName: name, User: "dev",
	}
}

// testModel returns a model without colors, a fixed clock and an 80x24 screen.
type testModel struct {
	*model
	clock   time.Time
	stopped []kill.Target
	opened  []string
	copied  []string
}

func newTestModel(t *testing.T) *testModel {
	t.Helper()
	tm := &testModel{clock: start}
	m := newModel(Config{
		Stop: func(target kill.Target, force bool) error {
			tm.stopped = append(tm.stopped, target)
			return nil
		},
		Open: func(url string) error {
			tm.opened = append(tm.opened, url)
			return nil
		},
		Copy: func(text string) error {
			tm.copied = append(tm.copied, text)
			return nil
		},
		Home: "/home/dev",
	}, ui.NewRenderer(&bytes.Buffer{}, false))
	m.now = func() time.Time { return tm.clock }
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm.model = m
	return tm
}

func (tm *testModel) scan(ls ...scan.Listener) {
	tm.Update(scanMsg{ls: ls})
}

func (tm *testModel) press(keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "up", "down", "enter", "esc", "backspace":
			msg = tea.KeyMsg{Type: map[string]tea.KeyType{
				"up": tea.KeyUp, "down": tea.KeyDown, "enter": tea.KeyEnter,
				"esc": tea.KeyEsc, "backspace": tea.KeyBackspace,
			}[k]}
		case "?":
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		_, cmd = tm.Update(msg)
	}
	return cmd
}

// run executes cmd, including every command in a batch, and feeds the
// resulting messages to the model. Commands those messages return aren't run.
func (tm *testModel) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			tm.run(c)
		}
		return
	}
	tm.Update(msg)
}

// selected returns the port of the selected row.
func (tm *testModel) selected() uint16 {
	entries := tm.entries()
	i := tm.cursor(entries)
	if i < 0 {
		return 0
	}
	return entries[i].row[0].Port
}

func TestSelectionFollowsRowAcrossScans(t *testing.T) {
	tm := newTestModel(t)
	tm.scan(listener(3000, 1, "node"), listener(5432, 2, "postgres"), listener(8080, 3, "nginx"))
	tm.press("down")
	if got := tm.selected(); got != 5432 {
		t.Fatalf("selected %d after moving down, want 5432", got)
	}

	// A new row above the selection must not move it.
	tm.scan(listener(22, 9, "sshd"), listener(3000, 1, "node"), listener(5432, 2, "postgres"), listener(8080, 3, "nginx"))
	if got := tm.selected(); got != 5432 {
		t.Errorf("selected %d after a row was added above, want 5432", got)
	}

	// When the selected row closes and fades out, the selection stays put.
	tm.scan(listener(22, 9, "sshd"), listener(3000, 1, "node"), listener(8080, 3, "nginx"))
	tm.clock = tm.clock.Add(highlightFor)
	if got := tm.selected(); got != 8080 {
		t.Errorf("selected %d after the selected row closed, want 8080", got)
	}
}

func TestStopAsksForConfirmation(t *testing.T) {
	tm := newTestModel(t)
	tm.scan(listener(3000, 4242, "node"))

	tm.press("x")
	if !strings.Contains(tm.View(), "Send SIGTERM to node (PID 4242) on port 3000? [y/N]") {
		t.Fatalf("expected a confirmation prompt:\n%s", tm.View())
	}
	tm.press("n")
	if len(tm.stopped) != 0 || tm.confirm != nil {
		t.Fatal("declining the prompt must not stop anything")
	}

	tm.press("X")
	if !strings.Contains(tm.View(), "Send SIGKILL to node (PID 4242)") {
		t.Fatalf("expected a SIGKILL prompt:\n%s", tm.View())
	}
	cmd := tm.press("y")
	if cmd == nil {
		t.Fatal("confirming should return a command that stops the process")
	}
	tm.run(cmd)
	if len(tm.stopped) != 1 || tm.stopped[0].PID != 4242 {
		t.Fatalf("stopped %+v, want PID 4242", tm.stopped)
	}
	if !strings.Contains(tm.View(), "Stopped node (PID 4242) on port 3000") {
		t.Errorf("expected a success message:\n%s", tm.View())
	}
}

func TestStopRefusesHiddenOwners(t *testing.T) {
	tm := newTestModel(t)
	tm.scan(scan.Listener{Port: 631, Address: netip.MustParseAddr("127.0.0.1"), User: "root"})
	tm.press("x")
	if tm.confirm != nil {
		t.Fatal("must not offer to stop a process it can't see")
	}
	if !strings.Contains(tm.View(), "run peek with sudo") {
		t.Errorf("expected a hint to use sudo:\n%s", tm.View())
	}
}

func TestFilter(t *testing.T) {
	tm := newTestModel(t)
	pg := listener(5432, 2, "postgres")
	pg.Command = []string{"postgres", "-D", "/var/lib/postgresql"}
	tm.scan(listener(3000, 1, "node"), pg, listener(8080, 3, "nginx"))

	tm.press("/", "p", "o", "s", "t", "enter")
	view := tm.View()
	if !strings.Contains(view, "postgres") || strings.Contains(view, "nginx") {
		t.Errorf("filter should keep only postgres:\n%s", view)
	}
	if tm.selected() != 5432 {
		t.Errorf("selected %d, want the only match 5432", tm.selected())
	}

	tm.press("/", "backspace", "backspace", "backspace", "backspace", "x", "y", "z", "enter")
	if !strings.Contains(tm.View(), `Nothing matches "xyz"`) {
		t.Errorf("expected a no-match message:\n%s", tm.View())
	}

	tm.press("esc")
	if !strings.Contains(tm.View(), "nginx") {
		t.Errorf("esc should clear the filter:\n%s", tm.View())
	}
}

func TestNewAndClosedRowsAreMarked(t *testing.T) {
	tm := newTestModel(t)
	tm.scan(listener(3000, 1, "node"), listener(8080, 3, "nginx"))
	if strings.Contains(tm.View(), "+ ") {
		t.Errorf("rows from the first scan aren't new:\n%s", tm.View())
	}

	tm.scan(listener(3000, 1, "node"), listener(5432, 2, "postgres"))
	view := tm.View()
	if !hasLine(view, "+ 5432") {
		t.Errorf("expected postgres marked new:\n%s", view)
	}
	if !hasLine(view, "- 8080") {
		t.Errorf("expected nginx marked closed:\n%s", view)
	}

	tm.clock = tm.clock.Add(highlightFor)
	view = tm.View()
	if strings.Contains(view, "8080") || hasLine(view, "+ 5432") {
		t.Errorf("marks should expire after %s:\n%s", highlightFor, view)
	}
}

func TestScrollOffset(t *testing.T) {
	tests := []struct{ offset, cursor, height, total, want int }{
		{0, 0, 5, 20, 0},
		{0, 4, 5, 20, 0},
		{0, 5, 5, 20, 1},
		{10, 3, 5, 20, 3},
		{18, 19, 5, 20, 15},
		{4, 2, 5, 3, 0}, // everything fits
	}
	for _, tt := range tests {
		if got := scrollOffset(tt.offset, tt.cursor, tt.height, tt.total); got != tt.want {
			t.Errorf("scrollOffset(%d, %d, %d, %d) = %d, want %d", tt.offset, tt.cursor, tt.height, tt.total, got, tt.want)
		}
	}
}

func hasLine(view, prefix string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(strings.TrimRight(line, " "), prefix) {
			return true
		}
	}
	return false
}

func TestViewFitsScreenWhenScrolling(t *testing.T) {
	tm := newTestModel(t)
	tm.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	var ls []scan.Listener
	for i := range 30 {
		ls = append(ls, listener(uint16(3000+i), 100+i, "node"))
	}
	tm.scan(ls...)
	for range 25 {
		tm.press("down")
		view := tm.View()
		if n := strings.Count(view, "\n") + 1; n != 12 {
			t.Fatalf("view is %d lines, want 12:\n%s", n, view)
		}
		if !strings.Contains(view, "› ") {
			t.Fatalf("selected row scrolled out of view:\n%s", view)
		}
	}
}

// order returns the ports of the displayed rows, top to bottom.
func (tm *testModel) order() []uint16 {
	var ports []uint16
	for _, e := range tm.entries() {
		ports = append(ports, e.row[0].Port)
	}
	return ports
}

func TestSort(t *testing.T) {
	tm := newTestModel(t)
	redis := listener(6379, 1, "redis-server")
	redis.StartTime = start.Add(-72 * time.Hour)
	vite := listener(5173, 2, "node")
	vite.StartTime = start.Add(-time.Minute)
	db := scan.Listener{Port: 5432, Address: netip.MustParseAddr("0.0.0.0"), Container: "Webapp-db", ContainerID: "abc"}
	db.StartTime = start.Add(-time.Hour)
	hidden := scan.Listener{Port: 631, Address: netip.MustParseAddr("127.0.0.1"), User: "root"}
	tm.scan(hidden, vite, db, redis)

	steps := []struct {
		key   string
		want  []uint16
		title string
	}{
		{"", []uint16{631, 5173, 5432, 6379}, ""},
		{"S", []uint16{6379, 5432, 5173, 631}, "sorted by port, reversed"},
		{"S", []uint16{631, 5173, 5432, 6379}, ""},
		// Case-insensitive; the hidden owner has no name and goes last.
		{"s", []uint16{5173, 6379, 5432, 631}, "sorted by process"},
		{"S", []uint16{5432, 6379, 5173, 631}, "sorted by process, reversed"},
		{"S", []uint16{5173, 6379, 5432, 631}, "sorted by process"},
		// Newest first; the unknown start time goes last.
		{"s", []uint16{5173, 5432, 6379, 631}, "sorted by uptime"},
		{"S", []uint16{6379, 5432, 5173, 631}, "sorted by uptime, reversed"},
		{"S", []uint16{5173, 5432, 6379, 631}, "sorted by uptime"},
		{"s", []uint16{631, 5173, 5432, 6379}, ""},
	}
	for _, st := range steps {
		if st.key != "" {
			tm.press(st.key)
		}
		if got := tm.order(); !slices.Equal(got, st.want) {
			t.Errorf("after %q: order %v, want %v", st.key, got, st.want)
		}
		title := strings.SplitN(tm.View(), "\n", 2)[0]
		if st.title == "" && strings.Contains(title, "sorted") {
			t.Errorf("after %q: default order shouldn't be in the title: %q", st.key, title)
		}
		if st.title != "" && !strings.Contains(title, st.title) {
			t.Errorf("after %q: title %q, want it to contain %q", st.key, title, st.title)
		}
	}
}

func TestSortKeepsSelection(t *testing.T) {
	tm := newTestModel(t)
	a := listener(3000, 1, "zsh")
	b := listener(4000, 2, "api")
	c := listener(5000, 3, "node")
	tm.scan(a, b, c)
	tm.press("down") // 4000 api
	tm.press("s")    // by process: api, node, zsh
	if got := tm.selected(); got != 4000 {
		t.Errorf("selected %d after sorting, want 4000", got)
	}
	if got := tm.order(); !slices.Equal(got, []uint16{4000, 5000, 3000}) {
		t.Errorf("order %v, want [4000 5000 3000]", got)
	}
}

func TestDetailPanel(t *testing.T) {
	tm := newTestModel(t)
	next := listener(3000, 48213, "node")
	next.Command = []string{"node", "/home/dev/code/webapp/node_modules/.bin/next", "dev"}
	next.Cwd = "/home/dev/code/webapp"
	next.StartTime = start.Add(-(2*time.Hour + 14*time.Minute))
	next.Connections = 3
	v6 := next
	v6.Address = netip.MustParseAddr("::1")
	tm.scan(next, v6, listener(8080, 2, "nginx"))

	tm.press("enter")
	if tm.mode != modeDetail {
		t.Fatal("enter should open the detail panel")
	}
	view := tm.View()
	for _, want := range []string{
		"╭─ node · PID 48213", "3000 on 127.0.0.1, ::1 · local only",
		"node /home/dev/code/webapp/node_modules/.bin/next dev", "~/code/webapp",
		"2h14m ago", "user      dev", "3 connected", "esc back",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("panel missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "nginx") {
		t.Errorf("the panel replaces the table:\n%s", view)
	}

	tm.press("down")
	if !strings.Contains(tm.View(), "╭─ nginx · PID 2") {
		t.Errorf("moving the selection should switch the panel:\n%s", tm.View())
	}
	tm.press("esc")
	if tm.mode != modeList || !strings.Contains(tm.View(), "PORT") {
		t.Errorf("esc should return to the list:\n%s", tm.View())
	}
}

func TestOpenAndCopy(t *testing.T) {
	tm := newTestModel(t)
	tm.scan(listener(3000, 1, "node"))
	tm.run(tm.press("o"))
	tm.run(tm.press("c"))
	if !slices.Equal(tm.opened, []string{"http://localhost:3000"}) {
		t.Errorf("opened %q", tm.opened)
	}
	if !slices.Equal(tm.copied, []string{"http://localhost:3000"}) {
		t.Errorf("copied %q", tm.copied)
	}
	if !strings.Contains(tm.View(), "✓ Copied http://localhost:3000") {
		t.Errorf("expected a confirmation:\n%s", tm.View())
	}
}

func TestBrowseURL(t *testing.T) {
	row := func(addrs ...string) ui.Row {
		var r ui.Row
		for _, a := range addrs {
			r = append(r, scan.Listener{Port: 8080, Address: netip.MustParseAddr(a)})
		}
		return r
	}
	tests := []struct {
		row  ui.Row
		want string
	}{
		{row("127.0.0.1"), "http://localhost:8080"},
		{row("::1"), "http://localhost:8080"},
		{row("0.0.0.0"), "http://localhost:8080"},
		{row("192.168.1.20"), "http://192.168.1.20:8080"},
		{row("fd00::5"), "http://[fd00::5]:8080"},
		{row("192.168.1.20", "127.0.0.1"), "http://localhost:8080"},
	}
	for _, tt := range tests {
		if got := browseURL(tt.row); got != tt.want {
			t.Errorf("browseURL(%v) = %q, want %q", tt.row[0].Address, got, tt.want)
		}
	}
}

func TestHelp(t *testing.T) {
	tm := newTestModel(t)
	tm.scan(listener(3000, 1, "node"))
	tm.press("?")
	if !strings.Contains(tm.View(), "open the port in your browser") {
		t.Fatalf("expected the key reference:\n%s", tm.View())
	}
	tm.press("j")
	if tm.mode != modeList {
		t.Error("any key should close the help")
	}
}

func TestMouse(t *testing.T) {
	tm := newTestModel(t)
	tm.scan(listener(3000, 1, "node"), listener(5432, 2, "postgres"), listener(8080, 3, "nginx"))
	tm.View()
	click := func(y int) {
		tm.Update(tea.MouseMsg{X: 10, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	}

	click(firstRowLine + 2)
	if tm.selected() != 8080 || tm.mode != modeList {
		t.Fatalf("clicking the third row selected %d (mode %d)", tm.selected(), tm.mode)
	}
	click(firstRowLine + 2)
	if tm.mode != modeDetail {
		t.Error("clicking the selected row should open its details")
	}
	tm.press("esc")
	click(firstRowLine + 10) // below the rows
	if tm.selected() != 8080 {
		t.Errorf("clicking empty space changed the selection to %d", tm.selected())
	}
	tm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if tm.selected() != 5432 {
		t.Errorf("wheel up selected %d, want 5432", tm.selected())
	}
}

func TestTitleSummarizesExposure(t *testing.T) {
	tm := newTestModel(t)
	public := listener(8080, 3, "nginx")
	public.Address = netip.MustParseAddr("0.0.0.0")
	db := scan.Listener{Port: 5432, Address: netip.MustParseAddr("0.0.0.0"), Container: "db", ContainerID: "abc"}
	tm.scan(listener(3000, 1, "node"), public, db)
	title := strings.SplitN(tm.View(), "\n", 2)[0]
	if !strings.Contains(title, "3 listening · 2 exposed · 1 docker") {
		t.Errorf("title = %q", title)
	}
}

func TestSpinnerWhileStopping(t *testing.T) {
	tm := newTestModel(t)
	tm.scan(listener(3000, 4242, "node"))
	tm.press("x")
	tm.press("y") // the stop command isn't run, so it stays in progress
	footer := lastLine(tm.View())
	if !strings.Contains(footer, spinFrames[0]+" Stopping node (PID 4242)") {
		t.Fatalf("footer = %q", footer)
	}
	tm.Update(spinMsg{})
	if !strings.Contains(lastLine(tm.View()), spinFrames[1]) {
		t.Errorf("spinner didn't advance: %q", lastLine(tm.View()))
	}
}

func TestFormatStart(t *testing.T) {
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.Local)
	tests := []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 10, 8, 9, 14, 0, 0, time.Local), "today 09:14"},
		{time.Date(2026, 10, 7, 18, 2, 0, 0, time.Local), "yesterday 18:02"},
		{time.Date(2026, 10, 3, 11, 20, 0, 0, time.Local), "Oct 3 11:20"},
		{time.Date(2025, 12, 24, 8, 0, 0, 0, time.Local), "Dec 24 2025"},
	}
	for _, tt := range tests {
		if got := formatStart(tt.t, now); got != tt.want {
			t.Errorf("formatStart(%v) = %q, want %q", tt.t, got, tt.want)
		}
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		s     string
		width int
		rows  int
		want  []string
	}{
		{"short", 10, 2, []string{"short"}},
		{"python3 -m http.server 8765 --bind 0.0.0.0", 29, 4, []string{"python3 -m http.server 8765", "--bind 0.0.0.0"}},
		{"/tmp/claude-1000/project/scratchpad", 20, 4, []string{"/tmp/claude-1000/", "project/scratchpad"}},
		{"abcdefghij", 4, 4, []string{"abcd", "efgh", "ij"}},
		{"one two three four", 7, 2, []string{"one two", "three…"}},
	}
	for _, tt := range tests {
		if got := wrap(tt.s, tt.width, tt.rows); !slices.Equal(got, tt.want) {
			t.Errorf("wrap(%q, %d, %d) = %q, want %q", tt.s, tt.width, tt.rows, got, tt.want)
		}
	}
}

func lastLine(s string) string {
	return s[strings.LastIndexByte(s, '\n')+1:]
}
