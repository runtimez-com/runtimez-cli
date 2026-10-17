package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The chrome is drawn by hand rather than with lipgloss borders because the title sits IN
// the top rule — "╭─ pods [186] ─────╮" — which a plain border cannot express. Doing it
// here keeps the arithmetic in one place instead of at every call site.

const (
	minPanelWidth = 30 // below this a box costs more columns than it earns
	borderCost    = 4  // two border runes plus a space of padding either side
)

// panel wraps body in a titled box exactly width columns wide. Lines longer than the box
// are clipped, never wrapped: a wrapped table row reads as two rows.
func panel(title, body string, width int) string {
	if width < minPanelWidth {
		// Too narrow to frame — the content is what matters, so give it every column.
		return body
	}
	inner := width - borderCost

	var b strings.Builder
	b.WriteString(topRule(title, width))
	b.WriteByte('\n')
	for _, line := range strings.Split(body, "\n") {
		line = clip(line, inner)
		pad := inner - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(borderStyle.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + borderStyle.Render("│"))
		b.WriteByte('\n')
	}
	b.WriteString(borderStyle.Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return b.String()
}

// topRule is "╭─ title ────╮", with the title dropped when it would leave no rule at all.
func topRule(title string, width int) string {
	if title == "" {
		return borderStyle.Render("╭" + strings.Repeat("─", width-2) + "╮")
	}
	title = clip(title, width-8)
	// "╭─ " + title + " " + fill + "╮" must come to exactly width.
	fill := width - 5 - lipgloss.Width(title)
	if fill < 1 {
		return borderStyle.Render("╭" + strings.Repeat("─", width-2) + "╮")
	}
	return borderStyle.Render("╭─ ") + headerStyle.Render(title) + " " +
		borderStyle.Render(strings.Repeat("─", fill)+"╮")
}

// panelHeight is how many rows a panel of n body lines occupies.
func panelHeight(bodyLines, width int) int {
	if width < minPanelWidth {
		return bodyLines
	}
	return bodyLines + 2
}

// columns lays blocks side by side with a gap, for the key-hint columns in the header.
func columns(gap int, blocks ...string) string {
	rendered := make([][]string, 0, len(blocks))
	widths := make([]int, 0, len(blocks))
	height := 0
	for _, blk := range blocks {
		if blk == "" {
			continue
		}
		lines := strings.Split(blk, "\n")
		w := 0
		for _, l := range lines {
			if lw := lipgloss.Width(l); lw > w {
				w = lw
			}
		}
		rendered = append(rendered, lines)
		widths = append(widths, w)
		if len(lines) > height {
			height = len(lines)
		}
	}

	var b strings.Builder
	for row := 0; row < height; row++ {
		for i, lines := range rendered {
			cell := ""
			if row < len(lines) {
				cell = lines[row]
			}
			b.WriteString(cell)
			if i < len(rendered)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+gap))
			}
		}
		if row < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
