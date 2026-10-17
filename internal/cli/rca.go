package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

// clusterEnv resolves the env plus a required org and cluster — the preamble every
// cluster-scoped command shares.
func clusterEnv(cmd *cobra.Command) (*env, string, string, error) {
	e, err := loadEnv(cmd, true)
	if err != nil {
		return nil, "", "", err
	}
	orgID, err := e.requireOrg()
	if err != nil {
		return nil, "", "", err
	}
	clusterID, err := e.requireCluster()
	if err != nil {
		return nil, "", "", err
	}
	return e, orgID, clusterID, nil
}

func scanStamp(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.Format(time.RFC3339) + " (" + ageSince(*t) + " ago)"
}

func newRcaCmd() *cobra.Command {
	var from, to string

	cmd := &cobra.Command{
		Use:   "rca [namespace/name]",
		Short: "What is broken, and why",
		Long: `With no argument, asks the cluster what is currently degraded and explains the
riskiest workload. With a workload reference, shows that workload's evidence bundle:
termination reason, restarts, warning events, log tail, in-flight spans and dependencies.`,
		Example: `  rtz rca
  rtz rca payments/checkout
  rtz rca explain payments/checkout`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				return runWorkloadRCA(cmd, e, orgID, clusterID, args[0])
			}

			auto, err := e.client.AutoRCA(cmd.Context(), orgID, clusterID, from, to)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(auto, nil)
			}

			out := cmd.OutOrStdout()
			// degraded=false leaves every other field null, so this branch is the whole
			// answer — rendering an empty suspect below would invent a problem.
			if !auto.Degraded {
				msg := auto.Message
				if msg == "" {
					msg = "No degraded workloads in this window."
				}
				fmt.Fprintln(out, msg)
				return nil
			}

			if s := auto.TopSuspect; s != nil {
				fmt.Fprintf(out, "%s/%s (%s)\n", s.Namespace, s.Name, render.Dash(s.Kind))
				fmt.Fprintf(out, "  Risk: %d/100 %s\n", s.Score, render.Dash(s.Level))
				for _, r := range s.Reasons {
					fmt.Fprintf(out, "  · [%s] %s\n", render.Dash(r.Severity), r.Title)
				}
				fmt.Fprintln(out)
			}
			printExplain(cmd, auto.Explanation)
			return nil
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "window start (ISO-8601)")
	cmd.Flags().StringVar(&to, "to", "", "window end (ISO-8601)")
	cmd.AddCommand(newRcaExplainCmd())
	return cmd
}

func runWorkloadRCA(cmd *cobra.Command, e *env, orgID, clusterID, ref string) error {
	ns, name, err := splitWorkloadRef(ref, "")
	if err != nil {
		return err
	}
	r, err := e.client.WorkloadRCA(cmd.Context(), orgID, clusterID, ns, name, "")
	if err != nil {
		return err
	}
	if e.printer.Format.Structured() {
		return e.printer.Print(r, nil)
	}

	view.WorkloadRCA(cmd.OutOrStdout(), r)
	return nil
}

func newRcaExplainCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "explain <namespace>/<name>",
		Short: "Narrative root-cause analysis for one workload",
		Long: `Asks the backend to synthesise a root cause from the evidence bundle.

Results are cached. --force bypasses the cache, which means a fresh model call and fresh
token cost, so it is never the default.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			ns, name, err := splitWorkloadRef(args[0], "")
			if err != nil {
				return err
			}

			ex, err := e.client.ExplainRCA(cmd.Context(), orgID, clusterID, ns, name, "", force)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(ex, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s/%s\n\n", ns, name)
			printExplain(cmd, ex)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "bypass the result cache (costs a fresh model call)")
	return cmd
}

// printExplain renders the narrative plus the provenance that says how much to trust it.
func printExplain(cmd *cobra.Command, ex *api.RCAExplain) {
	out := cmd.OutOrStdout()
	if ex == nil {
		fmt.Fprintln(out, "No explanation was produced.")
		return
	}

	if ex.Summary != "" {
		fmt.Fprintln(out, ex.Summary)
	}
	if len(ex.CausalChain) > 0 {
		fmt.Fprintln(out, "\nWhat happened")
		for _, s := range ex.CausalChain {
			role := strings.ToLower(strings.ReplaceAll(s.Role, "_", " "))
			fmt.Fprintf(out, "  %d. [%s] %s\n", s.Step, render.Dash(role), s.Description)
		}
	}
	if len(ex.RankedCauses) > 0 {
		fmt.Fprintln(out, "\nProbable causes")
		for i, c := range ex.RankedCauses {
			fmt.Fprintf(out, "  %d. %s (%s)\n", i+1, c.Cause, render.Dash(c.Confidence))
		}
	}
	if ex.WhatChanged != "" {
		fmt.Fprintf(out, "\nWhat changed\n  %s\n", ex.WhatChanged)
	}
	if ex.Recommendation != "" {
		fmt.Fprintf(out, "\nRecommendation\n  %s\n", ex.Recommendation)
	}

	// Provenance is part of the answer: a deterministic gate result and a model-written
	// narrative deserve different levels of trust, and the reader cannot tell them apart
	// from the prose alone.
	source := "deterministic (no model call)"
	switch {
	case ex.LLMUsed && ex.FromCache:
		source = "model, replayed from cache"
	case ex.LLMUsed:
		source = "model " + render.Dash(ex.ModelID)
	}
	fmt.Fprintf(out, "\nSource: %s", source)
	if ex.GateReason != "" {
		fmt.Fprintf(out, " · gate: %s", ex.GateReason)
	}
	if ex.TokenUsage != nil {
		fmt.Fprintf(out, " · tokens: %d in / %d out", ex.TokenUsage.InputTokens, ex.TokenUsage.OutputTokens)
	}
	fmt.Fprintln(out)
}

func truncateLine(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func ageSince(t time.Time) string {
	return durationShort(time.Since(t))
}

func durationShort(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
