// Package ui renders listeners as a terminal table or as JSON.
package ui

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"
)

// Adaptive colors pick a shade per terminal background so the output reads
// well on both light and dark themes. They're exported so the interactive
// view matches the table.
var (
	Accent  = lipgloss.AdaptiveColor{Light: "#5B3CC4", Dark: "#A78BFA"}
	Warning = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"} // exposed binds
	Success = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"} // local-only binds
	Danger  = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	Muted   = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	// selection is the background of the selected row in interactive mode.
	selection = lipgloss.AdaptiveColor{Light: "#E5E7EB", Dark: "#374151"}
)

type styles struct {
	header  lipgloss.Style
	port    lipgloss.Style
	exposed lipgloss.Style
	local   lipgloss.Style
	dim     lipgloss.Style
	plain   lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) styles {
	return styles{
		header:  r.NewStyle().Bold(true).Foreground(Muted),
		port:    r.NewStyle().Bold(true).Foreground(Accent),
		exposed: r.NewStyle().Foreground(Warning),
		local:   r.NewStyle().Foreground(Success),
		dim:     r.NewStyle().Foreground(Muted),
		plain:   r.NewStyle(),
	}
}

// NewRenderer returns a Lip Gloss renderer for w, with colors disabled
// entirely when color is false.
func NewRenderer(w io.Writer, color bool) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(w)
	if !color {
		r.SetColorProfile(termenv.Ascii)
		// Adaptive colors are irrelevant without color; setting this skips
		// the terminal background query (OSC 11) Lip Gloss would otherwise send.
		r.SetHasDarkBackground(true)
	}
	return r
}

// ColorEnabled reports whether output to f should be colored: f must be a
// terminal and NO_COLOR (https://no-color.org) must be unset or empty.
func ColorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}
