package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/runtimez-com/runtimez-cli/internal/api"
)

func press(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(Model)
	if cmd != nil {
		if out := cmd(); out != nil {
			if rows, ok := out.(rowsMsg); ok {
				next, _ = m.Update(rows)
				m = next.(Model)
			}
		}
	}
	return m
}

func digit(t *testing.T, m Model, d string) Model {
	return press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(d)})
}

func TestTabLinesShowGroupsAndTheGroupsScreens(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	strip := strings.Split(m.View(), "\n")[:tabStripLines]
	for _, want := range []string{"1 Resources", "2 Reliability", "3 Observability", "4 Capacity", "5 Add-ons"} {
		if !strings.Contains(strip[0], want) {
			t.Errorf("group line is missing %q: %q", want, strip[0])
		}
	}
	for _, want := range []string{"pods", "deploy", "sts", "nodes", "all", "all namespaces"} {
		if !strings.Contains(strip[1], want) {
			t.Errorf("screen line is missing %q: %q", want, strip[1])
		}
	}
	if strings.Contains(strip[1], "karpenter") {
		t.Error("the screen line shows only the current group")
	}
}

func TestNumberKeysSwitchGroupAndReturnToTheLastScreen(t *testing.T) {
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"NodePools": karpenter("OK")}}
	m := newModel(t, c)

	m = digit(t, m, "5")
	if m.view.Title != "Karpenter NodePools" {
		t.Fatalf("5 opened %q, want the first add-on screen", m.view.Title)
	}
	if !strings.Contains(strings.Split(m.View(), "\n")[1], "traefik") {
		t.Error("after 5 the screen line lists the add-on screens")
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.view.Title != "Cluster Autoscaler" {
		t.Fatalf("tab opened %q, want the next add-on", m.view.Title)
	}

	m = digit(t, m, "1")
	if m.view.Title != "Pods" {
		t.Fatalf("1 opened %q, want Pods (the last Resources screen)", m.view.Title)
	}
	m = digit(t, m, "5")
	if m.view.Title != "Cluster Autoscaler" {
		t.Errorf("5 opened %q, want the add-on screen used last", m.view.Title)
	}

	// A number with no group, and 0 (all namespaces), keep their meaning.
	m = digit(t, m, "9")
	if m.view.Title != "Cluster Autoscaler" {
		t.Errorf("9 moved to %q", m.view.Title)
	}
}

func TestTabWrapsWithinTheGroup(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.view.Title != "All resources" {
		t.Errorf("shift+tab from the first screen opened %q, want the last (All resources)", m.view.Title)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.view.Title != "Pods" {
		t.Errorf("tab from the last screen opened %q, want the first", m.view.Title)
	}
}

// A screen reached by its alias still lights its own tab, and [ ] keep the tabs in step.
func TestTabsFollowAliasesAndHistory(t *testing.T) {
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"MeshWorkloads": &api.MeshWorkloads{}}}
	m := open(t, c, "mesh")
	if cur, _ := m.current(); cur.Name != "istio" {
		t.Fatalf("current = %q, want istio", cur.Name)
	}
	m = digit(t, m, "5")
	if m.view.Title != "Mesh workloads" {
		t.Errorf("5 on an add-on screen should stay put, got %q", m.view.Title)
	}
	m = digit(t, m, "1")
	m = key(m, "[")
	if cur, _ := m.current(); cur.Group != "Add-ons" {
		t.Errorf("after [ the tabs show %q, want Add-ons", cur.Group)
	}
}

// The tab lines never push the frame past the window, and the screen on show stays visible
// however narrow it gets.
func TestTabLinesFitTheWindow(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120, 200} {
		m := newModel(t, &fakeClient{rows: pods()})
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		m = next.(Model)
		m = digit(t, m, "5")
		m = press(t, m, tea.KeyMsg{Type: tea.KeyShiftTab}) // ebs, the last add-on tab

		out := m.View()
		if got := strings.Count(out, "\n") + 1; got != 30 {
			t.Errorf("width %d: %d lines, want the window's 30", w, got)
		}
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("width %d: line %d is %d wide: %q", w, i, lw, line)
			}
		}
		if !strings.Contains(lines[1], "ebs") {
			t.Errorf("width %d: the active screen tab is not shown: %q", w, lines[1])
		}
		if !strings.Contains(lines[0], "5") {
			t.Errorf("width %d: the active group tab is not shown: %q", w, lines[0])
		}
	}
}
