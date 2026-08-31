package tui

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// Capable reports whether this terminal can host the full-screen UI.
//
// Two checks, both robust: output must be a terminal (not a pipe, not a CI log), and TERM
// must not be "dumb". Deliberately NOT probed: legacy Windows conhost. Bubble Tea enables
// virtual-terminal processing through ConPTY on Windows 10 1809 and later, which covers
// Windows Terminal and PowerShell 7, and every heuristic for detecting the older console
// also misfires on working modern ones — refusing to start on a terminal that would have
// rendered fine is worse than the rare garbled frame.
func Capable() bool {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	return !strings.EqualFold(os.Getenv("TERM"), "dumb")
}
