package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/runtimez-com/runtimez-cli/internal/version"
)

// The two tab lines are the menu kept on screen: the groups of the catalog on top, numbered
// for the 1–5 keys, and the screens of the current group under them, walked with tab. They
// answer "what else is here" without the operator having to ask.

// tabStripLines is how many rows tabStrip draws; bodyHeight and the panes budget for it.
const tabStripLines = 2

// tabLabels shortens a group name for its tab. Groups not listed use their own name.
var tabLabels = map[string]string{"Capacity & lifecycle": "Capacity"}

// tabGroups is the catalog's groups in order, without Navigation (commands, not screens).
func tabGroups() []string {
	var out []string
	for _, c := range catalog {
		if c.Group == "Navigation" {
			continue
		}
		if len(out) == 0 || out[len(out)-1] != c.Group {
			out = append(out, c.Group)
		}
	}
	return out
}

// groupScreens is the primary names of one group's screens, in catalog order.
func groupScreens(group string) []string {
	var out []string
	for _, c := range catalog {
		if c.Group == group {
			out = append(out, c.Name)
		}
	}
	return out
}

// commandFor resolves a view name or alias to its catalog entry.
func commandFor(name string) (command, bool) {
	for _, c := range catalog {
		for _, n := range c.names() {
			if n == name {
				return c, true
			}
		}
	}
	return command{}, false
}

// current is the catalog entry of the screen on show.
func (m Model) current() (command, bool) {
	if m.histPos < 0 || m.histPos >= len(m.history) {
		return command{}, false
	}
	return commandFor(m.history[m.histPos])
}

// remember records the screen as the one to come back to when its group's number is pressed.
func (m Model) remember(name string) Model {
	c, ok := commandFor(name)
	if !ok {
		return m
	}
	last := make(map[string]string, len(m.groupLast)+1)
	for k, v := range m.groupLast {
		last[k] = v
	}
	last[c.Group] = c.Name
	m.groupLast = last
	return m
}

// switchGroup opens group n (1-based) at the screen last used there, or its first screen.
func (m Model) switchGroup(n int) (tea.Model, tea.Cmd) {
	groups := tabGroups()
	if n < 1 || n > len(groups) {
		return m, nil
	}
	g := groups[n-1]
	if cur, ok := m.current(); ok && cur.Group == g {
		return m, nil
	}
	name := m.groupLast[g]
	if name == "" {
		name = groupScreens(g)[0]
	}
	return m.switchView(name)
}

// cycleScreen moves to the next (+1) or previous (-1) screen in the current group, wrapping.
func (m Model) cycleScreen(delta int) (tea.Model, tea.Cmd) {
	cur, ok := m.current()
	if !ok {
		return m, nil
	}
	screens := groupScreens(cur.Group)
	i := indexOf(screens, cur.Name)
	next := screens[((i+delta)%len(screens)+len(screens))%len(screens)]
	return m.switchView(next)
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return 0
}

// tabStrip is the group tabs over the current group's screen tabs.
func (m Model) tabStrip() string {
	return m.groupLine() + "\n" + m.screenLine()
}

func (m Model) groupLine() string {
	w := m.screenWidth()
	cur, _ := m.current()
	right := metaStyle.Render(m.identity() + " · rtz " + version.Version)

	// Full labels, then three letters, then numbers alone: the active group always shows.
	for _, short := range []int{0, 3, -1} {
		var parts []string
		for i, g := range tabGroups() {
			label := g
			if l, ok := tabLabels[g]; ok {
				label = l
			}
			switch {
			case short == -1:
				label = ""
			case short > 0 && len(label) > short:
				label = label[:short]
			}
			num := itoa(i + 1)
			if g == cur.Group {
				parts = append(parts, chipStyle.Render(" "+strings.TrimSpace(num+" "+label)+" "))
				continue
			}
			parts = append(parts, keyStyle.Render(" "+num)+metaStyle.Render(strings.TrimRight(" "+label, " ")))
		}
		left := strings.Join(parts, " ")
		switch {
		case lipgloss.Width(left)+1+lipgloss.Width(right) <= w:
			return spread(left, right, w)
		case lipgloss.Width(left) <= w:
			return left
		}
	}
	return clip(chipStyle.Render(" "+m.view.Title+" "), w)
}

func (m Model) screenLine() string {
	w := m.screenWidth()
	cur, ok := m.current()

	var tabs []string
	active := 0
	if ok {
		for i, name := range groupScreens(cur.Group) {
			if name == cur.Name {
				active = i
				tabs = append(tabs, chipStyle.Render(" "+name+" "))
				continue
			}
			tabs = append(tabs, metaStyle.Render(" "+name+" "))
		}
	} else {
		tabs = []string{chipStyle.Render(" " + m.view.Title + " ")}
	}

	scope := "  " + metaStyle.Render(m.scopeLabel()) + m.breadcrumb()
	keys := footerStyle.Render(keyStyle.Render("tab") + " next   " + keyStyle.Render(":") + " browse   " +
		keyStyle.Render("/") + " filter   " + keyStyle.Render("?") + " help")

	// A long group drops its leading tabs rather than the one on show.
	start := 0
	for start < active && lipgloss.Width(strings.Join(tabs[start:], "")) > w-2 {
		start++
	}
	left := strings.Join(tabs[start:], "")
	if start > 0 {
		left = metaStyle.Render("‹") + left
	}

	switch {
	case lipgloss.Width(left+scope)+1+lipgloss.Width(keys) <= w:
		return spread(left+scope, keys, w)
	case lipgloss.Width(left+scope) <= w:
		return left + scope
	}
	return clip(left, w)
}
