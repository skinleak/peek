package tui

import (
	"bytes"
	"net/netip"
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
}

func newTestModel(t *testing.T) *testModel {
	t.Helper()
	tm := &testModel{clock: start}
	m := newModel(Config{
		Stop: func(target kill.Target, force bool) error {
			tm.stopped = append(tm.stopped, target)
			return nil
		},
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
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		_, cmd = tm.Update(msg)
	}
	return cmd
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
	tm.Update(cmd())
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
