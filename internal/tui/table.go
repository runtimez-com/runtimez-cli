package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// renderTable draws the visible slice of a table with the cursor row highlighted.
//
// Column widths come from the content, then get trimmed right-to-left when the terminal is
// too narrow: the leftmost columns are the identifying ones (namespace, name), so they are
// the last to lose space.
func renderTable(t *render.Table, visible []int, cursor, offset, width, height int) string {
	if t == nil || len(t.Headers) == 0 {
		return ""
	}
	if height < 1 {
		height = 1
	}

	widths := columnWidths(t, visible, width)

	var b strings.Builder
	b.WriteString(colStyle.Render(padRow(t.Headers, widths)))
	b.WriteByte('\n')

	end := offset + height
	if end > len(visible) {
		end = len(visible)
	}
	for i := offset; i < end; i++ {
		row := t.Rows[visible[i]]
		line := padRow(row, widths)
		if i == cursor {
			// Pad to full width so the highlight reads as a bar, not a ragged patch.
			line = lipgloss.NewStyle().Width(width).Render(line)
			b.WriteString(selStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func columnWidths(t *render.Table, visible []int, max int) []int {
	widths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, idx := range visible {
		row := t.Rows[idx]
		for i := 0; i < len(row) && i < len(widths); i++ {
			if w := lipgloss.Width(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	const gap = 2
	total := 0
	for _, w := range widths {
		total += w + gap
	}
	// Shrink from the right: the last columns are the least identifying, so losing width
	// there costs the least. Never below 6 characters, which still shows a prefix.
	for i := len(widths) - 1; i > 0 && total > max; i-- {
		slack := widths[i] - 6
		if slack <= 0 {
			continue
		}
		take := slack
		if total-take < max {
			take = total - max
		}
		widths[i] -= take
		total -= take
	}
	return widths
}

func padRow(cells []string, widths []int) string {
	var b strings.Builder
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		cell = clip(cell, w)
		b.WriteString(cell)
		if i < len(widths)-1 {
			b.WriteString(strings.Repeat(" ", w-lipgloss.Width(cell)+2))
		}
	}
	return b.String()
}

// clip truncates with an ellipsis so a cut value is never mistaken for a short one.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	if w == 1 {
		return "…"
	}
	if len(runes) > w-1 {
		runes = runes[:w-1]
	}
	return string(runes) + "…"
}
