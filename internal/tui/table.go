package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// tableOpts is everything the table draw needs beyond the data itself.
type tableOpts struct {
	visible        []int
	cursor, offset int
	width, height  int
	sortCol        int // -1 when the server's own order is in effect
	sortDesc       bool
}

// renderTable draws the visible slice of a table with the cursor row highlighted.
//
// Column widths come from the content, then get trimmed right-to-left when the terminal is
// too narrow: the leftmost columns are the identifying ones (namespace, name), so they are
// the last to lose space.
func renderTable(t *render.Table, o tableOpts) string {
	if t == nil || len(t.Headers) == 0 {
		return ""
	}
	if o.height < 1 {
		o.height = 1
	}

	headers := headerCells(t.Headers, o.sortCol, o.sortDesc)
	widths := columnWidths(headers, t, o.visible, o.width)

	var b strings.Builder
	b.WriteString(paintHeader(headers, widths, o.sortCol))
	b.WriteByte('\n')

	end := o.offset + o.height
	if end > len(o.visible) {
		end = len(o.visible)
	}
	for i := o.offset; i < end; i++ {
		row := t.Rows[o.visible[i]]
		if i == o.cursor {
			// Pad to full width so the highlight reads as a bar, not a ragged patch. The
			// selected row drops per-cell colour: two colour schemes on one line fight.
			line := lipgloss.NewStyle().Width(o.width).Render(padRow(row, widths))
			b.WriteString(selStyle.Render(line))
		} else {
			b.WriteString(paintRow(t.Headers, row, widths))
		}
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// headerCells appends the sort marker to the sorted column, so the arrow the eye looks for
// is attached to the column it actually describes.
func headerCells(headers []string, sortCol int, desc bool) []string {
	out := make([]string, len(headers))
	copy(out, headers)
	if sortCol >= 0 && sortCol < len(out) {
		if desc {
			out[sortCol] += " ↓"
		} else {
			out[sortCol] += " ↑"
		}
	}
	return out
}

func paintHeader(headers []string, widths []int, sortCol int) string {
	cells := make([]string, len(headers))
	for i, h := range headers {
		cells[i] = clip(h, widths[i])
		if i == sortCol {
			cells[i] = sortColStyle.Render(cells[i])
		} else {
			cells[i] = colStyle.Render(cells[i])
		}
	}
	return joinCells(cells, headers, widths)
}

func paintRow(headers, row []string, widths []int) string {
	muted := rowMuted(headers, row)
	cells := make([]string, len(widths))
	plain := make([]string, len(widths))
	for i := range widths {
		v := ""
		if i < len(row) {
			v = row[i]
		}
		v = clip(v, widths[i])
		plain[i] = v
		switch {
		case muted:
			cells[i] = mutedStyle.Render(v)
		case i < len(headers):
			if tn := cellTone(headers[i], v); tn != toneNone {
				cells[i] = toneStyle(tn).Render(v)
			} else {
				cells[i] = v
			}
		default:
			cells[i] = v
		}
	}
	return joinCells(cells, plain, widths)
}

// joinCells lays styled cells on the grid. Padding is measured from the unstyled text
// because escape sequences occupy no columns.
func joinCells(styled, plain []string, widths []int) string {
	var b strings.Builder
	for i, w := range widths {
		b.WriteString(styled[i])
		if i < len(widths)-1 {
			b.WriteString(strings.Repeat(" ", w-lipgloss.Width(plain[i])+2))
		}
	}
	return b.String()
}

func columnWidths(headers []string, t *render.Table, visible []int, max int) []int {
	widths := make([]int, len(headers))
	for i, h := range headers {
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
