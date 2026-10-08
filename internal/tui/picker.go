package tui

import (
	"errors"
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

// ErrCancelled is returned by Pick when the user leaves without choosing.
var ErrCancelled = errors.New("cancelled")

// maxPickerRows is how many choices the picker shows at once.
const maxPickerRows = 10

// PickOptions configure Pick.
type PickOptions struct {
	Force bool   // the chosen processes will get SIGKILL
	Home  string // home directory to abbreviate as "~"
	// Hidden is how many ports were left out because their owner couldn't
	// be seen.
	Hidden int
}

// choice is one process or container the user can pick, with what's
// shown about it.
type choice struct {
	target  kill.Target
	name    string // process or container name
	suffix  string // " (vite)", " (docker)"
	cwd     string
	started time.Time
	search  string // lowercased text the filter matches against
}

// Pick shows an inline list of the processes and containers owning ls and
// returns the ones the user chose to stop. It returns ErrCancelled if the
// user cancels.
func Pick(ls []scan.Listener, o PickOptions) ([]kill.Target, error) {
	p := newPicker(ls, o, ui.NewRenderer(os.Stdout, ui.ColorEnabled(os.Stdout)))
	if _, err := tea.NewProgram(p).Run(); err != nil {
		return nil, err
	}
	if p.chosen == nil {
		return nil, ErrCancelled
	}
	return p.chosen, nil
}

type picker struct {
	opts   PickOptions
	st     styles
	now    func() time.Time
	width  int
	all    []choice
	filter string

	cursor int          // index into the filtered choices
	offset int          // first visible filtered choice
	marked map[int]bool // indexes into all

	chosen []kill.Target // set when the user confirms
	done   bool
}

func newPicker(ls []scan.Listener, o PickOptions, r *lipgloss.Renderer) *picker {
	return &picker{
		opts:   o,
		st:     newStyles(r),
		now:    time.Now,
		all:    choices(ls, o.Home),
		marked: make(map[int]bool),
	}
}

// choices describes each process or container that owns one of ls.
func choices(ls []scan.Listener, home string) []choice {
	targets, _ := kill.Targets(ls)
	out := make([]choice, 0, len(targets))
	for _, t := range targets {
		i := slices.IndexFunc(ls, func(l scan.Listener) bool {
			if t.ContainerID != "" {
				return l.ContainerID == t.ContainerID
			}
			return l.PID == t.PID && l.ContainerID == ""
		})
		l := ls[i]
		c := choice{target: t, name: t.Name, started: l.StartTime}
		switch {
		case t.ContainerID != "":
			c.suffix = " (docker)"
		case ui.Describe(l.ProcessName, l.Command) != "":
			c.suffix = " (" + ui.Describe(l.ProcessName, l.Command) + ")"
		}
		if l.Cwd != "" && t.ContainerID == "" {
			c.cwd = ui.Tildify(l.Cwd, home)
		}
		c.search = strings.ToLower(strings.Join([]string{
			portList(t.Ports), c.name, c.suffix, strconv.Itoa(t.PID), l.Cwd, strings.Join(l.Command, " "),
		}, " "))
		out = append(out, c)
	}
	return out
}

func portList(ports []uint16) string {
	s := make([]string, len(ports))
	for i, p := range ports {
		s[i] = strconv.Itoa(int(p))
	}
	return strings.Join(s, ", ")
}

func (p *picker) Init() tea.Cmd { return nil }

// visible returns the indexes (into all) of the choices matching the filter.
func (p *picker) visible() []int {
	var out []int
	f := strings.ToLower(p.filter)
	for i, c := range p.all {
		if strings.Contains(c.search, f) {
			out = append(out, i)
		}
	}
	return out
}

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
	case tea.KeyMsg:
		return p, p.handleKey(msg)
	}
	return p, nil
}

func (p *picker) handleKey(k tea.KeyMsg) tea.Cmd {
	vis := p.visible()
	switch k.Type {
	case tea.KeyCtrlC:
		return p.quit()
	case tea.KeyEsc:
		if p.filter == "" {
			return p.quit()
		}
		p.filter, p.cursor = "", 0
	case tea.KeyUp, tea.KeyShiftTab:
		p.cursor = max(0, p.cursor-1)
	case tea.KeyDown, tea.KeyTab:
		p.cursor = min(len(vis)-1, p.cursor+1)
	case tea.KeySpace:
		if p.cursor < len(vis) {
			i := vis[p.cursor]
			p.marked[i] = !p.marked[i]
			p.cursor = min(len(vis)-1, p.cursor+1)
		}
	case tea.KeyCtrlA:
		all := !p.allMarked(vis)
		for _, i := range vis {
			p.marked[i] = all
		}
	case tea.KeyEnter:
		return p.confirm(vis)
	case tea.KeyBackspace:
		if r := []rune(p.filter); len(r) > 0 {
			p.filter, p.cursor = string(r[:len(r)-1]), 0
		}
	case tea.KeyRunes:
		p.filter, p.cursor = p.filter+string(k.Runes), 0
	}
	return nil
}

func (p *picker) allMarked(vis []int) bool {
	for _, i := range vis {
		if !p.marked[i] {
			return false
		}
	}
	return len(vis) > 0
}

// confirm chooses the marked processes, or the highlighted one if none is
// marked.
func (p *picker) confirm(vis []int) tea.Cmd {
	var chosen []kill.Target
	for i, c := range p.all {
		if p.marked[i] {
			chosen = append(chosen, c.target)
		}
	}
	if len(chosen) == 0 {
		if p.cursor >= len(vis) {
			return nil // nothing matches the filter
		}
		chosen = []kill.Target{p.all[vis[p.cursor]].target}
	}
	p.chosen = chosen
	return p.quit()
}

func (p *picker) quit() tea.Cmd {
	p.done = true
	return tea.Quit
}

func (p *picker) View() string {
	if p.done {
		return "" // clear the picker; the caller reports what happens next
	}
	vis := p.visible()
	p.cursor = max(0, min(p.cursor, len(vis)-1))
	p.offset = scrollOffset(p.offset, p.cursor, maxPickerRows, len(vis))

	verb := "stop"
	if p.opts.Force {
		verb = "force kill (SIGKILL)"
	}
	var b strings.Builder
	b.WriteString(p.st.title.Render("Which processes should peek "+verb+"?") + "\n")
	b.WriteString(p.st.text.Render("> "+p.filter) + p.st.cursor.Render(" ") + "\n")

	if len(vis) == 0 {
		b.WriteString(p.st.dim.Render("  Nothing matches.") + "\n")
	}
	lines := p.rows(vis)
	for i := p.offset; i < min(len(vis), p.offset+maxPickerRows); i++ {
		b.WriteString(truncateTo(lines[i], p.width) + "\n")
	}
	if more := len(vis) - min(len(vis), p.offset+maxPickerRows); more > 0 || p.offset > 0 {
		b.WriteString(p.st.dim.Render(fmt.Sprintf("  %d of %d shown, ↑↓ to scroll", min(maxPickerRows, len(vis)), len(vis))) + "\n")
	}
	if p.opts.Hidden > 0 {
		b.WriteString(p.st.dim.Render(fmt.Sprintf("  %s owned by other users not shown; run with sudo to include them",
			pluralize(p.opts.Hidden, "port", "ports"))) + "\n")
	}

	marked := 0
	for _, v := range p.marked {
		if v {
			marked++
		}
	}
	enter := "stop it"
	if marked > 0 {
		enter = fmt.Sprintf("stop %d", marked)
	}
	keys := []keyHint{{"↑↓", "move", 5}, {"space", "select", 7}, {"ctrl+a", "all", 2}, {"enter", enter, 9}, {"esc", "cancel", 8}}
	b.WriteString(" " + keyHints(p.st, keys, p.width-1))
	return b.String()
}

// rows renders the visible choices as aligned columns: mark, ports,
// process, PID, uptime and directory.
func (p *picker) rows(vis []int) []string {
	var wPorts, wName, wPID, wUp int
	for _, i := range vis {
		c := p.all[i]
		wPorts = max(wPorts, len(portList(c.target.Ports)))
		wName = max(wName, lipgloss.Width(c.name+c.suffix))
		wPID = max(wPID, len(pidText(c.target)))
		wUp = max(wUp, len(p.uptime(c)))
	}
	out := make([]string, len(vis))
	for row, i := range vis {
		c := p.all[i]
		cursor, mark := "  ", p.st.dim.Render("○")
		if row == p.cursor {
			cursor = p.st.title.Render("›") + " "
		}
		if p.marked[i] {
			mark = p.st.added.Render("●")
		}
		ports := portList(c.target.Ports)
		name := p.st.text.Render(c.name) + p.st.dim.Render(c.suffix)
		line := cursor + mark + " " +
			p.st.title.Render(ports) + strings.Repeat(" ", wPorts-len(ports)+2) +
			name + strings.Repeat(" ", wName-lipgloss.Width(c.name+c.suffix)+2) +
			strings.Repeat(" ", wPID-len(pidText(c.target))) + p.st.text.Render(pidText(c.target)) + "  " +
			p.st.dim.Render(p.uptime(c)) + strings.Repeat(" ", wUp-len(p.uptime(c))+2) +
			p.st.text.Render(c.cwd)
		out[row] = strings.TrimRight(line, " ")
	}
	return out
}

func (p *picker) uptime(c choice) string {
	if c.started.IsZero() {
		return ""
	}
	return ui.FormatUptime(p.now().Sub(c.started))
}

func pidText(t kill.Target) string {
	if t.ContainerID != "" {
		return "-"
	}
	return strconv.Itoa(t.PID)
}
