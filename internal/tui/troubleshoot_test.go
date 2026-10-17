package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/k8s"
)

// drill runs a key and delivers the command's message, the way the runtime would. Dropping
// the command is how a test ends up asserting against a model that never made the request.
func drill(t *testing.T, m Model, k string) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	m = next.(Model)
	if cmd == nil {
		return m
	}
	if msg := cmd(); msg != nil {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	return m
}

// The whole point of the drill-down keys: the request that goes out has to be about the
// row under the cursor. A scoped-looking screen showing cluster-wide data is worse than no
// screen, because it reads as evidence about the workload.
func TestDrillDownScopesTheRequestToTheSelectedRow(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = key(m, "down") // ledger-9, owned by no controller in the fixture

	m = drill(t, m, "l")
	if m.view.Title != "Logs" {
		t.Fatalf("l opened %q", m.view.Title)
	}
	if c.lastLogService != "ledger-9" {
		t.Errorf("logs were fetched for %q, want ledger-9", c.lastLogService)
	}

	m = drill(t, m, "r")
	if c.lastRCARef != "payments/ledger-9" {
		t.Errorf("rca was fetched for %q", c.lastRCARef)
	}

	m = drill(t, m, "c")
	if c.lastChangesRef != "payments/ledger-9" {
		t.Errorf("changes were fetched for %q", c.lastChangesRef)
	}

	m = drill(t, m, "t")
	if !strings.Contains(c.lastTraceQuery, `serviceName : "ledger-9"`) {
		t.Errorf("trace query = %q, want a serviceName filter", c.lastTraceQuery)
	}
}

// The scope has to be visible. An operator who cannot see what a screen is about will read
// it as being about the cluster.
func TestScopeIsVisibleAndClearedByEscape(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = drill(t, m, "l")

	if !strings.Contains(m.View(), "payments/checkout-1") {
		t.Errorf("scope not shown on screen:\n%s", m.View())
	}

	// esc clears the filter first, and only then the scope: they were set at different
	// times and throwing away both at once discards work the user is still doing.
	m.filter = "abc"
	m = key(m, "esc")
	if m.scope.empty() {
		t.Error("esc cleared the scope while a filter was still set")
	}
	m = key(m, "esc")
	if !m.scope.empty() {
		t.Error("second esc did not clear the scope")
	}
}

// A screen that cannot narrow by the scope must say so rather than implying it did.
func TestUnscopableViewSaysTheScopeDoesNotApply(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = drill(t, m, "l") // sets a scope
	m = submit(t, typeIn(key(m, ":"), "signals"))

	out := m.View()
	if !strings.Contains(out, "not applied here") {
		t.Errorf("unscopable view did not disclaim the scope:\n%s", out)
	}
}

// Logs with no scope are cluster-wide, and the notice has to say which it is — otherwise
// an empty scoped view and a quiet cluster look identical.
func TestLogsStateWhetherTheyAreScoped(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)

	m = submit(t, typeIn(key(m, ":"), "logs"))
	if msg := cmdMsg(t, m, "logs"); !strings.Contains(msg, "all services") {
		t.Errorf("unscoped logs notice = %q", msg)
	}

	m = newModel(t, c)
	m = drill(t, m, "l")
	if !strings.Contains(m.data.notice, "service checkout-1") {
		t.Errorf("scoped logs notice = %q", m.data.notice)
	}
}

func cmdMsg(t *testing.T, m Model, view string) string {
	t.Helper()
	d, err := views[view].fetch(t.Context(), fetchReq{Client: &fakeClient{}, OrgID: "o", ClusterID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	return d.notice
}

// Triage ranks what is failing. A completed Job pod is not a problem; a Running pod with
// restarts is, and a Pending pod looks calm and is not.
func TestTriageKeepsWhatIsActuallyWrong(t *testing.T) {
	rows := []k8s.Resource{
		{Kind: "Pod", Namespace: "a", Name: "done", Status: `{"phase":"Succeeded"}`},
		{Kind: "Pod", Namespace: "a", Name: "healthy", Status: `{"phase":"Running","restartCount":0}`},
		{Kind: "Pod", Namespace: "a", Name: "flapping", Status: `{"phase":"Running","restartCount":7}`},
		{Kind: "Pod", Namespace: "a", Name: "stuck", Status: `{"phase":"Pending"}`},
		{Kind: "Pod", Namespace: "a", Name: "dead", Status: `{"phase":"Failed"}`},
	}
	d, err := triageView.fetch(t.Context(), fetchReq{
		Client: &fakeClient{rows: rows}, OrgID: "o", ClusterID: "c"})
	if err != nil {
		t.Fatal(err)
	}

	body := renderRows(d)
	for _, want := range []string{"flapping", "stuck", "dead", "restarting", "not scheduled"} {
		if !strings.Contains(body, want) {
			t.Errorf("triage dropped %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"healthy", "done"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("triage listed the healthy pod %q:\n%s", unwanted, body)
		}
	}
}

// A partial triage said to be complete is worse than no triage: "nothing is failing" is a
// conclusion, and it must not be reached from a source that did not answer.
func TestTriageSaysWhenASourceIsMissing(t *testing.T) {
	d, err := triageView.fetch(t.Context(), fetchReq{
		Client: &failingRCAClient{fakeClient: fakeClient{rows: nil}}, OrgID: "o", ClusterID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.notice, "auto-RCA unavailable") {
		t.Errorf("notice = %q, want the missing source named", d.notice)
	}
}

var errBoom = errors.New("boom")

type failingRCAClient struct{ fakeClient }

func (f *failingRCAClient) AutoRCA(_ context.Context, _, _, _, _ string) (*api.AutoRCA, error) {
	return nil, errBoom
}

func renderRows(d dataset) string {
	var b strings.Builder
	if d.table != nil {
		for _, r := range d.table.Rows {
			b.WriteString(strings.Join(r, " ") + "\n")
		}
		b.WriteString(d.table.EmptyMessage)
	}
	b.WriteString(d.body)
	return b.String()
}

// The ask pane must subscribe before it asks. The reverse races the first frames, and the
// early steps are the ones that explain what the agent decided to look at.
func TestAskStreamsFramesThenTheAnswer(t *testing.T) {
	c := &fakeClient{
		rows: pods(),
		streamFrames: []api.StreamFrame{
			{Type: "step", Phase: "thinking", Step: 1, Thought: "checking restarts", Tool: "getPods"},
			{Type: "step", Phase: "observation", ResultSummary: "12 restarts in 1h"},
		},
		answer: &api.AgentAnswer{
			Answer:    "the readiness probe never passes",
			SessionID: "s-42",
			Verdict:   &api.AgentVerdict{Headline: "Readiness probe failing", Confidence: "HIGH"},
		},
	}
	m := newModel(t, c)
	m = key(m, "down")
	m = key(m, "a")

	if m.mode != modeAsk {
		t.Fatal("a did not open the ask pane")
	}
	// Seeded with the selection, so the common case is one keystroke and enter.
	if !strings.Contains(m.ask.input.Value(), "payments/ledger-9") {
		t.Errorf("question not seeded with the selection: %q", m.ask.input.Value())
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.ask.busy {
		t.Fatal("submitting did not mark the pane busy")
	}
	m = pump(t, m, cmd, 8)

	out := m.View()
	for _, want := range []string{"checking restarts", "getPods", "12 restarts in 1h",
		"Readiness probe failing", "confidence HIGH", "the readiness probe never passes"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in the transcript:\n%s", want, out)
		}
	}
	if m.ask.sessionID != "s-42" {
		t.Errorf("session not retained: %q", m.ask.sessionID)
	}
}

// Ctrl-C in a live run stops the agent server-side. Dropping the connection alone leaves it
// burning a slot long after the person walked away.
func TestCtrlCCancelsTheRunOnTheServer(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = key(m, "a")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	sent := m.ask.streamID
	if sent == "" {
		t.Fatal("no stream id was minted for the run")
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Model)
	if cmd != nil {
		cmd()
	}
	if got := c.cancelled(); got != sent {
		t.Errorf("cancelled %q but the run was %q", got, sent)
	}
	if m.ask.busy {
		t.Error("pane still busy after cancel")
	}
}

// pump delivers messages the way the runtime would, up to a bound so a test cannot hang.
func pump(t *testing.T, m Model, cmd tea.Cmd, max int) Model {
	t.Helper()
	cmds := []tea.Cmd{cmd}
	for i := 0; i < max && len(cmds) > 0; i++ {
		c := cmds[0]
		cmds = cmds[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		select {
		case msg := <-done:
			if msg == nil {
				continue
			}
			if batch, isBatch := msg.(tea.BatchMsg); isBatch {
				cmds = append(cmds, batch...)
				continue
			}
			next, nextCmd := m.Update(msg)
			m = next.(Model)
			cmds = append(cmds, nextCmd)
		case <-time.After(2 * time.Second):
			t.Fatal("a command blocked for 2s")
		}
	}
	return m
}
