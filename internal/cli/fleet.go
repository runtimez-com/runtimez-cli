package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/render"
)

func newFleetCmd() *cobra.Command {
	var windowDays int
	var env string

	cmd := &cobra.Command{
		Use:   "fleet",
		Short: "Org-wide rollup across every cluster",
		Long: `Cluster health, findings by severity, and recent release verdicts for the whole
organization.

Risk scores run 0-100 and HIGHER IS WORSE (0-30 LOW, 31-60 MEDIUM, 61-80 HIGH, 81-100
CRITICAL).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}

			summary, err := e.client.Fleet(cmd.Context(), orgID, windowDays, env, "")
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(summary, nil)
			}

			out := cmd.OutOrStdout()
			score := "<no data>"
			if summary.FleetRiskScore != nil {
				score = fmt.Sprintf("%d/100 %s (higher is worse)",
					*summary.FleetRiskScore, render.Dash(summary.FleetRiskLevel))
			}
			fmt.Fprintf(out, "Fleet risk:  %s\n", score)
			fmt.Fprintf(out, "Clusters:    %d total, %d connected, %d degraded\n",
				summary.Clusters.Total, summary.Clusters.Connected, summary.Clusters.Degraded)

			f := summary.FindingsBySeverity
			fmt.Fprintf(out, "Findings:    %d critical, %d high, %d medium, %d low\n", f.Crit, f.High, f.Med, f.Low)

			r := summary.Releases7d
			fmt.Fprintf(out, "Releases:    %d healthy, %d degraded, %d awaiting, %d insufficient data (of %d)\n",
				r.Healthy, r.Degraded, r.AwaitingVerification, r.InsufficientData, r.Total)

			if len(summary.ClusterLastVerdicts) > 0 {
				fmt.Fprintln(out)
				t := &render.Table{Headers: []string{"CLUSTER", "LAST VERDICT", "LAST OUTCOME"}}
				ids := make([]string, 0, len(summary.ClusterLastVerdicts))
				for id := range summary.ClusterLastVerdicts {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				for _, id := range ids {
					t.Rows = append(t.Rows, []string{
						id,
						render.Dash(summary.ClusterLastVerdicts[id]),
						render.Dash(summary.ClusterLastOutcomes[id]),
					})
				}
				_ = (&render.Printer{Out: out, Format: render.FormatTable}).Print(nil, t)
			}
			if summary.GeneratedAt != nil {
				fmt.Fprintf(out, "\nGenerated %s ago\n", age(summary.GeneratedAt))
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&windowDays, "window", 7, "release window in days")
	// The backend accepts env for forward-compatibility and currently ignores it, so the
	// flag exists but must not imply a filter that is happening.
	cmd.Flags().StringVar(&env, "env", "", "environment filter (accepted by the API but currently a no-op server-side)")
	return cmd
}
