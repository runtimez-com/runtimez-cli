package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/runtimez-com/runtimez-cli/internal/k8s"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

// Screen layout, top to bottom: a tab strip naming what is in focus, a titled panel of
// context and keys, the table in its own titled panel, then hints and a status line.
//
// Every key drawn in the chrome is a key handleNormalKey actually accepts. A hint bar that
// advertises a binding the binary does not have is worse than no hint bar — it sends the
// operator looking for a feature during the one hour they cannot afford to.

const logo = "┬─┐┬ ┬┌┐┌┌┬┐┬┌┬┐┌─┐┌─┐\n" +
	"├┬┘│ ││││ │ ││││├┤ ┌─┘\n" +
	"┴└─└─┘┘└┘ ┴ ┴┴ ┴└─┘└─┘"

// View renders the whole screen.
func (m Model) View() string {
	if m.mode == modeHelp {
		return m.helpView()
	}
	if m.mode == modeAsk {
		return m.askView()
	}
	if m.mode == modeDetail && m.rowPane != "" {
		return m.rowPaneView()
	}
	if m.mode == modeDetail && m.detail != nil {
		return m.detailView()
	}

	var b strings.Builder
	b.WriteString(m.tabStrip())
	b.WriteByte('\n')
	if m.mode == modeCommand {
		// The menu takes the context panel's rows as well as the table's: the whole
		// catalog fits on an ordinary terminal, and the frame is still the same height.
		b.WriteString(m.paletteView())
		b.WriteByte('\n')
		b.WriteString(m.footer())
		return b.String()
	}
	if head := m.contextPanel(); head != "" {
		b.WriteString(head)
		b.WriteByte('\n')
	}
	b.WriteString(m.bodyPanel())
	b.WriteByte('\n')
	b.WriteString(m.footer())
	return b.String()
}

// screenWidth is the usable column count, with a floor so a zero-size model (no
// WindowSizeMsg yet) still renders something readable rather than nothing.
func (m Model) screenWidth() int {
	if m.width < 20 {
		return 100
	}
	return m.width
}

func (m Model) identity() string {
	if m.opts.ContextName != "" {
		return m.opts.ContextName
	}
	return "no context"
}

// breadcrumb names the workload the drill-down keys are acting on. Without it, a logs
// screen scoped to one pod is indistinguishable from the cluster-wide one.
func (m Model) breadcrumb() string {
	if m.scope.empty() {
		return ""
	}
	marker := valueStyle.Render(m.scope.ref())
	if !m.view.scopable {
		// Say plainly that this screen ignores the scope rather than implying it applies.
		return metaStyle.Render(" · scope ") + marker + metaStyle.Render(" (not applied here)")
	}
	return metaStyle.Render(" · ") + marker
}

func (m Model) scopeLabel() string {
	if m.namespace == "" {
		return "all namespaces"
	}
	return "namespace " + m.namespace
}

// contextPanel is the box naming what you are pointed at and which keys act on it. It is
// dropped on a short terminal, where rows are worth more than orientation.
func (m Model) contextPanel() string {
	if m.height > 0 && m.height < 20 {
		return ""
	}

	cluster := m.opts.ClusterName
	if cluster == "" {
		cluster = m.opts.ClusterID
	}
	ns := m.namespace
	if ns == "" {
		ns = "<all>"
	}
	count := "—"
	if m.data.table != nil {
		count = k8s.Itoa(len(m.data.table.Rows))
	}

	scopeText := metaStyle.Render("<none> — select a row")
	if !m.scope.empty() {
		// Clipped: a long pod name would otherwise push the key columns off the panel and
		// cost every hint its label. The full reference is in the tab strip above.
		scopeText = valueStyle.Render(clip(m.scope.ref(), 30))
		if m.scope.Service != "" && m.scope.Service != m.scope.Name {
			scopeText += metaStyle.Render(" → " + clip(m.scope.Service, 22))
		}
	}

	facts := field("Context", valueStyle.Render(m.identity())) + "\n" +
		field("Cluster", valueStyle.Render(cluster)) + "\n" +
		field("API", urlStyle.Render(m.opts.APIURL)) + "\n" +
		field("Namespace", valueStyle.Render(ns)) + "\n" +
		field("Resource", valueStyle.Render(m.view.Title)) + "\n" +
		field("Scope", scopeText) + "\n" +
		field("Count", valueStyle.Render(count))

	blocks := []string{facts, keyBlock(
		[2]string{"↵", "describe"},
		[2]string{"l", "its logs"},
		[2]string{"r", "its root cause"},
		[2]string{"c", "its changes"},
		[2]string{"t", "its traces"},
		[2]string{"a", "ask about it"},
	), keyBlock(
		[2]string{"/", "filter"},
		[2]string{":", "browse screens"},
		[2]string{"s", "sort column"},
		[2]string{"i", "invert sort"},
		[2]string{"w", "wide columns"},
		[2]string{"0", "all namespaces"},
	), keyBlock(
		[2]string{"p", "pause refresh"},
		[2]string{"ctrl+r", "refresh now"},
		[2]string{"g G", "top / bottom"},
		[2]string{"[ ]", "back / forward"},
		[2]string{"esc", "clear filter/scope"},
		[2]string{"q", "quit"},
	)}

	body := columns(3, blocks...)
	// The wordmark is the first thing to go: it is decoration, and the key columns are not.
	if m.screenWidth() >= 118 {
		body = alignRight(body, logoStyle.Render(logo), m.screenWidth()-borderCost)
	}
	return panel(cluster, body, m.screenWidth())
}

func field(label, value string) string {
	return labelStyle.Render(lipgloss.NewStyle().Width(11).Render(label+":")) + value
}

// keyBlock sizes its key column to its own widest key, so a block holding only short keys
// is not padded out to make room for one in a different block.
func keyBlock(rows ...[2]string) string {
	w := 0
	for _, r := range rows {
		if n := lipgloss.Width(r[0]); n > w {
			w = n
		}
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, keyStyle.Render(lipgloss.NewStyle().Width(w+1).Render(r[0]))+metaStyle.Render(r[1]))
	}
	return strings.Join(lines, "\n")
}

// bodyPanel is the data, framed and titled with what it is and how much of it there is.
func (m Model) bodyPanel() string {
	title := m.view.Title
	if m.data.table != nil {
		if n, total := len(m.visible), len(m.data.table.Rows); n != total {
			title += fmt.Sprintf(" [%d/%d]", n, total)
		} else {
			title += fmt.Sprintf(" [%d]", total)
		}
	}
	return panel(title, m.body(), m.screenWidth())
}

func (m Model) body() string {
	height := m.bodyHeight()
	pad := func(s string) string {
		// Keep the frame a constant height so the screen does not jump between an error,
		// a loading state and data.
		lines := strings.Count(s, "\n") + 1
		if lines < height+1 {
			s += strings.Repeat("\n", height+1-lines)
		}
		return s
	}

	switch {
	case m.err != nil:
		return pad(errStyle.Render("error: " + m.err.Error()))
	case m.data.body != "":
		// Some screens are prose, not rows — a root cause is a narrative.
		return pad(clipLines(m.data.body, height+1))
	case m.loading && m.data.table == nil && m.data.body == "":
		// Nothing for this screen has arrived yet. A refresh of a loaded screen keeps its
		// rows and says so in the corner instead.
		return pad(m.loaderLine("Loading " + m.view.Title + "…"))
	case m.data.table == nil || len(m.data.table.Rows) == 0:
		msg := "No resources found."
		if m.data.table != nil && m.data.table.EmptyMessage != "" {
			msg = m.data.table.EmptyMessage
		}
		return pad(metaStyle.Render(msg))
	case len(m.visible) == 0:
		// An empty result and a filter that matched nothing are different facts.
		return pad(metaStyle.Render(fmt.Sprintf("No rows match %q — esc clears the filter.", m.filter)))
	}

	return pad(renderTable(m.activeTable(), tableOpts{
		visible: m.visible, cursor: m.cursor, offset: m.offset,
		width: m.screenWidth() - borderCost, height: height,
		sortCol: m.sortCol, sortDesc: m.sortDesc,
	}))
}

func (m Model) footer() string {
	// While typing, the input line replaces the hints — that is what the user is looking at.
	switch m.mode {
	case modeFilter:
		return keyStyle.Render(" /") + m.input.View()
	case modeCommand:
		// Two lines, like the hint and status lines it replaces, so the frame holds still.
		return keyStyle.Render(" :") + m.input.View() + "\n" + " " + strings.Join([]string{
			hint("↑↓", "select"), hint("tab", "complete"), hint("enter", "open"), hint("esc", "cancel"),
		}, "  ")
	}

	// Most useful first; what does not fit the window is dropped rather than wrapped. The
	// help screen lists them all.
	hints := " "
	for _, h := range []string{
		hint(":", "screens"), hint("1-5", "groups"), hint("tab", "next"), hint("/", "filter"),
		hint("?", "help"), hint("d", "describe"), hint("l", "logs"), hint("r", "rca"),
		hint("c", "changes"), hint("t", "traces"), hint("a", "ask"), hint("s", "sort"),
		hint("w", "wide"), hint("q", "quit"),
	} {
		if lipgloss.Width(hints+h) > m.screenWidth() {
			break
		}
		hints += h + "  "
	}
	hints = strings.TrimRight(hints, " ")

	var left []string
	if m.data.table != nil {
		n, total := len(m.visible), len(m.data.table.Rows)
		if n == total {
			left = append(left, fmt.Sprintf("%d %s", total, plural(total, "row")))
		} else {
			left = append(left, fmt.Sprintf("%d of %d %s", n, total, plural(total, "row")))
		}
	}
	if m.filter != "" {
		left = append(left, "filter: "+m.filter)
	}
	if m.wide {
		left = append(left, "wide")
	}
	if !m.lastUpdated.IsZero() {
		left = append(left, "updated "+k8s.Duration(sinceUpdate(m))+" ago")
	}

	status := footerStyle.Render(" " + strings.Join(left, " · "))
	if m.data.notice != "" {
		status += "  " + warnStyle.Render(m.data.notice)
	}
	if m.status != "" {
		status += "  " + warnStyle.Render(m.status)
	}

	return hints + "\n" + spread(status, m.liveMarker()+" ", m.screenWidth())
}

// liveMarker says whether what is on screen is still tracking the cluster. "paused" has to
// be visible: a stale screen read as live is how someone concludes an incident is over.
func (m Model) liveMarker() string {
	switch {
	case m.err != nil:
		return errStyle.Render("● error")
	case m.paused:
		return warnStyle.Render("❚❚ paused")
	case m.loading && m.describing != "":
		return m.spin.View() + metaStyle.Render(" describing "+clip(m.describing, 40))
	case m.loading && m.data.table == nil && m.data.body == "":
		return m.spin.View() + metaStyle.Render(" loading")
	case m.loading:
		return m.spin.View() + metaStyle.Render(" refreshing")
	default:
		return okStyle.Render("● live")
	}
}

// clipLines keeps a prose body inside the pane and says when it was cut.
func clipLines(body string, max int) string {
	lines := strings.Split(body, "\n")
	if len(lines) <= max {
		return body
	}
	lines = lines[:max-1]
	lines = append(lines, metaStyle.Render("… truncated — the flag command prints it all"))
	return strings.Join(lines, "\n")
}

func hint(key, label string) string {
	return keyStyle.Render(key) + footerStyle.Render(" "+label)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// alignRight puts a multi-line block flush against the right edge of another, top-aligned.
func alignRight(body, right string, width int) string {
	rightLines := strings.Split(right, "\n")
	rw := 0
	for _, l := range rightLines {
		if w := lipgloss.Width(l); w > rw {
			rw = w
		}
	}
	bodyLines := strings.Split(body, "\n")
	bw := 0
	for _, l := range bodyLines {
		if w := lipgloss.Width(l); w > bw {
			bw = w
		}
	}
	if bw+rw+2 > width {
		return body
	}
	for i := range bodyLines {
		if i >= len(rightLines) {
			continue
		}
		pad := width - lipgloss.Width(bodyLines[i]) - lipgloss.Width(rightLines[i])
		if pad < 1 {
			pad = 1
		}
		bodyLines[i] += strings.Repeat(" ", pad) + rightLines[i]
	}
	return strings.Join(bodyLines, "\n")
}

// spread pushes right to the right-hand edge, degrading to a single space when the two
// halves do not both fit.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) detailView() string {
	var buf strings.Builder
	view.Detail(&buf, m.detail)

	body := buf.String()
	// Keep the pane inside the window; the detail of a big workload is longer than a screen.
	if m.height > 6 {
		lines := strings.Split(body, "\n")
		if len(lines) > m.height-7 {
			lines = lines[:m.height-7]
			lines = append(lines, metaStyle.Render("… truncated — `rtz describe "+m.detailRef+"` prints it all"))
		}
		body = strings.Join(lines, "\n")
	}

	return m.tabStrip() + "\n" +
		panel("describe "+m.detailRef, body, m.screenWidth()) + "\n" +
		footerStyle.Render(" "+keyStyle.Render("esc")+" back   "+keyStyle.Render("q")+" back")
}

func (m Model) rowPaneView() string {
	body := strings.TrimRight(m.rowPane, "\n")
	if m.height > 7 {
		body = clipLines(body, m.height-7)
	}
	return m.tabStrip() + "\n" +
		panel(strings.ToLower(m.view.Title), body, m.screenWidth()) + "\n" +
		footerStyle.Render(" "+keyStyle.Render("esc")+" back   "+keyStyle.Render("q")+" back")
}

func (m Model) helpView() string {
	keys := [][2]string{
		{"↑ ↓ / j k", "move"},
		{"pgup pgdn", "page"},
		{"g / G", "top / bottom"},
		{"enter, d", "describe the selected workload, or open the row's findings"},
		{"/", "filter the visible rows"},
		{":", "browse and search every screen"},
		{"1 – 5", "switch group: " + groupKeyList()},
		{"tab", "next screen in the group (shift+tab: previous)"},
		{"s / i", "sort by the next column / invert the order"},
		{"w", "wide columns, when the view has extras"},
		{"0", "clear the namespace scope"},
		{"[ / ]", "back / forward through the views you visited"},
		{"l r c t", "the selection's logs, root cause, changes, traces"},
		{"a", "ask the agent about the selection"},
		{"esc", "clear the filter, then the scope, then close a pane"},
		{"p", "pause or resume auto-refresh"},
		{"ctrl+r", "refresh now"},
		{"?", "this help"},
		{"q, ctrl+c", "quit"},
	}

	var b strings.Builder
	b.WriteString(headerStyle.Render("Keys") + "\n")
	for _, r := range keys {
		b.WriteString(keyStyle.Render(lipgloss.NewStyle().Width(12).Render(r[0])) + r[1] + "\n")
	}
	b.WriteString("\n" + headerStyle.Render("Commands") + metaStyle.Render("  (press : to browse and search them)") + "\n")
	for _, g := range commandGroups() {
		b.WriteString(keyStyle.Render(lipgloss.NewStyle().Width(22).Render(g.name)) + strings.Join(g.names, "  ") + "\n")
	}
	b.WriteString("\n" + metaStyle.Render(
		"Every view here has a flag equivalent — :deploy is `rtz get deploy`, d is `rtz describe`."))

	return m.tabStrip() + "\n" +
		panel("keys", b.String(), m.screenWidth()) + "\n" +
		footerStyle.Render(" press any key to return")
}

// sinceUpdate is elapsed time since the last successful fetch.
func sinceUpdate(m Model) time.Duration {
	return time.Since(m.lastUpdated)
}

// loaderLine is the spinner and what it is waiting for, with the wait once it is long enough
// to wonder about — a request that is slow and one that is stuck look the same otherwise.
func (m Model) loaderLine(what string) string {
	line := m.spin.View() + " " + metaStyle.Render(what)
	if d := time.Since(m.loadStart); d >= 2*time.Second && !m.loadStart.IsZero() {
		line += metaStyle.Render("  " + k8s.Duration(d))
	}
	return line
}
