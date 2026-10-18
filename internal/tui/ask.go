package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/runtimez-com/runtimez-cli/internal/api"
)

// The ask pane is the agent's investigation, live. It matters that it streams: an agent
// that thinks for forty seconds behind a blank screen is indistinguishable from one that
// has hung, and the early steps — which tool it reached for first — are usually the most
// informative part of the run.

// askState is the conversation pane.
type askState struct {
	input     textinput.Model
	lines     []string
	busy      bool
	streamID  string
	sessionID string
	// cancel stops the local subscription. The server-side run is stopped separately, via
	// CancelInvestigation: dropping the connection alone leaves the agent burning a slot.
	cancel context.CancelFunc
	frames chan api.StreamFrame
	offset int
}

type askFrameMsg struct {
	frame api.StreamFrame
	open  bool
}

type askAnswerMsg struct {
	answer *api.AgentAnswer
	err    error
}

// openAsk starts a conversation, seeded with whatever is selected.
func (m Model) openAsk() (tea.Model, tea.Cmd) {
	if r, ok := m.selected(); ok {
		m.scope = scopeOf(r)
	}

	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 400
	if !m.scope.empty() {
		in.SetValue("What is wrong with " + m.scope.ref() + "?")
	}
	in.Focus()

	m.ask = askState{input: in}
	m.mode = modeAsk
	if m.scope.empty() {
		m.ask.lines = []string{metaStyle.Render("Ask about this cluster. Select a row first to ask about one workload.")}
	} else {
		m.ask.lines = []string{metaStyle.Render("Asking about " + m.scope.ref() + " — edit the question, then enter.")}
	}
	return m, textinput.Blink
}

// submitAsk subscribes to the stream and then asks, in that order. The reverse races the
// first frames, and the steps that get lost are the early ones that explain what the agent
// decided to look at.
func (m Model) submitAsk() (tea.Model, tea.Cmd) {
	question := strings.TrimSpace(m.ask.input.Value())
	if question == "" || m.ask.busy {
		return m, nil
	}

	o := m.opts
	streamID := api.NewStreamID()
	ctx, cancel := context.WithCancel(context.Background())
	frames := make(chan api.StreamFrame, 64)

	m.ask.busy = true
	m.ask.streamID = streamID
	m.ask.cancel = cancel
	m.ask.frames = frames
	m.ask.lines = append(m.ask.lines, "", keyStyle.Render("› ")+question)
	m.ask.input.SetValue("")

	sessionID := m.ask.sessionID
	go func() {
		defer close(frames)
		_ = o.Client.StreamInvestigation(ctx, o.OrgID, o.ClusterID, streamID, func(f api.StreamFrame) error {
			select {
			case frames <- f:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		})
	}()

	askCmd := func() tea.Msg {
		a, err := o.Client.Ask(ctx, o.OrgID, o.ClusterID, api.AgentQuery{
			Query:        question,
			SessionID:    sessionID,
			StartSession: sessionID == "",
			StreamID:     streamID,
		})
		return askAnswerMsg{answer: a, err: err}
	}
	return m, tea.Batch(waitFrame(frames), askCmd)
}

// waitFrame turns the next frame on the channel into a message. Re-issued on every frame,
// which is how a Bubble Tea model consumes a stream without blocking its update loop.
func waitFrame(ch chan api.StreamFrame) tea.Cmd {
	return func() tea.Msg {
		f, open := <-ch
		return askFrameMsg{frame: f, open: open}
	}
}

// cancelAsk stops the run on the server as well as locally.
func (m Model) cancelAsk() tea.Cmd {
	if m.ask.cancel != nil {
		m.ask.cancel()
	}
	o, streamID := m.opts, m.ask.streamID
	if streamID == "" {
		return nil
	}
	return func() tea.Msg {
		// A fresh context: the one that was just cancelled cannot carry the cancel call.
		ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = o.Client.CancelInvestigation(ctx, o.OrgID, o.ClusterID, streamID)
		return nil
	}
}

func (m Model) handleAskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		// While a run is live, Ctrl-C means "stop the agent", not "quit the tool" — the
		// same contract `rtz ask` has. A second press quits.
		if m.ask.busy {
			cmd := m.cancelAsk()
			m.ask.busy = false
			m.ask.lines = append(m.ask.lines, warnStyle.Render("  cancelled"))
			return m, cmd
		}
		return m, tea.Quit

	case tea.KeyEsc:
		var cmd tea.Cmd
		if m.ask.busy {
			cmd = m.cancelAsk()
		}
		m.mode = modeNormal
		m.ask = askState{}
		return m, cmd

	case tea.KeyEnter:
		return m.submitAsk()

	case tea.KeyPgUp:
		m.ask.offset -= 5
		if m.ask.offset < 0 {
			m.ask.offset = 0
		}
		return m, nil

	case tea.KeyPgDown:
		m.ask.offset += 5
		return m, nil
	}

	var cmd tea.Cmd
	m.ask.input, cmd = m.ask.input.Update(msg)
	return m, cmd
}

// appendFrame renders one streamed frame into the transcript.
func (a *askState) appendFrame(f api.StreamFrame) {
	switch f.Type {
	case "step":
		switch f.Phase {
		case "thinking":
			if f.Thought != "" {
				a.lines = append(a.lines, fmt.Sprintf("  %d. %s", f.Step, f.Thought))
			}
			if f.Tool != "" {
				a.lines = append(a.lines, metaStyle.Render("     → "+f.Tool+" "+truncateAsk(f.Args, 60)))
			}
		case "observation":
			if f.ResultSummary != "" {
				a.lines = append(a.lines, metaStyle.Render("     ← "+truncateAsk(f.ResultSummary, 100)))
			}
		}
	case "error":
		a.lines = append(a.lines, errStyle.Render("  ! "+f.Message))
	case "cancelled":
		a.lines = append(a.lines, warnStyle.Render("  cancelled"))
	}
}

// appendAnswer renders the finished investigation.
func (a *askState) appendAnswer(ans *api.AgentAnswer, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return // the cancel line is already there
	case err != nil:
		a.lines = append(a.lines, errStyle.Render("  error: "+err.Error()))
		return
	case ans == nil:
		a.lines = append(a.lines, metaStyle.Render("  no answer was returned"))
		return
	}

	if ans.SessionID != "" {
		a.sessionID = ans.SessionID
	}
	a.lines = append(a.lines, "")
	if v := ans.Verdict; v != nil && v.Headline != "" {
		a.lines = append(a.lines, headerStyle.Render(v.Headline))
		if v.Confidence != "" {
			// Confidence is HIGH/MEDIUM/LOW, never a percentage — rendering it as one
			// would invent a precision the agent never claimed.
			a.lines = append(a.lines, metaStyle.Render("  confidence "+v.Confidence))
		}
	}
	for _, line := range strings.Split(strings.TrimRight(ans.Answer, "\n"), "\n") {
		a.lines = append(a.lines, line)
	}
	if ans.HitStepLimit {
		a.lines = append(a.lines,
			warnStyle.Render("  the agent hit its step limit — this answer is partial"))
	}
}

func truncateAsk(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// askView draws the transcript with the prompt pinned to the bottom.
func (m Model) askView() string {
	height := m.bodyHeight()
	if height < 3 {
		height = 3
	}
	body := height - 2 // one line for the prompt, one for the separator

	lines := m.ask.lines
	start := len(lines) - body - m.ask.offset
	if start < 0 {
		start = 0
	}
	end := start + body
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for _, l := range lines[start:end] {
		b.WriteString(l + "\n")
	}
	for i := end - start; i < body; i++ {
		b.WriteByte('\n')
	}

	prompt := keyStyle.Render("› ") + m.ask.input.View()
	if m.ask.busy {
		prompt = "  " + m.spin.View() + metaStyle.Render(" working… ") + warnStyle.Render("ctrl+c") + metaStyle.Render(" stops the agent")
	}
	b.WriteString(prompt)

	title := "ask"
	if !m.scope.empty() {
		title += " · " + m.scope.ref()
	}
	return m.tabStrip() + "\n" +
		panel(title, b.String(), m.screenWidth()) + "\n" +
		footerStyle.Render(" "+keyStyle.Render("enter")+" send   "+
			keyStyle.Render("ctrl+c")+" stop   "+
			keyStyle.Render("esc")+" close   "+
			keyStyle.Render("pgup/pgdn")+" scroll")
}
