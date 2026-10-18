package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A column sorts the way it reads or it is worse than unsorted: an operator scanning for
// the oldest pod trusts the arrow, and a lexicographic "10m before 9m" quietly lies.
func TestSortRecoversTheTypeTheTextEncodes(t *testing.T) {
	cases := []struct{ a, b string }{
		{"9m", "10m"},    // durations, not strings
		{"45s", "2m"},    // across units
		{"3h", "2d"},     // across units again
		{"370Mi", "1Gi"}, // byte suffixes
		{"LOW", "HIGH"},  // severity has an order, and it is not alphabetical
		{"MEDIUM", "CRITICAL"},
		{"2", "10"},    // plain counts
		{"1/3", "2/3"}, // ready ratios sort by how many are up
	}
	for _, c := range cases {
		rows := [][]string{{c.b}, {c.a}}
		vis := []int{0, 1}
		sortRows(rows, vis, 0, false)
		if rows[vis[0]][0] != c.a {
			t.Errorf("ascending %q vs %q put %q first", c.a, c.b, rows[vis[0]][0])
		}
	}
}

// An absent value is not a zero. Sorting "<none>" to the top of an ascending latency column
// would put the rows we know nothing about where the fastest ones belong.
func TestAbsentValuesSortAfterRealOnes(t *testing.T) {
	rows := [][]string{{"<none>"}, {"5"}, {"-"}, {"1"}}
	vis := []int{0, 1, 2, 3}
	sortRows(rows, vis, 0, false)
	if got := rows[vis[0]][0]; got != "1" {
		t.Fatalf("first = %q, want 1", got)
	}
	if got := rows[vis[1]][0]; got != "5" {
		t.Fatalf("second = %q, want 5", got)
	}
}

func TestSortCyclesThroughColumnsAndBackToServerOrder(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	cols := len(m.data.table.Headers)

	for i := 0; i < cols; i++ {
		m = key(m, "s")
		if m.sortCol != i {
			t.Fatalf("after %d presses sortCol = %d, want %d", i+1, m.sortCol, i)
		}
	}
	// One more press has to reach the unsorted state, not wrap straight to column 0 —
	// otherwise the server's own order becomes unreachable once you leave it.
	m = key(m, "s")
	if m.sortCol != -1 {
		t.Fatalf("sortCol = %d after the last column, want -1", m.sortCol)
	}
	if !strings.Contains(m.status, "server") {
		t.Errorf("returning to the unsorted state was not announced: %q", m.status)
	}
}

func TestSortMarkerNamesTheSortedColumnAndItsDirection(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	if strings.Contains(m.View(), "↑") || strings.Contains(m.View(), "↓") {
		t.Error("unsorted table drew a sort arrow")
	}
	m = key(m, "s") // NAMESPACE
	if !strings.Contains(m.View(), "NAMESPACE ↑") {
		t.Errorf("ascending marker missing:\n%s", m.View())
	}
	m = key(m, "i")
	if !strings.Contains(m.View(), "NAMESPACE ↓") {
		t.Errorf("inverted marker missing:\n%s", m.View())
	}
}

func TestInvertWithoutASortSaysSoRatherThanDoingNothing(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, "i")
	if !strings.Contains(m.status, "press s") {
		t.Errorf("status = %q", m.status)
	}
}

func TestWideRevealsTheExtraColumns(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	if strings.Contains(m.View(), "CONTROLLED BY") {
		t.Error("wide columns shown before w was pressed")
	}
	m = key(m, "w")
	out := m.View()
	for _, want := range []string{"NODE", "CONTROLLED BY"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q after w:\n%s", want, out)
		}
	}
	m = key(m, "w")
	if strings.Contains(m.View(), "CONTROLLED BY") {
		t.Error("w did not toggle back")
	}
}

func TestHistoryWalksBackAndForward(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = typeIn(key(m, ":"), "deploy")
	m = submit(t, m)
	if m.view.Title != "Deployments" {
		t.Fatalf("view = %q", m.view.Title)
	}

	m = key(m, "[")
	if m.view.Title != "Pods" {
		t.Fatalf("back landed on %q, want Pods", m.view.Title)
	}
	m = key(m, "]")
	if m.view.Title != "Deployments" {
		t.Fatalf("forward landed on %q, want Deployments", m.view.Title)
	}
	m = key(m, "]")
	if !strings.Contains(m.status, "no later view") {
		t.Errorf("walking off the end was silent: %q", m.status)
	}
}

func TestAllNamespacesKeyClearsTheScope(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = submit(t, typeIn(key(m, ":"), "ns payments"))
	if m.namespace != "payments" {
		t.Fatalf("namespace = %q", m.namespace)
	}
	m = key(m, "0")
	if m.namespace != "" {
		t.Fatalf("0 left namespace %q", m.namespace)
	}
}

// A screen that says nothing while the data underneath has stopped moving is how someone
// concludes an incident is over.
func TestPausedStateIsVisibleOnScreen(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	if !strings.Contains(m.View(), "live") {
		t.Errorf("no live marker:\n%s", m.View())
	}
	m = key(m, "p")
	out := m.View()
	if !strings.Contains(out, "paused") || strings.Contains(out, "● live") {
		t.Errorf("paused screen still reads as live:\n%s", out)
	}
}

// The frame is decoration; the rows are not. On a terminal too short or too narrow for the
// chrome the data has to survive, not the box.
func TestChromeYieldsToTheDataOnASmallTerminal(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})

	short, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 16})
	out := short.(Model).View()
	if strings.Contains(out, "Context:") {
		t.Errorf("context panel survived a 16-row terminal:\n%s", out)
	}
	if !strings.Contains(out, "checkout-1") {
		t.Errorf("rows lost on a short terminal:\n%s", out)
	}

	narrow, _ := m.Update(tea.WindowSizeMsg{Width: 24, Height: 30})
	out = narrow.(Model).View()
	if strings.Contains(out, "╭") {
		t.Errorf("box drawn at 24 columns:\n%s", out)
	}
}

// An off-by-one in the border arithmetic is invisible in a screenshot and glaring on a real
// terminal, where the right-hand edge zig-zags.
func TestEveryChromeLineIsExactlyTheTerminalWidth(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	for _, w := range []int{80, 100, 132, 180} {
		sized, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 34})
		for i, line := range strings.Split(sized.(Model).View(), "\n") {
			if !strings.ContainsAny(line, "╭│╰") {
				continue // only the framed lines carry the promise
			}
			if got := lipgloss.Width(line); got != w {
				t.Errorf("width %d: line %d is %d columns:\n%s", w, i, got, line)
			}
		}
	}
}

// Every key the chrome advertises has to be a key the model accepts. A hint bar that names
// a binding the binary does not have sends someone hunting during the hour they can least
// afford it.
func TestAdvertisedKeysAreAllHandled(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	out := m.View()
	for _, k := range []string{"s", "i", "w", "d", "0", "p", "?", "q", "l", "r"} {
		if !strings.Contains(out, k) {
			continue
		}
		before := m
		after := key(m, k)
		if k == "q" {
			continue // quits; nothing to compare
		}
		if after.status == before.status && after.view.Title == before.view.Title &&
			after.mode == before.mode && after.sortCol == before.sortCol &&
			after.wide == before.wide && after.paused == before.paused &&
			after.loading == before.loading {
			t.Errorf("key %q is advertised but changed nothing", k)
		}
	}
}

func TestScoreColumnsAreLeftUncoloured(t *testing.T) {
	// Higher is worse on a risk score and better on an upgrade-readiness score, so one
	// colour rule would be confidently wrong on one of them.
	if got := cellTone("SCORE", "95"); got != toneNone {
		t.Errorf("SCORE tone = %v, want toneNone", got)
	}
	if got := cellTone("STATUS", "CrashLoopBackOff"); got != toneBad {
		t.Errorf("CrashLoopBackOff tone = %v, want toneBad", got)
	}
	if got := cellTone("READY", "0/3"); got != toneBad {
		t.Errorf("0/3 tone = %v, want toneBad", got)
	}
	if got := cellTone("READY", "2/3"); got != toneWarn {
		t.Errorf("2/3 tone = %v, want toneWarn", got)
	}
	if got := cellTone("RESTARTS", "0"); got != toneNone {
		t.Errorf("0 restarts tone = %v, want toneNone", got)
	}
}

// A finished pod is evidence, not noise: it stays listed, dimmed, rather than disappearing.
func TestCompletedRowsAreMutedNotHidden(t *testing.T) {
	headers := []string{"NAME", "STATUS"}
	if !rowMuted(headers, []string{"backup-1", "Succeeded"}) {
		t.Error("a Succeeded row was not muted")
	}
	if rowMuted(headers, []string{"api-7", "Running"}) {
		t.Error("a Running row was muted")
	}
}
