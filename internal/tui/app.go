// Package tui is the full-screen interactive surface.
//
// It is a renderer over internal/api and internal/view, never a second data path: every
// screen here has an exact flag-command equivalent, and both build their columns from the
// same definitions in internal/view.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/k8s"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeCommand
	modeDetail
	modeHelp
	modeAsk
)

// Options configures a session.
type Options struct {
	Client      Client
	OrgID       string
	ClusterID   string
	ClusterName string
	ContextName string
	APIURL      string
	Refresh     time.Duration
	InitialView string
}

// Model is the root Bubble Tea model.
type Model struct {
	opts Options

	width, height int

	view      viewDef
	namespace string
	// scope is the workload the drill-down keys act on. It survives a view switch on
	// purpose — that is what makes l, r, c and t a chain rather than four separate lookups.
	scope scope

	data    dataset
	visible []int // indices into the table's rows that survive the filter

	cursor, offset int

	mode   mode
	input  textinput.Model
	filter string

	// sortCol is -1 while the server's own ordering stands; `s` walks it across the
	// columns and `i` flips the direction.
	sortCol  int
	sortDesc bool
	wide     bool
	// shown is the table actually drawn — data.table, or its widened form. Cached rather
	// than rebuilt per frame: widening copies every row, and a 5000-row listing would pay
	// that on every keystroke.
	shown *render.Table

	// history is the trail of view names `[` and `]` walk, in visit order.
	history []string
	histPos int

	detail    *api.WorkloadDetail
	detailRef string
	// rowPane is the open detail of a non-resource row (dataset.rowText), drawn in the same
	// pane as a describe.
	rowPane string

	ask askState

	loading     bool
	paused      bool
	lastUpdated time.Time
	err         error
	status      string
}

type rowsMsg struct {
	data dataset
	err  error
}

type detailMsg struct {
	ref    string
	detail *api.WorkloadDetail
	err    error
}

type tickMsg time.Time

// New builds the model.
func New(opts Options) Model {
	if opts.Refresh <= 0 {
		opts.Refresh = 10 * time.Second
	}
	name := opts.InitialView
	if name == "" {
		name = "pods"
	}
	v, ok := views[name]
	if !ok {
		v = views["pods"]
	}

	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 120

	return Model{
		opts: opts, view: v, input: in, loading: true,
		sortCol: -1,
		history: []string{name}, histPos: 0,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetch(), m.tick())
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(m.opts.Refresh, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) fetch() tea.Cmd {
	o, v := m.opts, m.view
	req := fetchReq{Client: o.Client, OrgID: o.OrgID, ClusterID: o.ClusterID, Namespace: m.namespace}
	// A view that ignores the namespace scope must not be handed one, or the :ns filter
	// would appear to apply to screens it has no effect on. Same for the workload scope: a
	// screen that cannot narrow by it must not look as though it did.
	if !v.namespaced {
		req.Namespace = ""
	}
	if v.scopable {
		req.Scope = m.scope
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := v.fetch(ctx, req)
		return rowsMsg{data: d, err: err}
	}
}

func (m Model) fetchDetail(r k8s.Resource) tea.Cmd {
	o := m.opts
	ref := r.Namespace + "/" + r.Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := o.Client.Detail(ctx, o.OrgID, o.ClusterID, r.Namespace, r.Name, r.Kind)
		return detailMsg{ref: ref, detail: d, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		// A refresh under the cursor while a pane is open or the user is typing would move
		// the ground under them, so those states hold the current data.
		if m.paused || m.mode != modeNormal {
			return m, m.tick()
		}
		return m, tea.Batch(m.fetch(), m.tick())

	case rowsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.data = msg.data
		// The caveat travels with the data and is rendered from it; copying it into
		// status printed it twice.
		m.status = ""
		m.rebuildShown()
		m.applyFilter()
		m.lastUpdated = time.Now()
		return m, nil

	case detailMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.mode = modeNormal
			return m, nil
		}
		if msg.detail == nil || msg.detail.Workload == nil {
			m.status = "no workload detail for " + msg.ref
			m.mode = modeNormal
			return m, nil
		}
		m.detail, m.detailRef, m.mode = msg.detail, msg.ref, modeDetail
		return m, nil

	case askFrameMsg:
		if !msg.open {
			// The channel closed: the run is over one way or another.
			m.ask.busy = false
			return m, nil
		}
		m.ask.appendFrame(msg.frame)
		m.ask.offset = 0
		return m, waitFrame(m.ask.frames)

	case askAnswerMsg:
		m.ask.busy = false
		m.ask.appendAnswer(msg.answer, msg.err)
		m.ask.offset = 0
		// The run is over, so drop the subscription. Leaving it open holds an SSE
		// connection and a goroutine per question for as long as the pane is open — the
		// flag command stops listening here too.
		if m.ask.cancel != nil {
			m.ask.cancel()
			m.ask.cancel = nil
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The ask pane owns Ctrl-C while a run is live — there it means "stop the agent", the
	// same contract the flag command has — so it gets first refusal on every key.
	if m.mode == modeAsk {
		return m.handleAskKey(msg)
	}
	// Everywhere else Ctrl-C quits, whatever is focused.
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}

	switch m.mode {
	case modeFilter, modeCommand:
		return m.handleInputKey(msg)
	case modeHelp:
		m.mode = modeNormal
		return m, nil
	case modeDetail:
		switch msg.String() {
		case "esc", "q", "enter":
			m.mode, m.detail, m.rowPane = modeNormal, nil, ""
		}
		return m, nil
	}
	return m.handleNormalKey(msg)
}

func (m Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if m.mode == modeFilter {
			m.filter = ""
			m.applyFilter()
		}
		m.mode = modeNormal
		m.input.Blur()
		return m, nil

	case tea.KeyEnter:
		value := strings.TrimSpace(m.input.Value())
		wasCommand := m.mode == modeCommand
		m.mode = modeNormal
		m.input.Blur()
		if wasCommand {
			return m.runCommand(value)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.mode == modeFilter {
		// Filter as you type: waiting for enter to see the effect makes narrowing a guess.
		m.filter = m.input.Value()
		m.applyFilter()
	}
	return m, cmd
}

func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit

	case "?":
		m.mode = modeHelp
		return m, nil

	case "/":
		m.mode = modeFilter
		m.input.SetValue(m.filter)
		m.input.Focus()
		m.status = ""
		return m, textinput.Blink

	case ":":
		m.mode = modeCommand
		m.input.SetValue("")
		m.input.Focus()
		m.status = ""
		return m, textinput.Blink

	case "esc":
		// One step at a time, narrowest first: the filter is what was typed most recently,
		// so clearing both at once would throw away a scope the user is still working in.
		switch {
		case m.filter != "":
			m.filter = ""
			m.applyFilter()
			m.status = ""
			return m, nil
		case !m.scope.empty():
			m.scope = scope{}
			m.loading, m.status = true, "scope cleared"
			return m, m.fetch()
		}
		m.status = ""
		return m, nil

	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "pgup", "ctrl+b":
		m.moveCursor(-m.bodyHeight())
		return m, nil
	case "pgdown", "ctrl+f":
		m.moveCursor(m.bodyHeight())
		return m, nil
	case "home", "g":
		m.cursor, m.offset = 0, 0
		return m, nil
	case "end", "G":
		m.cursor = len(m.visible) - 1
		m.clampCursor()
		return m, nil

	case "p":
		m.paused = !m.paused
		m.status = map[bool]string{true: "auto-refresh paused", false: "auto-refresh resumed"}[m.paused]
		return m, nil

	case "ctrl+r":
		m.loading, m.status = true, "refreshing…"
		return m, m.fetch()

	case "enter", "d":
		r, ok := m.selected()
		if !ok {
			if text, ok := m.selectedRowText(); ok {
				m.rowPane, m.mode = text, modeDetail
				return m, nil
			}
			if m.data.table != nil && len(m.data.resources) == 0 {
				m.status = "this view has no per-row detail — press : to switch to a resource view"
			}
			return m, nil
		}
		// Detail exists for workload controllers only; saying so beats opening a blank pane.
		if !isDescribable(r.Kind) {
			m.status = fmt.Sprintf("no detail view for %s — describe covers Pod, Deployment, StatefulSet and DaemonSet", r.Kind)
			return m, nil
		}
		m.loading = true
		return m, m.fetchDetail(r)

	case "s":
		// Walk the columns, then off the end back to the server's order — which is a real
		// state, not a missing one, and has to be reachable again.
		m.cycleSort()
		return m, nil

	case "i":
		if m.sortCol < 0 {
			m.status = "nothing is sorted yet — press s to pick a column"
			return m, nil
		}
		m.sortDesc = !m.sortDesc
		m.applyFilter()
		return m, nil

	case "w":
		if m.data.table == nil || len(m.data.table.WideHeaders) == 0 {
			m.status = "this view has no extra columns"
			return m, nil
		}
		m.wide = !m.wide
		m.rebuildShown()
		// The widened table has more columns, so a sort pinned to an index still points at
		// the same column: the extras are appended, never inserted.
		m.applyFilter()
		return m, nil

	case "0":
		if m.namespace == "" {
			m.status = "already showing every namespace"
			return m, nil
		}
		m.namespace = ""
		m.loading, m.status = true, "showing all namespaces"
		return m, m.fetch()

	case "[":
		return m.stepHistory(-1)
	case "]":
		return m.stepHistory(1)

	case "l":
		return m.drillDown("logs")
	case "r":
		return m.drillDown("rca")
	case "c":
		return m.drillDown("changes")
	case "t":
		return m.drillDown("traces")

	case "a":
		return m.openAsk()
	}
	return m, nil
}

func (m Model) runCommand(raw string) (tea.Model, tea.Cmd) {
	cmd := strings.TrimSpace(strings.TrimPrefix(raw, ":"))
	if cmd == "" {
		return m, nil
	}

	name, arg, _ := strings.Cut(cmd, " ")
	name = strings.ToLower(strings.TrimSpace(name))
	arg = strings.TrimSpace(arg)

	switch name {
	case "q", "quit", "exit":
		return m, tea.Quit
	case "ns", "namespace":
		// ":ns" with no argument clears back to every namespace.
		m.namespace = arg
		if arg == "" {
			m.status = "showing all namespaces"
		} else {
			m.status = "namespace: " + arg
		}
		m.loading = true
		return m, m.fetch()
	}

	if _, ok := views[name]; ok {
		return m.switchView(name)
	}
	if milestone, planned := plannedViews[name]; planned {
		m.status = fmt.Sprintf("%q is not a view here — see %s", name, milestone)
		return m, nil
	}
	m.status = fmt.Sprintf("unknown command %q — press ? for the list", name)
	return m, nil
}

// drillDown scopes to the row under the cursor and opens a view about it. On a screen with
// no resource behind the row it opens the view unscoped and says so, rather than silently
// showing the whole cluster under a heading that reads like one workload.
func (m Model) drillDown(name string) (tea.Model, tea.Cmd) {
	if r, ok := m.selected(); ok {
		m.scope = scopeOf(r)
	} else if m.scope.empty() {
		m.status = "nothing selected — " + name + " is cluster-wide"
	}
	return m.switchView(name)
}

// switchView loads a view by name and records the move so `[` can undo it.
func (m Model) switchView(name string) (tea.Model, tea.Cmd) {
	v, ok := views[name]
	if !ok {
		m.status = fmt.Sprintf("unknown view %q", name)
		return m, nil
	}
	if m.histPos < len(m.history)-1 {
		// Moving somewhere new from the middle of the trail abandons the forward branch,
		// the way a browser does.
		m.history = m.history[:m.histPos+1]
	}
	if len(m.history) == 0 || m.history[m.histPos] != name {
		m.history = append(m.history, name)
		m.histPos = len(m.history) - 1
	}
	return m.loadView(v)
}

// loadView resets everything that belongs to the outgoing screen. The sort is one of them:
// column 3 means something different on every view, so carrying it over would silently
// order the new table by whatever happens to sit there.
func (m Model) loadView(v viewDef) (tea.Model, tea.Cmd) {
	m.view = v
	m.cursor, m.offset, m.filter = 0, 0, ""
	m.sortCol, m.sortDesc, m.wide = -1, false, false
	m.shown = nil
	m.loading, m.status = true, ""
	return m, m.fetch()
}

func (m Model) stepHistory(delta int) (tea.Model, tea.Cmd) {
	next := m.histPos + delta
	if next < 0 || next >= len(m.history) {
		if delta < 0 {
			m.status = "no earlier view"
		} else {
			m.status = "no later view"
		}
		return m, nil
	}
	m.histPos = next
	return m.loadView(views[m.history[next]])
}

// cycleSort advances the sort one step: unsorted → column 0 → … → last column → unsorted.
func (m *Model) cycleSort() {
	t := m.activeTable()
	if t == nil || len(t.Headers) == 0 {
		return
	}
	m.sortCol++
	if m.sortCol >= len(t.Headers) {
		m.sortCol = -1
		m.sortDesc = false
		m.status = "sorted by the server's own order"
	} else {
		m.status = "sorted by " + strings.ToLower(t.Headers[m.sortCol])
	}
	m.applyFilter()
}

// isDescribable names the kinds the detail pane can render. The backend's
// /workloads/{ns}/{name}/detail takes any kind — it looks the row up by kind/namespace/name
// — so this list is about having a renderer, not about what the API allows.
func isDescribable(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Pod":
		return true
	}
	return false
}

// applyFilter recomputes which rows survive, matching against the rendered cells so what is
// typed matches what is on screen.
func (m *Model) applyFilter() {
	m.visible = m.visible[:0]
	if m.data.table == nil {
		return
	}
	needle := strings.ToLower(strings.TrimSpace(m.filter))
	for i, row := range m.data.table.Rows {
		if needle == "" || strings.Contains(strings.ToLower(strings.Join(row, " ")), needle) {
			m.visible = append(m.visible, i)
		}
	}
	if m.sortCol >= 0 {
		sortRows(m.activeTable().Rows, m.visible, m.sortCol, m.sortDesc)
	}
	m.clampCursor()
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
}

func (m *Model) clampCursor() {
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.bodyHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// selected returns the resource under the cursor.
func (m Model) selected() (k8s.Resource, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return k8s.Resource{}, false
	}
	idx := m.visible[m.cursor]
	// Reliability views have a table but no resource rows behind it, so a selection there
	// resolves to nothing rather than to the wrong object.
	if idx < 0 || idx >= len(m.data.resources) {
		return k8s.Resource{}, false
	}
	return m.data.resources[idx], true
}

// selectedRowText is the pre-rendered detail of the row under the cursor, on screens whose
// rows carry one. The visible index points into the table's rows, which rowText mirrors.
func (m Model) selectedRowText() (string, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return "", false
	}
	idx := m.visible[m.cursor]
	if idx < 0 || idx >= len(m.data.rowText) {
		return "", false
	}
	return m.data.rowText[idx], true
}

// bodyHeight is the row budget: the window minus everything the chrome occupies. It is
// derived from the same pieces View draws, so the two cannot disagree about how many rows
// fit — a disagreement shows up as a clipped last row or a scroll that stops early.
func (m Model) bodyHeight() int {
	if m.height <= 0 {
		return 20
	}
	chrome := 1 + // tab strip
		1 + // column header inside the table panel
		2 + // the table panel's own border
		2 // hint line + status line
	if m.contextPanelShown() {
		chrome += panelHeight(contextPanelLines, m.screenWidth())
	}
	h := m.height - chrome
	if h < 1 {
		return 1
	}
	return h
}

// contextPanelLines is the height of the context panel's body — seven facts, and the key
// columns are never taller than that.
const contextPanelLines = 7

func (m Model) contextPanelShown() bool { return m.height <= 0 || m.height >= 20 }

// activeTable is what gets drawn: the view's table, widened when `w` is on.
func (m Model) activeTable() *render.Table {
	if m.shown != nil {
		return m.shown
	}
	return m.data.table
}

// rebuildShown recomputes the drawn table after the data or the wide toggle changes.
func (m *Model) rebuildShown() {
	if m.wide {
		m.shown = m.data.table.Widened()
		return
	}
	m.shown = m.data.table
}
