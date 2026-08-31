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
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

// fakeClient records what the model asked for and returns canned rows.
type fakeClient struct {
	rows       []k8s.Resource
	detail     *api.WorkloadDetail
	err        error
	detailErr  error
	lastKind   string
	lastNS     string
	calls      int
	detailName string
	risk       *api.WorkloadRisk
	autoRCA    *api.AutoRCA
	slowOps    *api.SlowOperations
	changes    *api.Changes
	logs       *api.LogPage
	traces     *api.TracePage
}

func (f *fakeClient) Resources(_ context.Context, _, _, kind, ns string) ([]k8s.Resource, error) {
	f.calls++
	f.lastKind, f.lastNS = kind, ns
	return f.rows, f.err
}

func (f *fakeClient) Detail(_ context.Context, _, _, ns, name, _ string) (*api.WorkloadDetail, error) {
	f.detailName = ns + "/" + name
	return f.detail, f.detailErr
}

func (f *fakeClient) WorkloadRiskPosture(_ context.Context, _, _ string) (*api.WorkloadRisk, error) {
	f.calls++
	if f.risk == nil {
		return &api.WorkloadRisk{}, f.err
	}
	return f.risk, f.err
}

func (f *fakeClient) AutoRCA(_ context.Context, _, _, _, _ string) (*api.AutoRCA, error) {
	return f.autoRCA, f.err
}

func (f *fakeClient) SlowOperations(_ context.Context, _, _, _, _ string, _ int) (*api.SlowOperations, error) {
	f.calls++
	if f.slowOps == nil {
		return &api.SlowOperations{}, f.err
	}
	return f.slowOps, f.err
}

func (f *fakeClient) SearchLogs(_ context.Context, _ api.LogSearch) (*api.LogPage, error) {
	f.calls++
	if f.logs == nil {
		return &api.LogPage{}, f.err
	}
	return f.logs, f.err
}

func (f *fakeClient) ListTraces(_ context.Context, _ api.TraceListRequest) (*api.TracePage, error) {
	f.calls++
	if f.traces == nil {
		return &api.TracePage{}, f.err
	}
	return f.traces, f.err
}

func (f *fakeClient) Changes(_ context.Context, _, _ string, _, _, _ int) (*api.Changes, error) {
	f.calls++
	if f.changes == nil {
		return &api.Changes{}, f.err
	}
	return f.changes, f.err
}

func podDataset(rows []k8s.Resource) dataset {
	return dataset{table: view.TableFor("Pod", rows, true), resources: rows}
}

func workloadDataset(rows []k8s.Resource) dataset {
	return dataset{table: view.TableFor("Deployment", rows, true), resources: rows}
}

func pods() []k8s.Resource {
	return []k8s.Resource{
		{Kind: "Pod", Namespace: "payments", Name: "checkout-1", Status: `{"phase":"Running","restartCount":0}`},
		{Kind: "Pod", Namespace: "payments", Name: "ledger-9", Status: `{"phase":"CrashLoopBackOff","restartCount":12}`},
		{Kind: "Pod", Namespace: "search", Name: "indexer-3", Status: `{"phase":"Running","restartCount":1}`},
	}
}

// newModel returns a sized model with rows already loaded, as if the first fetch landed.
func newModel(t *testing.T, c *fakeClient) Model {
	t.Helper()
	// A short interval keeps the tick commands from blocking the suite for the 10s default.
	m := New(Options{Client: c, OrgID: "org1", ClusterID: "c1", ClusterName: "prod",
		APIURL: "http://api", Refresh: 10 * time.Millisecond})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)
	m3, _ := m.Update(rowsMsg{data: podDataset(c.rows)})
	return m3.(Model)
}

func key(m Model, s string) Model {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+r":
		msg = tea.KeyMsg{Type: tea.KeyCtrlR}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

// submit presses enter, keeps the model that comes back, and delivers the resulting
// command's message the way the Bubble Tea runtime would. Dropping either half is how a
// test ends up asserting against a model that never saw the switch.
func submit(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	next, _ = m.Update(msg)
	return next.(Model)
}

func typeIn(m Model, s string) Model {
	for _, r := range s {
		m = key(m, string(r))
	}
	return m
}

func TestRendersRowsWithDerivedColumns(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	out := m.View()

	for _, want := range []string{"Pods", "prod", "checkout-1", "CrashLoopBackOff", "12", "3 rows"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestFilterNarrowsRowsAsYouType(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, "/")
	m = typeIn(m, "ledger")

	if len(m.visible) != 1 {
		t.Fatalf("visible = %d, want 1", len(m.visible))
	}
	if got, _ := m.selected(); got.Name != "ledger-9" {
		t.Errorf("selected = %q", got.Name)
	}
}

func TestEscapeClearsTheFilter(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = typeIn(key(m, "/"), "ledger")
	m = key(m, "esc") // leaves filter mode and clears it

	if m.filter != "" || len(m.visible) != 3 {
		t.Errorf("filter %q left %d rows visible, want all 3", m.filter, len(m.visible))
	}
}

// A filter matching nothing and an empty cluster are different facts, and conflating them
// sends someone hunting for a sync problem that does not exist.
func TestNoMatchesReadsDifferentlyFromNoResources(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = typeIn(key(m, "/"), "zzzz")
	if out := m.View(); !strings.Contains(out, "No rows match") {
		t.Errorf("filtered-to-empty did not say so:\n%s", out)
	}

	empty := newModel(t, &fakeClient{rows: nil})
	if out := empty.View(); !strings.Contains(out, "No resources found") {
		t.Errorf("empty result did not say so:\n%s", out)
	}
}

func TestCursorMovementStaysInBounds(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	for i := 0; i < 10; i++ {
		m = key(m, "down")
	}
	if m.cursor != 2 {
		t.Errorf("cursor ran past the last row: %d", m.cursor)
	}
	for i := 0; i < 10; i++ {
		m = key(m, "up")
	}
	if m.cursor != 0 {
		t.Errorf("cursor ran above the first row: %d", m.cursor)
	}
}

// The cursor indexes the filtered list; if it kept indexing the full one, enter would open
// a row other than the highlighted one.
func TestSelectionFollowsTheFilteredList(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = typeIn(key(m, "/"), "search")
	m = key(m, "esc")
	m = typeIn(key(m, "/"), "payments")

	got, ok := m.selected()
	if !ok || got.Namespace != "payments" {
		t.Fatalf("selected = %+v", got)
	}
}

func TestCommandSwitchesView(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "deploy")
	m = key(m, "enter")

	if m.view.Kind != "Deployment" {
		t.Fatalf("view kind = %q, want Deployment", m.view.Kind)
	}
	if !strings.Contains(m.View(), "Deployments") {
		t.Errorf("header did not follow the view switch")
	}
}

func TestCommandNamespaceScopesAndClears(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)

	m = key(m, ":")
	m = typeIn(m, "ns payments")
	m = key(m, "enter")
	if m.namespace != "payments" {
		t.Fatalf("namespace = %q", m.namespace)
	}

	// A bare :ns is the way back to every namespace.
	m = key(m, ":")
	m = typeIn(m, "ns")
	m = key(m, "enter")
	if m.namespace != "" {
		t.Errorf("bare :ns did not clear the namespace: %q", m.namespace)
	}
}

// A roadmap command must say it is unbuilt. Doing nothing silently reads as a broken key.
func TestPlannedCommandSaysItIsNotBuilt(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, ":")
	m = typeIn(m, "upgrade")
	m = key(m, "enter")

	if !strings.Contains(m.status, "not a view here") {
		t.Errorf("status = %q, want a not-built-yet message", m.status)
	}
}

func TestUnknownCommandIsReported(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, ":")
	m = typeIn(m, "wibble")
	m = key(m, "enter")

	if !strings.Contains(m.status, "unknown command") {
		t.Errorf("status = %q", m.status)
	}
}

// Detail exists for workload controllers only; opening a blank pane on a Pod would read as
// a failure rather than an absence.
func TestDescribeOnANonWorkloadExplainsInstead(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = key(m, "d")

	if c.detailName != "" {
		t.Errorf("a detail request was made for a Pod: %q", c.detailName)
	}
	if !strings.Contains(m.status, "no detail view") {
		t.Errorf("status = %q", m.status)
	}
}

func TestDescribeOpensTheDetailPane(t *testing.T) {
	c := &fakeClient{
		rows: []k8s.Resource{{Kind: "Deployment", Namespace: "payments", Name: "checkout",
			Spec: `{"replicas":3}`, Status: `{"readyReplicas":2}`}},
		detail: &api.WorkloadDetail{
			Workload: &k8s.Resource{Kind: "Deployment", Namespace: "payments", Name: "checkout",
				Spec: `{"replicas":3}`, Status: `{"readyReplicas":2}`},
		},
	}
	m := New(Options{Client: c, OrgID: "o", ClusterID: "c", Refresh: 10 * time.Millisecond})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(Model)
	m3, _ := m.Update(rowsMsg{data: workloadDataset(c.rows)})
	m = m3.(Model)

	m4, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m4.(Model)
	if cmd == nil {
		t.Fatal("enter issued no fetch")
	}
	msg := cmd() // run the fetch the way the runtime would
	m5, _ := m.Update(msg)
	m = m5.(Model)

	if m.mode != modeDetail {
		t.Fatalf("mode = %v, want detail", m.mode)
	}
	out := m.View()
	if !strings.Contains(out, "payments/checkout") || !strings.Contains(out, "2/3") {
		t.Errorf("detail pane missing content:\n%s", out)
	}

	if m = key(m, "esc"); m.mode != modeNormal {
		t.Errorf("esc did not close the detail pane")
	}
}

// Refreshing under an open pane or a half-typed filter moves the ground under the user.
func TestAutoRefreshHoldsWhileAPaneOrPromptIsOpen(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	before := c.calls

	m = key(m, "/") // filter prompt open
	m2, cmd := m.Update(tickMsg(time.Now()))
	m = m2.(Model)
	if cmd != nil {
		cmd() // the tick reschedules itself; it must not also fetch
	}
	if c.calls != before {
		t.Errorf("a refresh ran while the filter prompt was open (%d -> %d)", before, c.calls)
	}
}

func TestPauseStopsAutoRefresh(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = key(m, "p")

	if !m.paused || !strings.Contains(m.status, "paused") {
		t.Fatalf("pause not registered: paused=%v status=%q", m.paused, m.status)
	}
	before := c.calls
	m2, cmd := m.Update(tickMsg(time.Now()))
	m = m2.(Model)
	if cmd != nil {
		cmd()
	}
	if c.calls != before {
		t.Errorf("paused UI still refreshed (%d -> %d)", before, c.calls)
	}

	// And resuming brings it back, rather than pausing forever.
	m = key(m, "p")
	if m.paused {
		t.Error("second press did not resume")
	}
}

func TestManualRefreshFetches(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	before := c.calls

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("ctrl+r issued no fetch")
	}
	cmd()
	if c.calls != before+1 {
		t.Errorf("calls = %d, want %d", c.calls, before+1)
	}
}

func TestFetchErrorIsShownNotSwallowed(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m2, _ := m.Update(rowsMsg{err: errors.New("connection refused")})
	m = m2.(Model)

	if out := m.View(); !strings.Contains(out, "connection refused") {
		t.Errorf("error not surfaced:\n%s", out)
	}
}

func TestHelpOverlayListsKeysAndClosesOnAnyKey(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	m = key(m, "?")

	out := m.View()
	for _, want := range []string{"Keys", "Commands", ":ns", "describe"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	if m = key(m, "x"); m.mode != modeNormal {
		t.Error("help did not close")
	}
}

func TestQuitKeys(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); cmd == nil {
		t.Error("q did not quit")
	}
	// Ctrl-C must quit from any mode, including mid-filter.
	filtering := key(m, "/")
	if _, cmd := filtering.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c did not quit from the filter prompt")
	}
}

func TestViewSwitchSendsTheCanonicalKind(t *testing.T) {
	c := &fakeClient{rows: pods()}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "sts")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no fetch after view switch")
	}
	cmd()
	if c.lastKind != "StatefulSet" {
		t.Errorf("fetched kind = %q, want StatefulSet", c.lastKind)
	}
}

// The TUI must show the same columns as `rtz get`, since both build from internal/view.
func TestColumnsMatchTheFlagCommand(t *testing.T) {
	m := newModel(t, &fakeClient{rows: pods()})
	out := m.View()
	for _, col := range []string{"NAMESPACE", "NAME", "STATUS", "RESTARTS", "AGE"} {
		if !strings.Contains(out, col) {
			t.Errorf("column %q missing from the TUI:\n%s", col, out)
		}
	}
}

func TestCapableIsFalseWithoutATTY(t *testing.T) {
	// Go test redirects stdout to a pipe, so this is the non-interactive case by construction.
	if Capable() {
		t.Error("Capable() reported true with a piped stdout")
	}
}

func TestCapableIsFalseForDumbTerminals(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if Capable() {
		t.Error("Capable() reported true for TERM=dumb")
	}
}

// --- reliability views (M3) -------------------------------------------------

func TestRiskViewRendersScores(t *testing.T) {
	c := &fakeClient{
		rows: pods(),
		risk: &api.WorkloadRisk{
			Workloads: []api.WorkloadRiskItem{
				{Namespace: "payments", Name: "checkout", Kind: "Deployment", Score: 82, Level: "CRITICAL",
					Factors: []api.WorkloadRiskFact{{Title: "No memory limit", Severity: "HIGH", Category: "RELIABILITY"}}},
			},
			MetricsAvailable: true, CVEScanAvailable: true,
			RuntimeAvailable: true, NetworkSignalsAvailable: true,
		},
	}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "risk")
	m = submit(t, m)

	out := m.View()
	for _, want := range []string{"Workload risk", "checkout", "82", "CRITICAL", "No memory limit"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A score computed without metrics is not a clean bill of health, and the caveat has to be
// on screen rather than in a log the operator never reads.
func TestRiskViewSurfacesMissingSignals(t *testing.T) {
	c := &fakeClient{
		rows: pods(),
		risk: &api.WorkloadRisk{
			Workloads:        []api.WorkloadRiskItem{{Namespace: "a", Name: "b", Score: 5, Level: "LOW"}},
			MetricsAvailable: false, CVEScanAvailable: true,
			RuntimeAvailable: true, NetworkSignalsAvailable: true,
		},
	}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "risk")
	m = submit(t, m)

	if !strings.Contains(m.View(), "not a clean bill of health") {
		t.Errorf("missing-signal caveat absent:\n%s", m.View())
	}
}

func TestSignalsViewRendersLatency(t *testing.T) {
	c := &fakeClient{
		rows: pods(),
		slowOps: &api.SlowOperations{Items: []api.SlowOperation{
			{ServiceName: "checkout", Operation: "POST /pay", P99Ms: 1450, P50Ms: 90, CallCount: 3200, ErrorRatePct: 2.5},
		}},
	}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "signals")
	m = submit(t, m)

	out := m.View()
	for _, want := range []string{"Slow operations", "checkout", "POST /pay", "1.45s", "2.50%"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestChangesViewRenders(t *testing.T) {
	when := time.Now().Add(-2 * time.Hour)
	c := &fakeClient{
		rows: pods(),
		changes: &api.Changes{
			Items: []api.ChangeEvent{{Kind: "Deployment", Namespace: "payments", Name: "checkout",
				ChangeType: "UPDATE", ChangeSummary: "image bumped", ChangedAt: &when}},
			Total: 1,
		},
	}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "changes")
	m = submit(t, m)

	out := m.View()
	for _, want := range []string{"Changes", "checkout", "UPDATE", "image bumped", "2h"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// Reliability views have a table but no resource rows, so a selection must resolve to
// nothing rather than to whatever object happens to sit at that index in a stale list.
func TestSelectionOnAReliabilityViewResolvesToNothing(t *testing.T) {
	c := &fakeClient{
		rows: pods(),
		risk: &api.WorkloadRisk{Workloads: []api.WorkloadRiskItem{
			{Namespace: "payments", Name: "checkout", Score: 90, Level: "CRITICAL"},
		}, MetricsAvailable: true, CVEScanAvailable: true, RuntimeAvailable: true, NetworkSignalsAvailable: true},
	}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "risk")
	m = submit(t, m)

	if _, ok := m.selected(); ok {
		t.Error("a risk row resolved to a Kubernetes resource")
	}
	m = key(m, "d")
	if c.detailName != "" {
		t.Errorf("a detail request was made from a risk row: %q", c.detailName)
	}
	if !strings.Contains(m.status, "no per-row detail") {
		t.Errorf("status = %q", m.status)
	}
}

// The :ns scope has no effect on cluster-wide reliability views, so it must not be sent.
func TestNamespaceScopeIsNotAppliedToClusterWideViews(t *testing.T) {
	c := &fakeClient{rows: pods(), risk: &api.WorkloadRisk{
		MetricsAvailable: true, CVEScanAvailable: true, RuntimeAvailable: true, NetworkSignalsAvailable: true}}
	m := newModel(t, c)

	m = key(m, ":")
	m = typeIn(m, "ns payments")
	m = submit(t, m)
	if c.lastNS != "payments" {
		t.Fatalf("namespace not applied to a resource view: %q", c.lastNS)
	}

	m = key(m, ":")
	m = typeIn(m, "risk")
	m = submit(t, m)
	// The risk fetch takes no namespace at all; the previous value must not leak into it.
	if m.namespace != "payments" {
		t.Errorf("the :ns scope was silently dropped from the model: %q", m.namespace)
	}
}

func TestLogsViewRenders(t *testing.T) {
	c := &fakeClient{
		rows: pods(),
		logs: &api.LogPage{
			Data: []api.LogRecord{{TS: "2026-08-30T10:00:00Z", Level: "ERROR",
				ServiceName: "checkout", Body: "connection refused"}},
			Total: 1,
		},
	}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "logs")
	m = submit(t, m)

	out := m.View()
	for _, want := range []string{"Logs", "ERROR", "checkout", "connection refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTracesViewRenders(t *testing.T) {
	c := &fakeClient{
		rows: pods(),
		traces: &api.TracePage{Data: []api.TraceRow{
			{TraceID: "abc", ServiceName: "gateway", Name: "GET /cart",
				DurationNano: 1_500_000_000, HasError: true},
		}},
	}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "traces")
	m = submit(t, m)

	out := m.View()
	for _, want := range []string{"Traces", "abc", "gateway", "1.50s"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// The logs view covers one hour; saying so on screen stops an empty result reading as
// "nothing ever logged" rather than "nothing in the last hour".
func TestLogsViewStatesItsWindow(t *testing.T) {
	c := &fakeClient{rows: pods(), logs: &api.LogPage{}}
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, "logs")
	m = submit(t, m)

	if !strings.Contains(m.View(), "last hour") {
		t.Errorf("logs view did not state its window:\n%s", m.View())
	}
}
