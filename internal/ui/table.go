package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/skinleak/peek/internal/scan"
)

const (
	columnGap   = "  "
	unknown     = "-"
	minCwdWidth = 12
)

// Options control table rendering.
type Options struct {
	Color bool      // emit ANSI styling
	Width int       // maximum line width; 0 means unlimited
	Home  string    // home directory to abbreviate as "~"
	Now   time.Time // reference time for uptimes
}

// DefaultOptions returns options suited to writing to f.
func DefaultOptions(f *os.File) Options {
	home, _ := os.UserHomeDir()
	return Options{
		Color: ColorEnabled(f),
		Width: terminalWidth(f),
		Home:  home,
		Now:   time.Now(),
	}
}

func terminalWidth(f *os.File) int {
	w, _, err := term.GetSize(f.Fd())
	if err != nil {
		return 0
	}
	return w
}

type cell struct {
	text       string
	style      lipgloss.Style
	suffix     string // rendered dimmed after text
	alignRight bool
}

func (c cell) width() int { return lipgloss.Width(c.text + c.suffix) }

// Column indexes, in display order.
const (
	colPort = iota
	colProcess
	colPID
	colAddress
	colCwd
	colUptime
	numCols
)

var headers = [numCols]string{"PORT", "PROCESS", "PID", "ADDRESS", "CWD", "UPTIME"}

// Row is one line of the table: listeners with the same port and owner that
// differ only in bind address, such as 127.0.0.1 and ::1.
type Row []scan.Listener

// rowKey identifies a row's port and owner.
type rowKey struct {
	port      uint16
	pid       int
	container string
	user      string // tells apart owners we can't see
}

func keyOf(l scan.Listener) rowKey {
	k := rowKey{port: l.Port, pid: l.PID, container: l.ContainerID}
	if l.PID == 0 && l.ContainerID == "" {
		k.user = l.User
	}
	return k
}

// Key identifies the row's port and owner across scans.
func (r Row) Key() string {
	k := keyOf(r[0])
	return fmt.Sprintf("%d/%d/%s/%s", k.port, k.pid, k.container, k.user)
}

// GroupRows merges listeners that share a port and owner into one row,
// keeping the order in which each row first appears.
func GroupRows(ls []scan.Listener) []Row {
	index := make(map[rowKey]int)
	var rows []Row
	for _, l := range ls {
		k := keyOf(l)
		i, ok := index[k]
		if !ok {
			i = len(rows)
			index[k] = i
			rows = append(rows, nil)
		}
		rows[i] = append(rows[i], l)
	}
	return rows
}

// addresses returns the row's distinct bind addresses and whether any of
// them is reachable from other hosts.
func (r Row) addresses() (addrs []string, exposed bool) {
	for _, l := range r {
		if a := l.Address.String(); !slices.Contains(addrs, a) {
			addrs = append(addrs, a)
		}
		exposed = exposed || l.Exposed()
	}
	return addrs, exposed
}

// RowStyle controls how Lines highlights a row.
type RowStyle struct {
	Selected bool // drawn with a highlighted background
	Faded    bool // drawn dimmed, e.g. for a port that just closed
}

// Table writes ls as an aligned, borderless table.
func Table(w io.Writer, ls []scan.Listener, o Options) error {
	lines := Lines(NewRenderer(w, o.Color), GroupRows(ls), o, nil)
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}

// Lines renders rows as aligned table lines without newlines: the header,
// then one line per row. style, if not nil, picks each row's highlighting.
func Lines(r *lipgloss.Renderer, rows []Row, o Options, style func(i int) RowStyle) []string {
	st := newStyles(r)

	cells := make([][numCols]cell, 0, len(rows)+1)
	var head [numCols]cell
	for i, h := range headers {
		head[i] = cell{text: h, style: st.header, alignRight: i == colPort || i == colPID}
	}
	cells = append(cells, head)
	for _, row := range rows {
		cells = append(cells, rowCells(row, st, o))
	}

	widths := columnWidths(cells)
	fitCwd(cells, &widths, o.Width)

	lines := make([]string, len(cells))
	for i, c := range cells {
		var rs RowStyle
		if i > 0 && style != nil {
			rs = style(i - 1)
		}
		lines[i] = renderRow(c, widths, st, rs)
	}
	return lines
}

func rowCells(row Row, st styles, o Options) [numCols]cell {
	l := row[0]
	var c [numCols]cell
	c[colPort] = cell{text: strconv.Itoa(int(l.Port)), style: st.port, alignRight: true}

	c[colProcess] = cell{text: l.ProcessName, style: st.plain}
	if target := describe(l.ProcessName, l.Command); target != "" {
		c[colProcess].suffix = " (" + target + ")"
	}
	c[colPID] = cell{text: strconv.Itoa(l.PID), style: st.plain, alignRight: true}
	if l.PID == 0 {
		c[colProcess] = cell{text: unknown, style: st.dim}
		c[colPID] = cell{text: unknown, style: st.dim, alignRight: true}
	}
	if l.Container != "" {
		c[colProcess] = cell{text: l.Container, style: st.plain, suffix: " (docker)"}
	}

	addrs, exposed := row.addresses()
	addrStyle := st.local
	if exposed {
		addrStyle = st.exposed
	}
	c[colAddress] = cell{text: strings.Join(addrs, ","), style: addrStyle}

	c[colCwd] = cell{text: unknown, style: st.dim}
	if l.Cwd != "" {
		c[colCwd] = cell{text: Tildify(l.Cwd, o.Home), style: st.plain}
	}

	c[colUptime] = cell{text: unknown, style: st.dim}
	if !l.StartTime.IsZero() {
		c[colUptime] = cell{text: FormatUptime(o.Now.Sub(l.StartTime)), style: st.dim}
	}
	return c
}

func columnWidths(rows [][numCols]cell) [numCols]int {
	var widths [numCols]int
	for _, row := range rows {
		for i, c := range row {
			widths[i] = max(widths[i], c.width())
		}
	}
	return widths
}

// fitCwd shrinks the CWD column, the only one with unbounded length, so the
// table fits within maxWidth. Paths are truncated from the left.
func fitCwd(rows [][numCols]cell, widths *[numCols]int, maxWidth int) {
	if maxWidth <= 0 {
		return
	}
	total := len(columnGap) * (numCols - 1)
	for _, w := range widths {
		total += w
	}
	over := total - maxWidth
	if over <= 0 {
		return
	}
	widths[colCwd] = max(minCwdWidth, widths[colCwd]-over)
	for i := range rows {
		c := &rows[i][colCwd]
		c.text = truncateLeft(c.text, widths[colCwd])
	}
}

func renderRow(row [numCols]cell, widths [numCols]int, st styles, rs RowStyle) string {
	text, suffix, space := func(c cell) lipgloss.Style { return c.style }, st.dim, st.plain
	if rs.Faded {
		text = func(cell) lipgloss.Style { return st.dim }
	}
	if rs.Selected {
		base := text
		text = func(c cell) lipgloss.Style { return base(c).Background(selection) }
		suffix = suffix.Background(selection)
		space = space.Background(selection)
	}

	var b strings.Builder
	for i, c := range row {
		pad := space.Render(strings.Repeat(" ", widths[i]-c.width()))
		styled := text(c).Render(c.text)
		if c.suffix != "" {
			styled += suffix.Render(c.suffix)
		}
		switch {
		case c.alignRight:
			b.WriteString(pad + styled)
		case i == numCols-1 && !rs.Selected:
			b.WriteString(styled) // no trailing whitespace
		default:
			b.WriteString(styled + pad)
		}
		if i < numCols-1 {
			b.WriteString(space.Render(columnGap))
		}
	}
	return b.String()
}

// JSON writes ls as an indented JSON array (never null).
func JSON(w io.Writer, ls []scan.Listener) error {
	if ls == nil {
		ls = []scan.Listener{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(ls)
}
