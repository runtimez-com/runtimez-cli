package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/runtimez-com/runtimez-cli/internal/k8s"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

// View renders the whole screen: a header naming what you are looking at, the body, and a
// footer carrying state and the next available keys.
func (m Model) View() string {
	if m.mode == modeHelp {
		return m.helpView()
	}
	if m.mode == modeDetail && m.detail != nil {
		return m.detailView()
	}

	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(m.body())
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	cluster := m.opts.ClusterName
	if cluster == "" {
		cluster = m.opts.ClusterID
	}

	title := headerStyle.Render("rtz") + "  " + headerStyle.Render(m.view.Title)
	ns := m.namespace
	if ns == "" {
		ns = "all namespaces"
	}
	meta := metaStyle.Render(fmt.Sprintf("%s · %s · %s", cluster, ns, m.opts.APIURL))
	return title + "  " + meta
}

func (m Model) body() string {
	switch {
	case m.err != nil:
		return errStyle.Render("error: " + m.err.Error())
	case m.loading && len(m.visible) == 0:
		return metaStyle.Render("loading…")
	case m.data.table == nil || len(m.data.table.Rows) == 0:
		return metaStyle.Render("No resources found.")
	case len(m.visible) == 0:
		// An empty result and a filter that matched nothing are different facts.
		return metaStyle.Render(fmt.Sprintf("No rows match %q — esc clears the filter.", m.filter))
	}
	return renderTable(m.data.table, m.visible, m.cursor, m.offset, m.width, m.bodyHeight())
}

func (m Model) footer() string {
	// While typing, the input line replaces the hints — that is what the user is looking at.
	switch m.mode {
	case modeFilter:
		return keyStyle.Render("/") + m.input.View()
	case modeCommand:
		return keyStyle.Render(":") + m.input.View()
	}

	var left []string
	if n := len(m.visible); m.data.table != nil {
		total := len(m.data.table.Rows)
		if n == total {
			left = append(left, fmt.Sprintf("%d rows", total))
		} else {
			left = append(left, fmt.Sprintf("%d of %d rows", n, total))
		}
	}
	if m.filter != "" {
		left = append(left, "filter: "+m.filter)
	}
	if !m.lastUpdated.IsZero() {
		left = append(left, "updated "+k8s.Duration(sinceUpdate(m))+" ago")
	}
	if m.paused {
		left = append(left, warnStyle.Render("paused"))
	}
	if m.loading {
		left = append(left, "loading…")
	}

	status := ""
	if m.status != "" {
		status = "  " + warnStyle.Render(m.status)
	}

	hints := footerStyle.Render(strings.Join([]string{
		keyStyle.Render(":") + "cmd",
		keyStyle.Render("/") + "filter",
		keyStyle.Render("d") + "escribe",
		keyStyle.Render("p") + "ause",
		keyStyle.Render("?") + "help",
		keyStyle.Render("q") + "uit",
	}, "  "))

	return footerStyle.Render(strings.Join(left, " · ")) + status + "\n" + hints
}

func (m Model) detailView() string {
	var buf strings.Builder
	view.Detail(&buf, m.detail)

	body := buf.String()
	// Keep the pane inside the window; the detail of a big workload is longer than a screen.
	if m.height > 4 {
		lines := strings.Split(body, "\n")
		if len(lines) > m.height-4 {
			lines = lines[:m.height-4]
			lines = append(lines, metaStyle.Render("  … truncated — use `rtz describe "+m.detailRef+"` for the full output"))
		}
		body = strings.Join(lines, "\n")
	}

	return headerStyle.Render("rtz describe ") + headerStyle.Render(m.detailRef) + "\n\n" +
		body + "\n" + footerStyle.Render(keyStyle.Render("esc")+" back  "+keyStyle.Render("q")+" back")
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"↑ ↓ / j k", "move"},
		{"pgup pgdn", "page"},
		{"g / G", "top / bottom"},
		{"enter, d", "describe the selected workload"},
		{"/", "filter the visible rows"},
		{":", "command bar"},
		{"esc", "clear the filter, or close a pane"},
		{"p", "pause or resume auto-refresh"},
		{"ctrl+r", "refresh now"},
		{"?", "this help"},
		{"q, ctrl+c", "quit"},
	}

	var b strings.Builder
	b.WriteString(headerStyle.Render("Keys") + "\n\n")
	for _, r := range rows {
		b.WriteString("  " + keyStyle.Render(lipgloss.NewStyle().Width(12).Render(r[0])) + r[1] + "\n")
	}

	b.WriteString("\n" + headerStyle.Render("Commands") + "\n\n")
	for _, c := range [][2]string{
		{":pods, :deploy, :sts, :ds", "resource views"},
		{":svc, :ing, :nodes, :jobs", "resource views"},
		{":all", "every kind"},
		{":risk, :signals, :changes", "reliability views"},
		{":logs, :traces", "observability views"},
		{":ns <name>", "scope to a namespace (bare :ns clears it)"},
		{":q", "quit"},
	} {
		b.WriteString("  " + keyStyle.Render(lipgloss.NewStyle().Width(26).Render(c[0])) + c[1] + "\n")
	}

	b.WriteString("\n" + metaStyle.Render(
		"Every view here has a flag equivalent — :deploy is `rtz get deploy`, d is `rtz describe`.") + "\n")
	b.WriteString("\n" + footerStyle.Render("press any key to return"))
	return b.String()
}

// sinceUpdate is elapsed time since the last successful fetch.
func sinceUpdate(m Model) time.Duration {
	return time.Since(m.lastUpdated)
}
