package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newUpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Kubernetes upgrade readiness",
	}
	cmd.AddCommand(newUpgradeCheckCmd(), newUpgradeFleetCmd())
	return cmd
}

func newUpgradeCheckCmd() *cobra.Command {
	var target, failOn string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Whether the selected cluster is safe to upgrade",
		Long: `Blockers for a Kubernetes version bump, worst first.

Without --target the backend evaluates the implied next minor version.`,
		Example: `  rtz upgrade check
  rtz upgrade check --target 1.31 --fail-on high`,
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

			r, err := e.client.UpgradeReadiness(cmd.Context(), orgID, clusterID, target)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				if err := e.printer.Print(r, nil); err != nil {
					return err
				}
			} else {
				printReadiness(cmd, r)
				if err := e.printer.Print(r.Findings, view.FindingsTable(r.Findings)); err != nil {
					return err
				}
				warnReadinessQuality(cmd.ErrOrStderr(), r)
			}

			sevs := make([]string, 0, len(r.Findings))
			for _, f := range r.Findings {
				sevs = append(sevs, f.Severity)
			}
			return gate(threshold, sevs, "upgrade blockers")
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target Kubernetes version, e.g. 1.31")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 4 when a blocker reaches this severity")
	return cmd
}

func printReadiness(cmd *cobra.Command, r *api.UpgradeReadiness) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s → %s\n", render.Dash(r.CurrentVersion), render.Dash(r.TargetVersion))
	fmt.Fprintf(out, "  Score:   %d/100 %s (higher is worse)\n", r.Score, render.Dash(r.RiskLevel))

	if d := r.Support.DaysUntilForcedUpgrade; d != nil {
		fmt.Fprintf(out, "  Support: %d days until forced upgrade", *d)
		if r.Support.ForcedUpgradeDate != "" {
			fmt.Fprintf(out, " (%s)", r.Support.ForcedUpgradeDate)
		}
		fmt.Fprintln(out)
	}
	// An illustrative figure and a quoted price look identical once printed, so the
	// distinction has to be carried in the words.
	if c := r.Support.AnnualCost; c != nil && *c > 0 {
		label := "estimated"
		if r.Support.DataSourced != nil && !*r.Support.DataSourced {
			label = "ILLUSTRATIVE, not a quote"
		}
		fmt.Fprintf(out, "  Extended support: $%.0f/year (%s)\n", *c, label)
	}
	fmt.Fprintln(out)
}

// warnReadinessQuality surfaces the fields that say how much the verdict is worth. A
// readiness score built on stale or partial data reads exactly like one built on fresh
// complete data.
func warnReadinessQuality(w io.Writer, r *api.UpgradeReadiness) {
	if r.Stale {
		age := "unknown age"
		if r.DataAgeSeconds != nil {
			age = durationShort(secondsToDuration(*r.DataAgeSeconds)) + " old"
		}
		fmt.Fprintf(w, "warning: this verdict is built on STALE inventory (%s) — re-sync before acting on it\n", age)
	}
	switch strings.ToUpper(r.ScanStatus) {
	case "PARTIAL", "INSUFFICIENT_DATA", "STALE":
		fmt.Fprintf(w, "warning: scan status is %s — blockers may be missing from this list\n", r.ScanStatus)
	}
	var incomplete []string
	for _, c := range r.Coverage {
		if s := strings.ToUpper(c.Status); s != "COMPLETE" && s != "OK" && s != "" {
			incomplete = append(incomplete, c.Tier+"="+c.Status)
		}
	}
	if len(incomplete) > 0 {
		sort.Strings(incomplete)
		fmt.Fprintf(w, "warning: detection tiers not fully covered: %s\n", strings.Join(incomplete, ", "))
	}
}

func newUpgradeFleetCmd() *cobra.Command {
	var failOn string

	cmd := &cobra.Command{
		Use:   "fleet",
		Short: "Upgrade readiness for every cluster in the org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			threshold, err := failOnThreshold(failOn)
			if err != nil {
				return err
			}
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}

			items, err := e.client.FleetUpgradeReadiness(cmd.Context(), orgID)
			if err != nil {
				return err
			}

			// Soonest deadline first: the cluster running out of support is the one that
			// actually forces a decision.
			sort.SliceStable(items, func(i, j int) bool {
				a, b := items[i].DaysUntilForcedUpgrade, items[j].DaysUntilForcedUpgrade
				if a == nil && b == nil {
					return scoreOf(items[i]) > scoreOf(items[j])
				}
				if a == nil {
					return false
				}
				if b == nil {
					return true
				}
				return *a < *b
			})

			t := &render.Table{
				Headers:      []string{"CLUSTER", "CURRENT", "TARGET", "SCORE", "LEVEL", "CRIT", "HIGH", "FORCED IN"},
				WideHeaders:  []string{"CLUSTER ID", "FORCED DATE", "REMEDIATION"},
				EmptyMessage: "No clusters to evaluate.",
			}
			for _, it := range items {
				t.Rows = append(t.Rows, []string{
					render.Dash(it.Name), render.Dash(it.CurrentVersion), render.Dash(it.ImpliedTargetVersion),
					intPtr(it.Score), render.Dash(it.RiskLevel),
					intPtr(it.CriticalCount), intPtr(it.HighCount), daysPtr(it.DaysUntilForcedUpgrade),
				})
				rem := "<none>"
				if it.EstimatedRemediationDays != nil {
					rem = strconv.FormatFloat(*it.EstimatedRemediationDays, 'f', 1, 64) + "d"
				}
				t.WideRows = append(t.WideRows, []string{
					it.ClusterID, render.Dash(it.ForcedUpgradeDate), rem,
				})
			}
			if err := e.printer.Print(items, t); err != nil {
				return err
			}

			// A cluster with no score was never analysed; counting it as "fine" would be a
			// false pass, so it is reported rather than folded into the gate silently.
			var unanalysed int
			sevs := make([]string, 0, len(items))
			for _, it := range items {
				if it.Score == nil {
					unanalysed++
					continue
				}
				sevs = append(sevs, it.RiskLevel)
			}
			if unanalysed > 0 && !e.printer.Format.Structured() {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: %d cluster(s) could not be analysed and are NOT covered by --fail-on\n", unanalysed)
			}
			return gate(threshold, sevs, "clusters")
		},
	}
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 4 when a cluster reaches this risk level")
	return cmd
}

func scoreOf(i api.FleetUpgradeItem) int {
	if i.Score == nil {
		return -1
	}
	return *i.Score
}

func intPtr(v *int) string {
	if v == nil {
		return "<none>"
	}
	return strconv.Itoa(*v)
}

func daysPtr(v *int) string {
	if v == nil {
		return "<unknown>"
	}
	return strconv.Itoa(*v) + "d"
}

// --- the CI gate -------------------------------------------------------------

func newRiskCheckCmd() *cobra.Command {
	var file, failOn string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Evaluate a rendered manifest bundle before it ships",
		Long: `Score a rendered multi-doc Kubernetes YAML bundle for deployment risk.

Stateless and cluster-free — it needs only a token, which is what makes it usable as a
pre-merge gate. Reads stdin with -f -.`,
		Example: `  helm template ./chart | rtz risk check -f - --fail-on high
  rtz risk check -f manifests.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(file) == "" {
				return usageErrorf("-f is required: a file path, or - for stdin")
			}
			threshold, err := failOnThreshold(failOn)
			if err != nil {
				return err
			}

			var raw []byte
			if file == "-" {
				raw, err = io.ReadAll(cmd.InOrStdin())
			} else {
				raw, err = os.ReadFile(file)
			}
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}
			if len(strings.TrimSpace(string(raw))) == 0 {
				// An empty bundle scores 0 and would pass any gate — which, in a CI job whose
				// template step silently produced nothing, is the worst possible green.
				return &ExitError{Code: ExitUsage, Err: fmt.Errorf(
					"the manifest is empty — refusing to report a passing score for nothing")}
			}

			// No cluster is needed; this endpoint is org- and cluster-free.
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}

			res, err := e.client.EvaluateManifest(cmd.Context(), string(raw))
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				if err := e.printer.Print(res, nil); err != nil {
					return err
				}
			} else {
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "%d resource(s) evaluated — score %d/100 %s (higher is worse)\n\n",
					res.ResourceCount, res.Score, render.Dash(res.Level))
				if err := e.printer.Print(res.Findings, manifestTable(res.Findings)); err != nil {
					return err
				}
			}

			sevs := make([]string, 0, len(res.Findings))
			for _, f := range res.Findings {
				sevs = append(sevs, f.Severity)
			}
			return gate(threshold, sevs, "findings")
		},
	}
	cmd.Flags().StringVarP(&file, "filename", "f", "", "manifest file, or - for stdin")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 4 when a finding reaches this severity")
	return cmd
}

func manifestTable(findings []api.ManifestFind) *render.Table {
	sorted := append([]api.ManifestFind(nil), findings...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, _ := api.SeverityRank(sorted[i].Severity)
		b, _ := api.SeverityRank(sorted[j].Severity)
		return a > b
	})
	t := &render.Table{
		Headers:      []string{"SEVERITY", "RULE", "RESOURCE", "TITLE"},
		WideHeaders:  []string{"CATEGORY", "IMPACT", "RECOMMENDATION"},
		EmptyMessage: "No findings — this bundle is clean.",
	}
	for _, f := range sorted {
		resource := f.ResourceName
		if f.ResourceType != "" {
			resource = f.ResourceType + "/" + resource
		}
		t.Rows = append(t.Rows, []string{
			render.Dash(f.Severity), render.Dash(f.RuleID), render.Dash(resource), truncateLine(f.Title, 55),
		})
		t.WideRows = append(t.WideRows, []string{
			render.Dash(f.Category), strconv.Itoa(f.ScoreImpact), truncateLine(f.Recommendation, 60),
		})
	}
	return t
}

func secondsToDuration(s int64) time.Duration { return time.Duration(s) * time.Second }
