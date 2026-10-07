package ui

import (
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"peek/internal/scan"
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
	alignRight bool
}

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

// Table writes ls as an aligned, borderless table.
func Table(w io.Writer, ls []scan.Listener, o Options) error {
	st := newStyles(newRenderer(w, o.Color))

	rows := make([][numCols]cell, 0, len(ls)+1)
	var head [numCols]cell
	for i, h := range headers {
		head[i] = cell{text: h, style: st.header, alignRight: i == colPort || i == colPID}
	}
	rows = append(rows, head)
	for _, l := range ls {
		rows = append(rows, listenerRow(l, st, o))
	}

	widths := columnWidths(rows)
	fitCwd(rows, &widths, o.Width)

	var b strings.Builder
	for _, row := range rows {
		writeRow(&b, row, widths)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func listenerRow(l scan.Listener, st styles, o Options) [numCols]cell {
	var row [numCols]cell
	row[colPort] = cell{text: strconv.Itoa(int(l.Port)), style: st.port, alignRight: true}

	row[colProcess] = cell{text: l.ProcessName, style: st.plain}
	row[colPID] = cell{text: strconv.Itoa(l.PID), style: st.plain, alignRight: true}
	if l.PID == 0 {
		row[colProcess] = cell{text: unknown, style: st.dim}
		row[colPID] = cell{text: unknown, style: st.dim, alignRight: true}
	}

	addrStyle := st.local
	if l.Exposed() {
		addrStyle = st.exposed
	}
	row[colAddress] = cell{text: l.Address.String(), style: addrStyle}

	row[colCwd] = cell{text: unknown, style: st.dim}
	if l.Cwd != "" {
		row[colCwd] = cell{text: tildify(l.Cwd, o.Home), style: st.plain}
	}

	row[colUptime] = cell{text: unknown, style: st.dim}
	if !l.StartTime.IsZero() {
		row[colUptime] = cell{text: formatUptime(o.Now.Sub(l.StartTime)), style: st.dim}
	}
	return row
}

func columnWidths(rows [][numCols]cell) [numCols]int {
	var widths [numCols]int
	for _, row := range rows {
		for i, c := range row {
			widths[i] = max(widths[i], lipgloss.Width(c.text))
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

func writeRow(b *strings.Builder, row [numCols]cell, widths [numCols]int) {
	for i, c := range row {
		pad := strings.Repeat(" ", widths[i]-lipgloss.Width(c.text))
		styled := c.style.Render(c.text)
		switch {
		case c.alignRight:
			b.WriteString(pad + styled)
		case i == numCols-1:
			b.WriteString(styled) // no trailing whitespace
		default:
			b.WriteString(styled + pad)
		}
		if i < numCols-1 {
			b.WriteString(columnGap)
		}
	}
	b.WriteByte('\n')
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
