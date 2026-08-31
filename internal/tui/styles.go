package tui

import "github.com/charmbracelet/lipgloss"

// Colors are set with ANSI 256 indices rather than hex so the UI inherits the user's own
// terminal theme instead of imposing a palette that fights it.
var (
	accent    = lipgloss.Color("42") // green — the runtimez accent
	dim       = lipgloss.Color("244")
	warnColor = lipgloss.Color("214")
	errColor  = lipgloss.Color("203")

	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	metaStyle   = lipgloss.NewStyle().Foreground(dim)
	colStyle    = lipgloss.NewStyle().Bold(true).Foreground(dim)
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("238"))
	footerStyle = lipgloss.NewStyle().Foreground(dim)
	errStyle    = lipgloss.NewStyle().Foreground(errColor)
	warnStyle   = lipgloss.NewStyle().Foreground(warnColor)
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
)
