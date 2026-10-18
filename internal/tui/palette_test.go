package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func tab(m Model) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	return next.(Model)
}

// The menu and the registry are one vocabulary: a name the menu offers must open something,
// and a screen the registry has must be findable from the menu.
func TestCatalogMatchesTheViewRegistry(t *testing.T) {
	builtins := map[string]bool{"ns": true, "namespace": true, "q": true, "quit": true, "exit": true}
	listed := map[string]bool{}
	for _, c := range catalog {
		for _, n := range c.names() {
			if listed[n] {
				t.Errorf(":%s is listed twice", n)
			}
			listed[n] = true
			if _, ok := views[n]; !ok && !builtins[n] {
				t.Errorf("the menu offers :%s but it opens nothing", n)
			}
		}
		if c.Group == "" || c.Desc == "" {
			t.Errorf(":%s needs a group and a description", c.Name)
		}
	}
	for n := range views {
		if !listed[n] {
			t.Errorf("view :%s is not in the menu", n)
		}
	}
}

func TestEmptyCommandBarListsEveryScreenGrouped(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m.height = 80 // tall enough to show the whole catalog without scrolling
	m = key(m, ":")
	out := m.View()
	for _, want := range []string{"Resources", "Reliability", "Observability", "Capacity & lifecycle", "Add-ons", "Navigation",
		"karpenter", "nodepools", "Karpenter NodePools", "maintain", "ns <name>", "↑↓ select", "tab complete"} {
		if !strings.Contains(out, want) {
			t.Errorf("menu is missing %q", want)
		}
	}
	if strings.Contains(out, "web-1") {
		t.Error("the menu should take the table's place, not sit under it")
	}
}

func TestMatchCommandsRanking(t *testing.T) {
	first := func(q string) string {
		ms := matchCommands(q)
		if len(ms) == 0 {
			return ""
		}
		return ms[0].Name
	}
	for q, want := range map[string]string{
		"ca":       "ca",        // exact beats the capacity prefix
		"cap":      "capacity",  // alias
		"kar":      "karpenter", // prefix
		"mesh":     "istio",     // alias
		"kapenter": "karpenter", // typo, by subsequence
		"po":       "pods",
	} {
		if got := first(q); got != want {
			t.Errorf("first match for %q = %q, want %q", q, got, want)
		}
	}

	names := map[string]bool{}
	for _, c := range matchCommands("scal") {
		names[c.Name] = true
	}
	if !names["karpenter"] || !names["ca"] {
		t.Errorf(`"scal" should find both autoscalers by description, got %v`, names)
	}
	if ms := matchCommands("ns pay"); len(ms) != 1 || ms[0].Name != "ns" {
		t.Errorf("with an argument the menu shows only that command, got %v", ms)
	}
	if ms := matchCommands("wibble"); len(ms) != 0 {
		t.Errorf("wibble matched %v", ms)
	}
}

func TestEnterOpensTheHighlightedCommand(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, ":")
	m = typeIn(m, "ist")
	m = key(m, "enter")
	if m.view.Title != "Mesh workloads" {
		t.Errorf("view = %q, want the Istio screen", m.view.Title)
	}

	// Arrows move the highlight; enter opens the highlighted row, not the first.
	m = key(m, ":")
	m = key(m, "down")
	m = key(m, "down")
	m = key(m, "up")
	if m.palSel != 1 {
		t.Fatalf("palSel = %d", m.palSel)
	}
	m = key(m, "enter")
	if m.view.Title != "Deployments" {
		t.Errorf("view = %q, want the second entry (Deployments)", m.view.Title)
	}
}

func TestTypingResetsTheHighlight(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, ":")
	m = key(m, "down")
	m = key(m, "down")
	m = typeIn(m, "e")
	if m.palSel != 0 {
		t.Errorf("palSel = %d after typing, want 0", m.palSel)
	}
}

func TestTabCompletesTheHighlightedName(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, ":")
	m = typeIn(m, "karp")
	m = tab(m)
	if got := m.input.Value(); got != "karpenter" {
		t.Errorf("tab filled %q", got)
	}

	// A command with an argument is filled with the space, ready for the argument.
	m = key(m, "esc")
	m = key(m, ":")
	m = typeIn(m, "namesp")
	m = tab(m)
	if got := m.input.Value(); got != "ns " {
		t.Errorf("tab filled %q, want %q", got, "ns ")
	}
	m = typeIn(m, "payments")
	if out := m.View(); !strings.Contains(out, "Scope to a namespace") || !strings.Contains(out, "enter runs :ns payments") {
		t.Error("with an argument typed, the menu should show the command and what enter will run")
	}
	m = key(m, "enter")
	if m.namespace != "payments" {
		t.Errorf("namespace = %q", m.namespace)
	}
}

func TestUnknownCommandSuggestsTheNearestName(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, ":")
	m = typeIn(m, "kapenterx")
	if out := m.View(); !strings.Contains(out, "did you mean :karpenter?") {
		t.Error("the menu should suggest the nearest name when nothing matches")
	}
	m = key(m, "enter")
	if !strings.Contains(m.status, "did you mean :karpenter?") {
		t.Errorf("status = %q", m.status)
	}
}

// Opening the menu must not move the frame: same line count, nothing wider than the window,
// and the highlight stays on screen however far it is moved.
func TestMenuKeepsTheFrame(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {60, 22}, {200, 50}} {
		m := newModel(t, &fakeClient{rows: pods()})
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		before := strings.Count(m.View(), "\n")

		m = key(m, ":")
		for i := 0; i < len(catalog)+5; i++ {
			m = key(m, "down")
		}
		out := m.View()
		if got := strings.Count(out, "\n"); got != before {
			t.Errorf("%dx%d: menu has %d lines, table had %d", size[0], size[1], got, before)
		}
		for _, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > size[0] {
				t.Errorf("%dx%d: line %d wide: %q", size[0], size[1], w, line)
			}
		}
		if !strings.Contains(out, "Quit") {
			t.Errorf("%dx%d: the last entry is highlighted but not on screen", size[0], size[1])
		}
		if m.palSel != len(catalog)-1 {
			t.Errorf("palSel = %d, want it clamped to the last entry", m.palSel)
		}
	}
}

func TestHelpListsCommandsFromTheCatalog(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m.height = 60
	m = key(m, "?")
	out := m.View()
	for _, c := range catalog {
		if !strings.Contains(out, ":"+c.Name) {
			t.Errorf("help is missing :%s", c.Name)
		}
	}
}
