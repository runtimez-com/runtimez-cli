package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/runtimez-com/runtimez-cli/internal/k8s"
)

// switchTo opens a view from the command bar without delivering its response.
func switchTo(m Model, name string) (Model, tea.Cmd) {
	m = key(m, ":")
	m = typeIn(m, name)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(Model), cmd
}

// Between leaving a screen and the next one answering, the table area is a loader naming
// the new screen — never the old screen's rows under the new title.
func TestSwitchingScreensShowsTheLoaderNotTheOldRows(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m, _ = switchTo(m, "karpenter")

	out := m.View()
	if !strings.Contains(out, "Loading Karpenter NodePools…") {
		t.Errorf("no loader for the new screen:\n%s", out)
	}
	if strings.Contains(out, "checkout-1") {
		t.Error("the old screen's rows are still drawn under the new title")
	}
	if !strings.Contains(out, "loading") {
		t.Error("the corner marker should say loading")
	}
}

func TestLoaderShowsTheWaitOnceItIsLong(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m, _ = switchTo(m, "capacity")
	if line := m.loaderLine("x"); !strings.HasSuffix(line, "x") {
		t.Errorf("a fresh load should not show a wait: %q", line)
	}
	m.loadStart = time.Now().Add(-5 * time.Second)
	if line := m.loaderLine("x"); !strings.Contains(line, "5s") {
		t.Errorf("loader = %q, want the 5s wait", line)
	}
}

// A response for a screen already left is dropped.
func TestAResponseForAScreenAlreadyLeftIsDropped(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	stale := m.seq
	m, _ = switchTo(m, "karpenter")

	next, _ := m.Update(rowsMsg{seq: stale, data: podDataset(pods())})
	m = next.(Model)
	if strings.Contains(m.View(), "checkout-1") {
		t.Error("a pods response landed on the Karpenter screen")
	}
	if !m.loading {
		t.Error("the stale response ended the new screen's load")
	}

	next, _ = m.Update(rowsMsg{seq: m.seq, data: podDataset([]k8s.Resource{{Kind: "Pod", Namespace: "a", Name: "fresh-1"}})})
	m = next.(Model)
	if m.loading || !strings.Contains(m.View(), "fresh-1") {
		t.Error("the current screen's response should land")
	}
}

// A refresh keeps the rows and says so in the corner, rather than blanking the table.
func TestRefreshKeepsRowsAndMarksTheCorner(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, "ctrl+r")
	out := m.View()
	if !strings.Contains(out, "checkout-1") {
		t.Error("refresh blanked the table")
	}
	if !strings.Contains(out, "refreshing") {
		t.Error("the corner should say refreshing")
	}
	if strings.Contains(out, "Loading Pods") {
		t.Error("a refresh is not a fresh load")
	}

	next, _ := m.Update(tickMsg(time.Now()))
	if !next.(Model).loading {
		t.Error("an auto-refresh should mark the screen as refreshing too")
	}
}

func TestDescribeNamesTheWorkloadWhileLoading(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = next.(Model)
	if !strings.Contains(m.View(), "describing payments/checkout-1") {
		t.Error("describe should name what it is loading")
	}
	next, _ = m.Update(detailMsg{ref: "payments/checkout-1"})
	if m = next.(Model); m.describing != "" || m.loading {
		t.Error("the describe loader should end when the detail lands")
	}
}

// The spinner ticks only while something is loading: an idle screen must not redraw ten
// times a second.
func TestSpinnerRunsOnlyWhileLoading(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m.opts.Animate = true

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	if !m.spinning || cmd == nil {
		t.Fatal("starting a load should start the spinner")
	}
	next, cmd = m.Update(m.spin.Tick())
	if m = next.(Model); cmd == nil {
		t.Error("the spinner should keep ticking while loading")
	}

	next, _ = m.Update(rowsMsg{seq: m.seq, data: podDataset(pods())})
	m = next.(Model)
	next, cmd = m.Update(spinner.TickMsg{ID: m.spin.ID()})
	if m = next.(Model); cmd != nil || m.spinning {
		t.Error("the spinner should stop on the first tick after the load ends")
	}

	// Off (as in tests), no tick is ever scheduled, but the loader still draws.
	m.opts.Animate = false
	m.spinning = false
	m, _ = switchTo(m, "pending")
	if m.spinning {
		t.Error("Animate=false must not start the spinner")
	}
	if !strings.Contains(m.View(), "Loading Pending pods…") {
		t.Error("the loader draws without animation")
	}
}
