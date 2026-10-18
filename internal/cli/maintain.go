package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newMaintainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "maintain",
		Short: "End-of-life deadlines, running packages, and data-service upgrade impact",
	}
	cmd.AddCommand(newMaintainItemsCmd(), newMaintainPackagesCmd(), newMaintainDependentsCmd(), newMaintainImpactCmd())
	return cmd
}

// errReadFailed is returned after printing whatever did come back: a list the backend could
// not finish reading must never exit 0, or a CI job reads it as "nothing due".
func errReadFailed(what string) error {
	return &ExitError{Code: ExitFailure, Err: errors.New(what + ": the backend could not read all of it — the list is incomplete")}
}

func newMaintainItemsCmd() *cobra.Command {
	var within, failOnDays int
	var platform bool

	cmd := &cobra.Command{
		Use:   "items",
		Short: "Support deadlines across the org, soonest first",
		Long: `Every Maintain item — Kubernetes, add-ons, runtimes and OS releases in images, data
services — with its next support deadline.

--fail-on-days N exits 4 when any listed item's deadline is N days away or less (or past).`,
		Example: `  rtz maintain items --within 90
  rtz maintain items --fail-on-days 30`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if within < -1 || failOnDays < -1 {
				return usageErrorf("--within and --fail-on-days take a day count of 0 or more")
			}
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}
			p, err := e.client.MaintainItems(cmd.Context(), orgID)
			if err != nil {
				return err
			}
			items := make([]api.MaintainItem, 0, len(p.Typed.Items))
			kept := make([]bool, len(p.Typed.Items))
			hiddenPlatform := 0
			for i, it := range p.Typed.Items {
				if it.Platform && !platform {
					hiddenPlatform++
					continue
				}
				if within >= 0 && (it.DaysLeft == nil || *it.DaysLeft > within) {
					continue
				}
				kept[i] = true
				items = append(items, it)
			}
			if e.printer.Format.Structured() {
				// The filters apply to the full document too, row for row: typed and raw items
				// are decoded from the same array, in the same order.
				if doc, ok := p.Raw.(map[string]any); ok {
					if raw, ok := doc["items"].([]any); ok && len(raw) == len(kept) {
						out := make([]any, 0, len(items))
						for i, r := range raw {
							if kept[i] {
								out = append(out, r)
							}
						}
						doc["items"] = out
					}
				}
				if err := e.printer.Print(p.Raw, nil); err != nil {
					return err
				}
			} else {
				if err := e.printer.Print(nil, view.MaintainItemsTable(items)); err != nil {
					return err
				}
				warn := cmd.ErrOrStderr()
				if hiddenPlatform > 0 {
					fmt.Fprintf(warn, "note: %d platform item(s) (Kubernetes, add-ons, system workloads) hidden — --platform to include; `rtz upgrade check` covers them\n", hiddenPlatform)
				}
				if p.Typed.Truncated {
					fmt.Fprintln(warn, "warning: the item list was truncated")
				}
			}
			if p.Typed.ReadFailed {
				return errReadFailed("maintain items")
			}
			if failOnDays >= 0 {
				due := 0
				for _, it := range items {
					if it.DaysLeft != nil && *it.DaysLeft <= failOnDays {
						due++
					}
				}
				if due > 0 {
					return &ExitError{Code: ExitPolicy, Err: fmt.Errorf("%d item(s) within %d days of a support deadline", due, failOnDays)}
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&within, "within", -1, "only items whose deadline is this many days away or less")
	cmd.Flags().IntVar(&failOnDays, "fail-on-days", -1, "exit 4 when an item's deadline is this many days away or less")
	cmd.Flags().BoolVar(&platform, "platform", false, "include platform items (Kubernetes, add-ons, system workloads)")
	return cmd
}

func newMaintainPackagesCmd() *cobra.Command {
	var ecosystem, q string
	var page, size int

	cmd := &cobra.Command{
		Use:     "packages",
		Short:   "Packages running across the fleet, most-used first, with end-of-life",
		Example: "  rtz maintain packages --ecosystem maven --q log4j",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if page < 0 || size < 1 {
				return usageErrorf("--page must be 0 or more and --size at least 1")
			}
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}
			p, err := e.client.MaintainInventory(cmd.Context(), orgID, ecosystem, q, page, size)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				if err := e.printer.Print(p.Raw, nil); err != nil {
					return err
				}
			} else {
				inv := &p.Typed
				if err := e.printer.Print(nil, view.InventoryTable(inv)); err != nil {
					return err
				}
				warn := cmd.ErrOrStderr()
				fmt.Fprintf(warn, "page %d · %d of %d packages\n", inv.Page, len(inv.Items), inv.Total)
				if inv.WorkloadsWithoutPackageList > 0 {
					fmt.Fprintf(warn, "warning: %d workload(s) have no package list yet (%d do) — they are not in these counts\n",
						inv.WorkloadsWithoutPackageList, inv.WorkloadsWithPackageList)
				}
			}
			if p.Typed.ReadFailed {
				return errReadFailed("maintain packages")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&ecosystem, "ecosystem", "", "only this ecosystem (maven, npm, pypi, go …)")
	cmd.Flags().StringVar(&q, "q", "", "package name contains")
	cmd.Flags().IntVar(&page, "page", 0, "page number, from 0")
	cmd.Flags().IntVar(&size, "size", 50, "packages per page")
	return cmd
}

// dataServiceRef resolves <namespace>/<kind>/<name> in the selected cluster.
func dataServiceRef(cmd *cobra.Command, ref string) (*env, string, api.DataServiceRef, error) {
	parts, err := splitRef(ref, 3, "<namespace>/<kind>/<name>, e.g. data/StatefulSet/postgres")
	if err != nil {
		return nil, "", api.DataServiceRef{}, err
	}
	e, orgID, clusterID, err := clusterEnv(cmd)
	if err != nil {
		return nil, "", api.DataServiceRef{}, err
	}
	return e, orgID, api.DataServiceRef{ClusterID: clusterID, Namespace: parts[0], Kind: parts[1], Name: parts[2]}, nil
}

func newMaintainDependentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "dependents <namespace>/<kind>/<name>",
		Short:   "Workloads that connect to a data service (its blast radius)",
		Example: "  rtz maintain dependents data/StatefulSet/postgres",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, ref, err := dataServiceRef(cmd, args[0])
			if err != nil {
				return err
			}
			p, err := e.client.DataServiceDependents(cmd.Context(), orgID, ref)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				if err := e.printer.Print(p.Raw, nil); err != nil {
					return err
				}
			} else {
				d, out := &p.Typed, cmd.OutOrStdout()
				fmt.Fprintf(out, "%s/%s/%s — coverage %s, %d of %d nodes reporting, last snapshot %s ago\n",
					ref.Namespace, ref.Kind, ref.Name, render.Dash(d.Coverage), d.NodesReported, d.NodesExpected,
					view.Since(d.LastSnapshotAt))
				if err := printSection(e, out, "Dependents", view.DependentsTable(d)); err != nil {
					return err
				}
				warnDependents(cmd.ErrOrStderr(), d.NodesReported, d.NodesExpected, d.Truncated,
					d.PlatformExcluded, d.OtherClustersHidden, d.Inbound)
			}
			if p.Typed.ReadFailed {
				return errReadFailed("dependents")
			}
			return nil
		},
	}
}

func newMaintainImpactCmd() *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "impact <namespace>/<kind>/<name>",
		Short: "What breaks if a data service is upgraded, per dependent",
		Long: `Each workload that connects to the data service, judged against the target version:
BUMP_DRIVER (a stated break), UNKNOWN (nothing to judge by — never read as fine), REVIEW, READY.`,
		Example: "  rtz maintain impact data/StatefulSet/postgres --target 17",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, ref, err := dataServiceRef(cmd, args[0])
			if err != nil {
				return err
			}
			p, err := e.client.DataServiceImpact(cmd.Context(), orgID, ref, target)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				if err := e.printer.Print(p.Raw, nil); err != nil {
					return err
				}
			} else {
				im, out, warn := &p.Typed, cmd.OutOrStdout(), cmd.ErrOrStderr()
				name := firstNonEmpty(im.DisplayName, im.Product, ref.Name)
				fmt.Fprintf(out, "%s %s → %s", name, render.Dash(im.CurrentVersion), render.Dash(im.Target))
				if im.TargetDefaulted {
					fmt.Fprint(out, " (default target)")
				}
				fmt.Fprintln(out)
				if im.Operator != "" {
					fmt.Fprintf(out, "  Operator: %s\n", im.Operator)
				}
				if s := im.Summary; s != nil {
					fmt.Fprintf(out, "  Dependents: %d bump driver · %d unknown · %d review · %d ready\n",
						s.BumpDriver, s.Unknown, s.Review, s.Ready)
				}
				if !im.RulePack {
					fmt.Fprintf(warn, "warning: no upgrade rule pack for %s — verdicts cannot name breaking changes\n", name)
				}
				if err := printSection(e, out, "Dependents", view.ImpactTable(im)); err != nil {
					return err
				}
				if len(im.UpgradeNotes) > 0 {
					fmt.Fprintf(out, "\nUpgrade notes (%d)\n", len(im.UpgradeNotes))
					for _, n := range im.UpgradeNotes {
						fmt.Fprintf(out, "  %s %s %s\n", render.Dash(n.Severity), render.Dash(n.Since), n.Title)
					}
				}
				warnDependents(warn, -1, -1, im.Truncated, im.PlatformExcluded, im.OtherClustersHidden, im.Inbound)
			}
			if p.Typed.ReadFailed {
				return errReadFailed("impact")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target version (backend default when unset)")
	return cmd
}

// warnDependents states what the dependents list cannot include. reported/expected < 0 skips
// the node-coverage line.
func warnDependents(w io.Writer, reported, expected int, truncated bool, platform, otherClusters int, in *api.DataServiceInbound) {
	if expected >= 0 && reported < expected {
		fmt.Fprintf(w, "warning: only %d of %d nodes reported connections — dependents on the others are missing\n", reported, expected)
	}
	if truncated {
		fmt.Fprintln(w, "warning: the dependents list was truncated")
	}
	if platform > 0 {
		fmt.Fprintf(w, "note: %d platform workload(s) excluded\n", platform)
	}
	if otherClusters > 0 {
		fmt.Fprintf(w, "note: %d dependent(s) in clusters you cannot see are hidden\n", otherClusters)
	}
	if in != nil && in.Reported && (in.ViaNode > 0 || in.Other > 0) {
		fmt.Fprintf(w, "note: callers that could not be named: %d via a node, %d other\n", in.ViaNode, in.Other)
	}
}
