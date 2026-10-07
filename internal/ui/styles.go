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
// well on both light and dark themes.
var (
	accent  = lipgloss.AdaptiveColor{Light: "#5B3CC4", Dark: "#A78BFA"}
	exposed = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	local   = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	muted   = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
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
		header:  r.NewStyle().Bold(true).Foreground(muted),
		port:    r.NewStyle().Bold(true).Foreground(accent),
		exposed: r.NewStyle().Foreground(exposed),
		local:   r.NewStyle().Foreground(local),
		dim:     r.NewStyle().Foreground(muted),
		plain:   r.NewStyle(),
	}
}

// newRenderer returns a Lip Gloss renderer for w, with colors disabled
// entirely when color is false.
func newRenderer(w io.Writer, color bool) *lipgloss.Renderer {
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
