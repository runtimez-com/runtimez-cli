package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

func newAskCmd() *cobra.Command {
	var sessionID string
	var newSession bool
	var quiet bool

	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask the SRE agent about this cluster",
		Long: `Put a question to the investigating agent and watch it work.

The agent's reasoning streams as it runs — each step names the tool it is about to use and
what that tool returned — so a long investigation shows progress rather than a blank prompt.
Ctrl-C cancels the run server-side, not just locally.`,
		Example: `  rtz ask "why is checkout returning 5xx"
  rtz ask "what changed in payments today" --new
  rtz ask "and the memory limits?" --session s-1a2b`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			question := strings.Join(args, " ")

			// Interrupt must reach the server: a run left going costs tokens and holds a
			// slot long after the person walked away.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			streamID := api.NewStreamID()
			out := cmd.OutOrStdout()

			// Subscribe before asking. The reverse order races the first frames, and the
			// steps that get lost are the early ones that explain what the agent decided
			// to look at.
			frames := make(chan api.StreamFrame, 64)
			streamDone := make(chan struct{})
			go func() {
				defer close(streamDone)
				defer close(frames)
				_ = e.client.StreamInvestigation(ctx, orgID, clusterID, streamID, func(f api.StreamFrame) error {
					select {
					case frames <- f:
					case <-ctx.Done():
						return ctx.Err()
					}
					return nil
				})
			}()

			type result struct {
				answer *api.AgentAnswer
				err    error
			}
			answers := make(chan result, 1)
			go func() {
				a, err := e.client.Ask(ctx, orgID, clusterID, api.AgentQuery{
					Query:        question,
					SessionID:    sessionID,
					StartSession: newSession || sessionID == "",
					StreamID:     streamID,
				})
				answers <- result{a, err}
			}()

			showProgress := !quiet && !e.printer.Format.Structured()
			for {
				select {
				case f, ok := <-frames:
					if !ok {
						frames = nil
						continue
					}
					if showProgress {
						renderFrame(out, f)
					}
				case r := <-answers:
					stop()
					if r.err != nil {
						if errors.Is(r.err, context.Canceled) {
							return cancelRun(cmd, e, orgID, clusterID, streamID)
						}
						return r.err
					}
					<-drain(streamDone, 500*time.Millisecond)
					if e.printer.Format.Structured() {
						return e.printer.Print(r.answer, nil)
					}
					printAnswer(cmd, r.answer, showProgress)
					return nil
				case <-ctx.Done():
					return cancelRun(cmd, e, orgID, clusterID, streamID)
				}
			}
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "continue an existing conversation")
	cmd.Flags().BoolVar(&newSession, "new", false, "start a new conversation")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress the live reasoning, print only the answer")
	cmd.AddCommand(newSessionsCmd(), newPromptsCmd())
	return cmd
}

// drain waits briefly for the stream goroutine so trailing frames land before the answer,
// without letting a stuck stream hold the command open.
func drain(done <-chan struct{}, wait time.Duration) <-chan struct{} {
	out := make(chan struct{})
	go func() {
		defer close(out)
		select {
		case <-done:
		case <-time.After(wait):
		}
	}()
	return out
}

func cancelRun(cmd *cobra.Command, e *env, orgID, clusterID, streamID string) error {
	fmt.Fprintln(cmd.ErrOrStderr(), "\ncancelling the investigation…")
	// The command's context is already cancelled, so the cancel call needs its own.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.client.CancelInvestigation(ctx, orgID, clusterID, streamID); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "could not cancel server-side: %v\n", err)
	}
	return &ExitError{Code: ExitFailure, Err: errors.New("cancelled")}
}

// renderFrame prints one streamed step.
func renderFrame(out io.Writer, f api.StreamFrame) {
	switch f.Type {
	case "step":
		switch f.Phase {
		case "thinking":
			if f.Thought != "" {
				fmt.Fprintf(out, "  %d. %s\n", f.Step, truncateLine(f.Thought, 100))
			}
			if f.Tool != "" {
				fmt.Fprintf(out, "     → %s %s\n", f.Tool, truncateLine(f.Args, 60))
			}
		case "observation":
			if f.ResultSummary != "" {
				fmt.Fprintf(out, "     ← %s\n", truncateLine(f.ResultSummary, 100))
			}
		}
	case "error":
		fmt.Fprintf(out, "  ! %s\n", f.Message)
	case "cancelled":
		fmt.Fprintln(out, "  cancelled")
	}
}

func printAnswer(cmd *cobra.Command, a *api.AgentAnswer, hadProgress bool) {
	out := cmd.OutOrStdout()
	if a == nil {
		fmt.Fprintln(out, "No answer was returned.")
		return
	}
	if hadProgress {
		fmt.Fprintln(out)
	}

	if v := a.Verdict; v != nil && v.Headline != "" {
		fmt.Fprintln(out, v.Headline)
		if v.Workload != "" {
			fmt.Fprintf(out, "  workload: %s\n", v.Workload)
		}
		if v.BlastRadius != "" {
			fmt.Fprintf(out, "  blast radius: %s\n", v.BlastRadius)
		}
		// Confidence is HIGH/MEDIUM/LOW. It is never a percentage, whatever a mockup showed.
		if v.Confidence != "" {
			fmt.Fprintf(out, "  confidence: %s\n", v.Confidence)
		}
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, a.Answer)

	if len(a.SuggestedCommands) > 0 {
		fmt.Fprintln(out, "\nSuggested next steps")
		for _, s := range a.SuggestedCommands {
			fmt.Fprintf(out, "  %s\n    %s\n", s.Label, s.Command)
		}
	}

	var meta []string
	if a.SessionID != "" {
		meta = append(meta, "session "+a.SessionID+" (continue with --session "+a.SessionID+")")
	}
	if len(a.ToolsUsed) > 0 {
		meta = append(meta, fmt.Sprintf("%d tool(s)", len(a.ToolsUsed)))
	}
	if a.TokenUsage != nil {
		meta = append(meta, fmt.Sprintf("tokens %d in / %d out",
			a.TokenUsage.InputTokens, a.TokenUsage.OutputTokens))
	}
	if len(meta) > 0 {
		fmt.Fprintf(out, "\n%s\n", strings.Join(meta, " · "))
	}
	// A truncated investigation must say so: the answer reads complete either way.
	if a.HitStepLimit {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: the agent hit its step limit — this answer is based on a PARTIAL investigation")
	}
}

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sessions",
		Aliases: []string{"session"},
		Short:   "Saved conversations with the agent",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			list, err := e.client.Sessions(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			t := &render.Table{
				Headers:      []string{"SESSION", "TITLE", "TURNS", "UPDATED"},
				EmptyMessage: "No saved conversations.",
			}
			for _, s := range list {
				updated := "<unknown>"
				if s.UpdatedAt != nil {
					updated = ageSince(*s.UpdatedAt) + " ago"
				}
				t.Rows = append(t.Rows, []string{
					s.SessionID, render.Dash(s.Title), fmt.Sprint(s.TurnCount), updated,
				})
			}
			return e.printer.Print(list, t)
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show <session-id>",
		Short: "Print a conversation's transcript",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			tr, err := e.client.Transcript(cmd.Context(), orgID, clusterID, args[0])
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(tr, nil)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s — %s\n", tr.SessionID, render.Dash(tr.Title))
			for _, turn := range tr.Turns {
				fmt.Fprintf(out, "\n> %s\n\n%s\n", turn.Question, turn.Answer)
			}
			return nil
		},
	})
	return cmd
}

func newPromptsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompts",
		Short: "Curated starter questions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			entries, err := e.client.PromptLibrary(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			t := &render.Table{
				Headers:      []string{"CATEGORY", "TITLE", "PROMPT"},
				EmptyMessage: "No prompts published.",
			}
			for _, p := range entries {
				t.Rows = append(t.Rows, []string{
					render.Dash(p.Category), p.Title, truncateLine(p.Prompt, 70),
				})
			}
			return e.printer.Print(entries, t)
		},
	}
}
