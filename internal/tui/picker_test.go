package tui

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/skinleak/peek/internal/scan"
	"github.com/skinleak/peek/internal/ui"
)

func newTestPicker(t *testing.T, o PickOptions) *picker {
	t.Helper()
	next := listener(3000, 101, "node")
	next.Command = []string{"node", "/home/dev/app/node_modules/.bin/next", "dev"}
	next.Cwd = "/home/dev/app"
	next.StartTime = start.Add(-time.Hour)
	next6 := next
	next6.Address = netip.MustParseAddr("::1")
	hmr := listener(3001, 101, "node") // same process, second port
	hmr.Command = next.Command
	db := scan.Listener{Port: 5432, Address: netip.MustParseAddr("0.0.0.0"), Container: "webapp-db", ContainerID: "abc123"}
	api := listener(8000, 202, "python3")
	api.Command = []string{"python3", "manage.py", "runserver"}

	o.Home = "/home/dev"
	p := newPicker([]scan.Listener{next, next6, hmr, db, api}, o, ui.NewRenderer(&bytes.Buffer{}, false))
	p.now = func() time.Time { return start }
	p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return p
}

func (p *picker) press(keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "ctrl+a":
			msg = tea.KeyMsg{Type: tea.KeyCtrlA}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		p.Update(msg)
	}
}

func chosenPIDs(p *picker) []int {
	var pids []int
	for _, t := range p.chosen {
		pids = append(pids, t.PID)
	}
	return pids
}

func TestPickerListsOneRowPerProcess(t *testing.T) {
	p := newTestPicker(t, PickOptions{})
	view := p.View()
	for _, want := range []string{
		"Which processes should peek stop?",
		"› ○ 3000, 3001  node (next)", "1h  ~/app",
		"○ 5432        webapp-db (docker)", "○ 8000        python3 (manage.py)",
		"enter stop it",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if n := strings.Count(view, "○"); n != 3 {
		t.Errorf("got %d rows, want 3 (one per process or container):\n%s", n, view)
	}
}

func TestPickerEnterStopsHighlighted(t *testing.T) {
	p := newTestPicker(t, PickOptions{})
	p.press("down", "down", "enter")
	if got := chosenPIDs(p); len(got) != 1 || got[0] != 202 {
		t.Errorf("chose %v, want the python3 process", got)
	}
	if p.View() != "" {
		t.Error("the picker should clear itself after choosing")
	}
}

func TestPickerMarksSeveral(t *testing.T) {
	p := newTestPicker(t, PickOptions{})
	p.press("space") // marks node and moves on
	p.press("down", "space")
	if !strings.Contains(p.View(), "enter stop 2") {
		t.Errorf("footer should count the selection:\n%s", p.View())
	}
	p.press("enter")
	if len(p.chosen) != 2 || p.chosen[0].PID != 101 || p.chosen[1].Name != "python3" {
		t.Errorf("chose %+v, want node and python3", p.chosen)
	}
}

func TestPickerFilter(t *testing.T) {
	p := newTestPicker(t, PickOptions{})
	p.press("d", "o", "c", "k")
	view := p.View()
	if !strings.Contains(view, "> dock") || !strings.Contains(view, "webapp-db") || strings.Contains(view, "python3") {
		t.Errorf("filter should leave only the container:\n%s", view)
	}
	p.press("enter")
	if len(p.chosen) != 1 || p.chosen[0].ContainerID != "abc123" {
		t.Errorf("chose %+v, want the container", p.chosen)
	}

	p = newTestPicker(t, PickOptions{})
	p.press("x", "y", "z")
	if !strings.Contains(p.View(), "Nothing matches.") {
		t.Errorf("expected a no-match message:\n%s", p.View())
	}
	p.press("enter")
	if p.chosen != nil || p.done {
		t.Error("enter with nothing matching must not choose anything")
	}
	p.press("esc") // clears the filter
	if p.done || !strings.Contains(p.View(), "python3") {
		t.Errorf("esc should clear the filter first:\n%s", p.View())
	}
	p.press("esc") // then cancels
	if !p.done || p.chosen != nil {
		t.Error("esc with an empty filter should cancel")
	}
}

func TestPickerSelectAll(t *testing.T) {
	p := newTestPicker(t, PickOptions{})
	p.press("ctrl+a", "enter")
	if len(p.chosen) != 3 {
		t.Errorf("ctrl+a then enter chose %d, want all 3", len(p.chosen))
	}
}

func TestPickerNotes(t *testing.T) {
	p := newTestPicker(t, PickOptions{Force: true, Hidden: 2})
	view := p.View()
	if !strings.Contains(view, "force kill (SIGKILL)") {
		t.Errorf("title should say force kill:\n%s", view)
	}
	if !strings.Contains(view, "2 ports owned by other users not shown") {
		t.Errorf("expected a note about hidden ports:\n%s", view)
	}
}
