package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/runtimez-com/runtimez-cli/internal/api/stream"
)

func agentPath(orgID, clusterID, suffix string) string {
	return fmt.Sprintf("/eac/api/1.0/orgs/%s/clusters/%s/agent%s", orgID, clusterID, suffix)
}

// NewStreamID mints the id that ties a query to its event stream. The client generates it
// so the stream can be subscribed BEFORE the query starts — subscribing afterwards races
// the first frames.
func NewStreamID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// A time-based fallback is still unique enough for a per-invocation stream key.
		return fmt.Sprintf("rtz-%d", time.Now().UnixNano())
	}
	return "rtz-" + hex.EncodeToString(b)
}

// AgentQuery is the investigation request.
type AgentQuery struct {
	Query        string `json:"query"`
	SessionID    string `json:"sessionId,omitempty"`
	StartSession bool   `json:"startSession,omitempty"`
	StreamID     string `json:"streamId,omitempty"`
}

// AgentAnswer is the completed investigation.
type AgentAnswer struct {
	Answer       string        `json:"answer"`
	Steps        []AgentStep   `json:"steps"`
	ToolsUsed    []string      `json:"toolsUsed"`
	SessionID    string        `json:"sessionId"`
	TurnSeq      int           `json:"turnSeq"`
	HitStepLimit bool          `json:"hitStepLimit"`
	Verdict      *AgentVerdict `json:"verdict"`
	TokenUsage   *struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
	} `json:"tokenUsage"`
	SuggestedCommands []SuggestedCommand `json:"suggestedCommands"`
}

// AgentVerdict is the structured headline. Confidence is HIGH/MEDIUM/LOW — never a
// percentage, whatever a mockup may have shown.
type AgentVerdict struct {
	Headline    string `json:"headline"`
	Workload    string `json:"workload"`
	Subtitle    string `json:"subtitle"`
	BlastRadius string `json:"blastRadius"`
	Confidence  string `json:"confidence"`
}

// AgentStep is one reasoning step in the completed answer.
type AgentStep struct {
	Step   int    `json:"step"`
	Tool   string `json:"tool"`
	Result string `json:"result"`
}

// SuggestedCommand is a follow-up the agent proposes.
type SuggestedCommand struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

// StreamFrame is one decoded event from an investigation stream.
//
// The frame types are step, done, cancelled and error. A "thinking" step carries the
// thought and the tool about to run; an "observation" step carries that tool's result.
type StreamFrame struct {
	Type          string
	Step          int    `json:"step"`
	Phase         string `json:"phase"`
	Thought       string `json:"thought"`
	Tool          string `json:"tool"`
	Args          string `json:"args"`
	ResultSummary string `json:"resultSummary"`
	Message       string `json:"message"`
}

// StreamInvestigation subscribes to a run's events and calls fn for each frame.
//
// Call it before Ask so no frame is missed, and cancel ctx to stop listening.
func (c *Client) StreamInvestigation(ctx context.Context, orgID, clusterID, streamID string, fn func(StreamFrame) error) error {
	u := c.URL(agentPath(orgID, clusterID, "/stream/"+url.PathEscape(streamID)), nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if bearer := c.Creds.Bearer(); bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	// The stream stays open for the length of an investigation, so it must not inherit the
	// per-request timeout that bounds ordinary calls.
	streamClient := &http.Client{Transport: c.HTTP.Transport}

	return stream.Open(ctx, streamClient, req, func(ev stream.Event) error {
		frame := StreamFrame{Type: ev.Name}
		if ev.Data != "" && ev.Data != "{}" {
			// A frame this client does not understand is skipped rather than fatal: the
			// backend may add fields, and dropping the investigation over one is worse.
			_ = json.Unmarshal([]byte(ev.Data), &frame)
		}
		if err := fn(frame); err != nil {
			return err
		}
		switch ev.Name {
		case "done", "cancelled", "error":
			return stream.ErrStopped
		}
		return nil
	})
}

// Ask runs an investigation and returns the completed answer.
func (c *Client) Ask(ctx context.Context, orgID, clusterID string, q AgentQuery) (*AgentAnswer, error) {
	out, err := Post[AgentAnswer](ctx, c, agentPath(orgID, clusterID, "/query"), nil, q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelInvestigation stops a running investigation server-side.
func (c *Client) CancelInvestigation(ctx context.Context, orgID, clusterID, streamID string) error {
	_, err := c.Do(ctx, http.MethodPost,
		agentPath(orgID, clusterID, "/stream/"+url.PathEscape(streamID)+"/cancel"), nil, map[string]any{})
	return err
}

// PromptLibraryEntry is a starter question.
type PromptLibraryEntry struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
}

// PromptLibrary lists the curated starter questions.
func (c *Client) PromptLibrary(ctx context.Context, orgID, clusterID string) ([]PromptLibraryEntry, error) {
	return Get[[]PromptLibraryEntry](ctx, c, agentPath(orgID, clusterID, "/prompt-library"), nil)
}

// AgentSession is one saved conversation.
type AgentSession struct {
	SessionID string     `json:"sessionId"`
	Title     string     `json:"title"`
	TurnCount int        `json:"turnCount"`
	CreatedAt *time.Time `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// Sessions lists saved conversations for the cluster.
func (c *Client) Sessions(ctx context.Context, orgID, clusterID string) ([]AgentSession, error) {
	return Get[[]AgentSession](ctx, c, agentPath(orgID, clusterID, "/sessions"), nil)
}

// Transcript is a saved conversation's turns.
type Transcript struct {
	SessionID string           `json:"sessionId"`
	Title     string           `json:"title"`
	Turns     []TranscriptTurn `json:"turns"`
}

// TranscriptTurn is one question and its answer.
type TranscriptTurn struct {
	Seq      int        `json:"seq"`
	Question string     `json:"question"`
	Answer   string     `json:"answer"`
	AskedAt  *time.Time `json:"askedAt"`
}

// Transcript fetches one conversation.
func (c *Client) Transcript(ctx context.Context, orgID, clusterID, sessionID string) (*Transcript, error) {
	out, err := Get[Transcript](ctx, c, agentPath(orgID, clusterID, "/sessions/"+url.PathEscape(sessionID)), nil)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
