package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

// failOnThreshold turns a --fail-on value into a severity rank. Rejecting an unknown value
// matters more here than anywhere else: a typo that parsed as "never fail" would turn a CI
// gate into a no-op that reports success forever.
func failOnThreshold(s string) (int, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	rank, ok := api.SeverityRank(s)
	if !ok {
		return 0, usageErrorf("unknown --fail-on severity %q — use low, medium, high or critical", s)
	}
	return rank, nil
}

// gate returns the policy exit error when anything reached the threshold.
func gate(threshold int, severities []string, what string) error {
	if threshold == 0 {
		return nil
	}
	worst, count := 0, 0
	for _, s := range severities {
		r, ok := api.SeverityRank(s)
		if !ok {
			continue
		}
		if r >= threshold {
			count++
		}
		if r > worst {
			worst = r
		}
	}
	if count == 0 {
		return nil
	}
	return &ExitError{Code: ExitPolicy, Err: fmt.Errorf(
		"%d %s at or above the --fail-on threshold", count, what)}
}

// warnMissingSignals says which evidence was unavailable.
//
// This is the difference between "nothing is wrong" and "we could not see". A clean score
// computed without metrics is not a clean bill of health, and presenting it as one is the
// most damaging thing this command could do.
func warnMissingSignals(w io.Writer, missing []string) {
	if len(missing) == 0 {
		return
	}
	fmt.Fprintf(w, "warning: scored WITHOUT %s — a low score here is not a clean bill of health\n",
		strings.Join(missing, ", "))
}

func newRiskCmd() *cobra.Command {
	var failOn string

	cmd := &cobra.Command{
		Use:   "risk",
		Short: "Workload risk posture for the selected cluster",
		Long: `Scored workloads, worst first. Scores run 0-100 and HIGHER IS WORSE
(0-30 LOW, 31-60 MEDIUM, 61-80 HIGH, 81-100 CRITICAL).

Subcommands cover the other axes: security, cve, compliance.`,
		Example: `  rtz risk
  rtz risk --fail-on high      # exit 4 when any workload is HIGH or worse
  rtz risk security -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			threshold, err := failOnThreshold(failOn)
			if err != nil {
				return err
			}
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}

			posture, err := e.client.WorkloadRiskPosture(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				if err := e.printer.Print(posture, nil); err != nil {
					return err
				}
			} else {
				out := cmd.OutOrStdout()
				s := posture.Summary
				fmt.Fprintf(out, "%d workloads: %d critical, %d high, %d medium, %d low\n",
					s.Total, s.Critical, s.High, s.Medium, s.Low)
				if posture.UnevaluableWorkloads > 0 {
					fmt.Fprintf(out, "%d workload(s) could not be evaluated\n", posture.UnevaluableWorkloads)
				}
				fmt.Fprintln(out)
				if err := e.printer.Print(posture.Workloads, view.RiskTable(posture.Workloads)); err != nil {
					return err
				}
				warnMissingSignals(cmd.ErrOrStderr(), posture.MissingSignals())
			}

			levels := make([]string, 0, len(posture.Workloads))
			for _, w := range posture.Workloads {
				levels = append(levels, w.Level)
			}
			return gate(threshold, levels, "workloads")
		},
	}

	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 4 when anything reaches this severity: low, medium, high, critical")

	cmd.AddCommand(newPostureCmd("security", "Policy findings from the security scan",
		func(e *env, cmd *cobra.Command, orgID, clusterID string) (*api.Posture, error) {
			return e.client.SecurityPosture(cmd.Context(), orgID, clusterID)
		}))
	cmd.AddCommand(newPostureCmd("compliance", "Control findings for the cluster's compliance framework",
		func(e *env, cmd *cobra.Command, orgID, clusterID string) (*api.Posture, error) {
			return e.client.CompliancePosture(cmd.Context(), orgID, clusterID)
		}))
	cmd.AddCommand(newCveCmd())
	cmd.AddCommand(newWorkloadRiskCmd())
	cmd.AddCommand(newRiskCheckCmd())
	return cmd
}

// newPostureCmd builds the security and compliance subcommands, which share a response shape.
func newPostureCmd(name, short string,
	fetch func(*env, *cobra.Command, string, string) (*api.Posture, error)) *cobra.Command {

	var failOn string
	cmd := &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			threshold, err := failOnThreshold(failOn)
			if err != nil {
				return err
			}
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}

			p, err := fetch(e, cmd, orgID, clusterID)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				if err := e.printer.Print(p, nil); err != nil {
					return err
				}
			} else {
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "Score %d/100 %s (higher is worse)\n", p.Score, render.Dash(p.RiskLevel))
				fmt.Fprintf(out, "Scanned: %s\n\n", scanStamp(p.ScannedAt))
				if err := e.printer.Print(p.Findings, view.FindingsTable(p.Findings)); err != nil {
					return err
				}
				// Zero findings means "clean" only if a scan actually ran.
				if len(p.Findings) == 0 && p.ScannedAt == nil {
					fmt.Fprintln(cmd.ErrOrStderr(),
						"warning: no findings because this cluster has NEVER been scanned — not because it is clean")
				}
			}

			sevs := make([]string, 0, len(p.Findings))
			for _, f := range p.Findings {
				sevs = append(sevs, f.Severity)
			}
			return gate(threshold, sevs, "findings")
		},
	}
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 4 when anything reaches this severity")
	return cmd
}

func newCveCmd() *cobra.Command {
	var failOn string
	var fixableOnly bool

	cmd := &cobra.Command{
		Use:   "cve",
		Short: "Image vulnerabilities for the selected cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			threshold, err := failOnThreshold(failOn)
			if err != nil {
				return err
			}
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}

			p, err := e.client.CvePosture(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}

			findings := p.Findings
			if fixableOnly {
				var kept []api.CveFinding
				for _, f := range findings {
					if f.FixedVersion != "" {
						kept = append(kept, f)
					}
				}
				findings = kept
			}

			if e.printer.Format.Structured() {
				if err := e.printer.Print(p, nil); err != nil {
					return err
				}
			} else {
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "Score %d/100 %s (higher is worse)\n", p.Score, render.Dash(p.RiskLevel))
				fmt.Fprintf(out, "Scanned: %s\n", scanStamp(p.ScannedAt))
				if n := severityTotal(p.NewCountsBySeverity); n > 0 {
					fmt.Fprintf(out, "New since the previous scan: %s\n", severitySummary(p.NewCountsBySeverity))
				}
				fmt.Fprintln(out)
				if err := e.printer.Print(findings, view.CveTable(findings)); err != nil {
					return err
				}
				if len(p.Findings) == 0 && p.ScannedAt == nil {
					fmt.Fprintln(cmd.ErrOrStderr(),
						"warning: no findings because no CVE scan has ever run on this cluster — not because it is clean")
				}
			}

			// The gate counts every finding, not just the fixable view: --fixable is a
			// display filter, and letting it lower the exit code would make the gate
			// depend on how you looked at the data.
			sevs := make([]string, 0, len(p.Findings))
			for _, f := range p.Findings {
				sevs = append(sevs, f.Severity)
			}
			return gate(threshold, sevs, "vulnerabilities")
		},
	}
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 4 when anything reaches this severity")
	cmd.Flags().BoolVar(&fixableOnly, "fixable", false, "show only vulnerabilities with a fixed version (display only; --fail-on still counts all)")
	return cmd
}

// newWorkloadRiskCmd explains one workload's score factor by factor.
func newWorkloadRiskCmd() *cobra.Command {
	var namespace string
	return &cobra.Command{
		Use:   "workload <namespace>/<name>",
		Short: "Show why one workload scored the way it did",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			ns, name, err := splitWorkloadRef(args[0], namespace)
			if err != nil {
				return err
			}

			posture, err := e.client.WorkloadRiskPosture(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			var found *api.WorkloadRiskItem
			for i := range posture.Workloads {
				if posture.Workloads[i].Namespace == ns && posture.Workloads[i].Name == name {
					found = &posture.Workloads[i]
					break
				}
			}
			if found == nil {
				return &ExitError{Code: ExitFailure, Err: fmt.Errorf(
					"no risk entry for %s/%s — it may be unevaluable, or not a scored kind", ns, name)}
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(found, nil)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s/%s (%s)\n", found.Namespace, found.Name, render.Dash(found.Kind))
			fmt.Fprintf(out, "  Score: %d/100 %s (higher is worse)\n\n", found.Score, render.Dash(found.Level))

			factors := append([]api.WorkloadRiskFact(nil), found.Factors...)
			sort.SliceStable(factors, func(i, j int) bool {
				return factors[i].ScoreContribution > factors[j].ScoreContribution
			})
			for _, f := range factors {
				fmt.Fprintf(out, "  [%s +%d] %s\n", render.Dash(f.Severity), f.ScoreContribution, f.Title)
				if f.Detail != "" {
					fmt.Fprintf(out, "      %s\n", f.Detail)
				}
				// Several rules carry the same sentence in both fields; printing it twice
				// reads like two separate instructions.
				if f.Recommendation != "" && f.Recommendation != f.Detail {
					fmt.Fprintf(out, "      fix: %s\n", f.Recommendation)
				}
			}
			warnMissingSignals(cmd.ErrOrStderr(), posture.MissingSignals())
			return nil
		},
	}
}

func severityTotal(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func severitySummary(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := api.SeverityRank(keys[i])
		b, _ := api.SeverityRank(keys[j])
		return a > b
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if m[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", m[k], strings.ToLower(k)))
		}
	}
	return strings.Join(parts, ", ")
}
