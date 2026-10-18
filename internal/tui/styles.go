package tui

import "github.com/charmbracelet/lipgloss"

// Colors are ANSI-256 indices rather than hex so the UI sits inside the user's own terminal
// theme instead of imposing one that fights it. They are adaptive because a palette tuned
// for a dark terminal goes unreadable on a light one, and plenty of people run light.
//
// NO_COLOR and a non-TTY are handled below lipgloss by termenv's profile detection, so
// nothing here needs to branch on them.
var (
	accent    = lipgloss.AdaptiveColor{Light: "63", Dark: "141"}  // violet — the chrome
	okColor   = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}   // green — healthy
	infoColor = lipgloss.AdaptiveColor{Light: "31", Dark: "116"}  // cyan — values
	linkColor = lipgloss.AdaptiveColor{Light: "25", Dark: "111"}  // blue — URLs
	warnColor = lipgloss.AdaptiveColor{Light: "130", Dark: "214"} // amber
	errColor  = lipgloss.AdaptiveColor{Light: "160", Dark: "203"} // red
	dim       = lipgloss.AdaptiveColor{Light: "245", Dark: "244"}
	faint     = lipgloss.AdaptiveColor{Light: "250", Dark: "240"} // rows that no longer matter
	onAccent  = lipgloss.AdaptiveColor{Light: "231", Dark: "236"} // text ON the accent bar
)

var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	metaStyle    = lipgloss.NewStyle().Foreground(dim)
	labelStyle   = lipgloss.NewStyle().Foreground(dim)
	valueStyle   = lipgloss.NewStyle().Foreground(infoColor)
	urlStyle     = lipgloss.NewStyle().Foreground(linkColor)
	colStyle     = lipgloss.NewStyle().Bold(true).Foreground(dim)
	sortColStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	selStyle     = lipgloss.NewStyle().Bold(true).Foreground(onAccent).Background(accent)
	footerStyle  = lipgloss.NewStyle().Foreground(dim)
	errStyle     = lipgloss.NewStyle().Foreground(errColor)
	warnStyle    = lipgloss.NewStyle().Foreground(warnColor)
	okStyle      = lipgloss.NewStyle().Foreground(okColor)
	mutedStyle   = lipgloss.NewStyle().Foreground(faint)
	keyStyle     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	borderStyle  = lipgloss.NewStyle().Foreground(accent)
	chipStyle    = lipgloss.NewStyle().Bold(true).Foreground(onAccent).Background(accent)
	logoStyle    = lipgloss.NewStyle().Foreground(faint)
)

// toneStyle maps a cell's semantic reading to how it is drawn. Kept in one place so a
// status colour means the same thing on every screen.
func toneStyle(t tone) lipgloss.Style {
	switch t {
	case toneOK:
		return okStyle
	case toneWarn:
		return warnStyle
	case toneBad:
		return errStyle
	case toneMuted:
		return mutedStyle
	}
	return lipgloss.NewStyle()
}
