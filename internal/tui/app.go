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
)

type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeCommand
	modeDetail
	modeHelp
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

	data    dataset
	visible []int // indices into the table's rows that survive the filter

	cursor, offset int

	mode   mode
	input  textinput.Model
	filter string

	detail    *api.WorkloadDetail
	detailRef string

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

	return Model{opts: opts, view: v, input: in, loading: true}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetch(), m.tick())
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(m.opts.Refresh, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) fetch() tea.Cmd {
	o, v, ns := m.opts, m.view, m.namespace
	// A view that ignores the namespace scope must not be handed one, or the :ns filter
	// would appear to apply to screens it has no effect on.
	if !v.namespaced {
		ns = ""
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := v.fetch(ctx, o.Client, o.OrgID, o.ClusterID, ns)
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
		m.status = msg.data.notice
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

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl-C always quits, whatever is focused.
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
			m.mode, m.detail = modeNormal, nil
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
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
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
			if m.data.table != nil && len(m.data.resources) == 0 {
				m.status = "this view has no per-row detail — press : to switch to a resource view"
			}
			return m, nil
		}
		// Detail exists for workload controllers only; saying so beats opening a blank pane.
		if !isDescribable(r.Kind) {
			m.status = fmt.Sprintf("no detail view for %s — describe covers Deployment, StatefulSet and DaemonSet", r.Kind)
			return m, nil
		}
		m.loading = true
		return m, m.fetchDetail(r)

	case "l", "r", "a":
		// Reserved in the roadmap; claiming the key now while doing nothing would read as a
		// broken binding.
		m.status = fmt.Sprintf("%q arrives with logs, risk and ask in a later milestone", msg.String())
		return m, nil
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

	if v, ok := views[name]; ok {
		m.view = v
		m.cursor, m.offset, m.filter = 0, 0, ""
		m.loading, m.status = true, ""
		return m, m.fetch()
	}
	if milestone, planned := plannedViews[name]; planned {
		m.status = fmt.Sprintf("%q is not a view here — see %s", name, milestone)
		return m, nil
	}
	m.status = fmt.Sprintf("unknown command %q — press ? for the list", name)
	return m, nil
}

func isDescribable(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet":
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

// bodyHeight is the row budget: total minus header block, column header and footer.
func (m Model) bodyHeight() int {
	h := m.height - 6
	if h < 1 {
		return 1
	}
	return h
}
