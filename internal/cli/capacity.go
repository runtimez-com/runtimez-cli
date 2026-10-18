package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newCapacityCmd() *cobra.Command {
	var section string

	cmd := &cobra.Command{
		Use:   "capacity",
		Short: "Schedulable capacity: headroom, pools, pending pods",
		Long: `Schedulable capacity of the selected cluster from its last sync.

Without --section it prints a bounded summary: headroom per axis, one row per node pool, and
the pending-pod count. --section prints one part in full: ` + strings.Join(view.CapacitySections, ", ") + `.`,
		Example: `  rtz capacity
  rtz capacity --section blocked
  rtz capacity --section storage -o wide
  rtz capacity trend --days 30`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if section != "" && !slices.Contains(view.CapacitySections, section) {
				return usageErrorf("unknown --section %q — use one of %s", section, strings.Join(view.CapacitySections, ", "))
			}
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			p, err := e.client.Capacity(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(p.Raw, nil)
			}
			c := &p.Typed
			out, warn := cmd.OutOrStdout(), cmd.ErrOrStderr()
			warnStatus(warn, "capacity", c.Status, c.Message)
			if section == "" {
				return printCapacitySummary(e, out, warn, c)
			}
			return printCapacitySection(e, out, warn, c, section)
		},
	}
	cmd.Flags().StringVar(&section, "section", "", "print one section in full: "+strings.Join(view.CapacitySections, "|"))
	cmd.AddCommand(newCapacityTrendCmd(), newCapacityCostCmd(), newCapacityNamespacesCmd(), newCapacityFleetCmd())
	return cmd
}

func printCapacitySummary(e *env, out, warn io.Writer, c *api.Capacity) error {
	fmt.Fprintf(out, "Capacity (synced %s ago)\n", view.Since(c.LastSyncAt))
	if a := c.Autoscaler; a != nil {
		acting := ""
		if a.Acting {
			acting = ", acting"
		}
		fmt.Fprintf(out, "  Autoscaler: %s%s", render.Dash(a.State), acting)
		if a.ManagedBy != "" {
			fmt.Fprintf(out, " (managed by %s)", a.ManagedBy)
		}
		fmt.Fprintln(out)
	}
	if cc := c.ClusterCeiling; cc != nil && cc.MaxNodes != nil {
		fmt.Fprintf(out, "  Node ceiling: %d of %d (%s)\n", cc.CurrentNodes, *cc.MaxNodes, render.Dash(cc.Source))
	}
	if u := c.Usage; u != nil {
		warnStatus(warn, "usage", u.Status, u.Reason)
		if u.NodesWithoutSamples > 0 {
			fmt.Fprintf(warn, "warning: %d node(s) have no usage samples — USED columns cover only %d node(s)\n",
				u.NodesWithoutSamples, u.NodesWithSamples)
		}
	}
	warnCapacityFlags(warn, c)

	if err := printSection(e, out, "Headroom", view.HeadroomTable(c)); err != nil {
		return err
	}
	if err := printSection(e, out, "Pools", view.PoolsTable(c.Pools)); err != nil {
		return err
	}
	if b := c.BlockedSummary; b != nil {
		fmt.Fprintf(out, "\nPending pods: %d (%d capacity shortfall, %d gated, %d nominated)", b.Total,
			b.CapacityShortfall, b.Gated, b.Nominated)
		if len(b.ByCategory) > 0 {
			fmt.Fprintf(out, " — %s", view.Counts(b.ByCategory))
		}
		fmt.Fprintln(out)
		if b.Total > 0 {
			fmt.Fprintln(out, "  rtz capacity --section blocked   for each pod and why")
		}
		if !b.EventsRead {
			fmt.Fprintln(warn, "warning: scheduler events were not read — pending reasons may be missing")
		}
	}
	return nil
}

func warnCapacityFlags(w io.Writer, c *api.Capacity) {
	f := c.Flags
	if f == nil {
		return
	}
	if f.NodeRowsTruncated {
		fmt.Fprintln(w, "warning: node rows were truncated — per-node figures are incomplete")
	}
	if f.PodRowsTruncated {
		fmt.Fprintln(w, "warning: pod rows were truncated — requested figures are a lower bound")
	}
	if f.Nodeless {
		fmt.Fprintln(w, "warning: no nodes were synced for this cluster")
	}
	for _, n := range f.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
}

func printCapacitySection(e *env, out, warn io.Writer, c *api.Capacity, section string) error {
	switch section {
	case "pools":
		return e.printer.Print(nil, view.PoolsTable(c.Pools))
	case "nodes":
		warnCapacityFlags(warn, c)
		return e.printer.Print(nil, view.CapacityNodesTable(c))
	case "blocked":
		if b := c.BlockedSummary; b != nil && b.Listed < b.Total {
			fmt.Fprintf(warn, "note: %d of %d pending pods listed\n", b.Listed, b.Total)
		}
		return e.printer.Print(nil, view.BlockedTable(c))
	case "storage":
		if s := c.Storage; s != nil {
			warnStatus(warn, "storage", s.Status, s.Reason)
			warnStatus(warn, "volume metrics", s.Metrics, s.MetricsReason)
			if s.PVCsNotListed > 0 {
				fmt.Fprintf(warn, "note: %d more claims not listed\n", s.PVCsNotListed)
			}
			defer printNotes(out, s.Notes)
		} else {
			fmt.Fprintln(warn, "warning: storage was not reported for this cluster")
		}
		return e.printer.Print(nil, view.StorageTable(c))
	case "network":
		if n := c.Network; n != nil {
			warnStatus(warn, "network", n.Status, n.Reason)
			if n.PodsWaitingForIP != nil && *n.PodsWaitingForIP > 0 {
				fmt.Fprintf(out, "Pods waiting for an IP: %d\n", *n.PodsWaitingForIP)
			}
			defer printNotes(out, n.Notes)
		} else {
			fmt.Fprintln(warn, "warning: pod networking was not reported for this cluster")
		}
		return e.printer.Print(nil, view.NetworkTable(c))
	case "batch":
		if b := c.Batch; b != nil {
			warnStatus(warn, "batch", b.Status, b.Message)
			if !b.Collected {
				fmt.Fprintln(warn, "warning: Jobs and CronJobs were not collected — an empty list is not \"no batch\"")
			}
			defer printNotes(out, b.Notes)
		}
		return e.printer.Print(nil, view.BatchTable(c))
	case "special":
		if s := c.Special; s != nil {
			warnStatus(warn, "special resources", s.Status, s.Reason)
			defer printNotes(out, s.Notes)
		}
		return e.printer.Print(nil, view.SpecialTable(c))
	}
	return usageErrorf("unknown --section %q", section)
}

func newCapacityTrendCmd() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "trend",
		Short: "Capacity growth and days until each pool is full",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if days < 0 {
				return usageErrorf("--days must be positive")
			}
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			p, err := e.client.CapacityTrend(cmd.Context(), orgID, clusterID, days)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(p.Raw, nil)
			}
			t, out := &p.Typed, cmd.OutOrStdout()
			warnStatus(cmd.ErrOrStderr(), "trend", t.Status, t.Message)
			if t.DaysCollected < t.MinDays {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d days collected — forecasts are not available yet\n",
					t.DaysCollected, t.MinDays)
			}
			fmt.Fprintf(out, "Trend %s → %s (%d days), requests target %.0f%% (%s)\n",
				render.Dash(t.From), render.Dash(t.To), t.Days, t.Target, render.Dash(t.TargetSource))
			if err := printSection(e, out, "Pools", view.CapacityTrendTable(t)); err != nil {
				return err
			}
			printNotes(out, t.Notes)
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 0, "window in days (backend default when unset)")
	return cmd
}

func newCapacityCostCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cost",
		Short: "Node cost per pool and the levers that change it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			p, err := e.client.CapacityCost(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(p.Raw, nil)
			}
			c, out, warn := &p.Typed, cmd.OutOrStdout(), cmd.ErrOrStderr()
			warnStatus(warn, "cost", c.Status, c.Message)
			fmt.Fprintf(out, "Cluster: %s/month (%s/hour), %d of %d nodes priced\n",
				view.Money(c.ClusterMonthly), view.Money(c.ClusterHourly), c.PricedNodes, c.Nodes)
			// A list price and a negotiated rate look identical once printed; name the source.
			if s := c.PriceSource; s != nil {
				fmt.Fprintf(out, "  Prices: %s", render.Dash(firstNonEmpty(s.Label, s.OnDemandSource, s.Provider)))
				if s.OnDemandDate != "" {
					fmt.Fprintf(out, " as of %s", s.OnDemandDate)
				}
				fmt.Fprintln(out)
			}
			if c.EffectiveMonthly != nil {
				fmt.Fprintf(out, "  Effective (after commitments): %s/month\n", view.Money(c.EffectiveMonthly))
			}
			if c.PricedNodes < c.Nodes {
				fmt.Fprintf(warn, "warning: %d node(s) unpriced — totals cover priced nodes only\n", c.Nodes-c.PricedNodes)
			}
			if cv := c.Coverage; cv != nil {
				warnStatus(warn, "commitment coverage", cv.State, cv.Detail)
			}
			if err := printSection(e, out, "Pools", view.CapacityCostTable(c)); err != nil {
				return err
			}
			if err := printSection(e, out, "Levers", view.CostLeversTable(c)); err != nil {
				return err
			}
			printNotes(out, c.Notes)
			return nil
		},
	}
}

func newCapacityNamespacesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "namespaces",
		Aliases: []string{"ns"},
		Short:   "Requests, quota and pending pods per namespace",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			p, err := e.client.CapacityNamespaces(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(p.Raw, nil)
			}
			n := &p.Typed
			warnStatus(cmd.ErrOrStderr(), "namespaces", n.Status, n.Message)
			if !n.QuotasCollected {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: ResourceQuotas were not collected — QUOTA reads NOT_COLLECTED, not \"no quota\"")
			}
			if err := e.printer.Print(nil, view.CapacityNamespacesTable(n)); err != nil {
				return err
			}
			printNotes(cmd.OutOrStdout(), n.Notes)
			return nil
		},
	}
}

func newCapacityFleetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fleet",
		Short: "Capacity across every cluster in the org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}
			p, err := e.client.FleetCapacity(cmd.Context(), orgID)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(p.Raw, nil)
			}
			f, out, warn := &p.Typed, cmd.OutOrStdout(), cmd.ErrOrStderr()
			if tot := f.Totals; tot != nil {
				fmt.Fprintf(out, "Fleet: %d clusters counted of %d", tot.ClustersCounted, f.ClustersTotal)
				if tot.CPU != nil {
					fmt.Fprintf(out, ", CPU requests %s", view.Pct(tot.CPU.RequestsCommitmentPct))
				}
				if tot.Memory != nil {
					fmt.Fprintf(out, ", memory requests %s", view.Pct(tot.Memory.RequestsCommitmentPct))
				}
				fmt.Fprintln(out)
				if s := tot.SoonestFull; s != nil && s.Days != nil {
					fmt.Fprintf(out, "  Fills first: %s %s %s in %s\n", render.Dash(s.ClusterName), s.Pool, s.Axis, view.Days(s.Days))
				}
				if len(tot.NotCounted) > 0 {
					fmt.Fprintf(warn, "warning: clusters not counted in the totals: %s\n", view.Counts(tot.NotCounted))
				}
				if tot.CountedFromEarlierSync > 0 {
					fmt.Fprintf(warn, "note: %d cluster(s) counted from an earlier sync\n", tot.CountedFromEarlierSync)
				}
			}
			if f.Truncated {
				fmt.Fprintln(warn, "warning: the cluster list was truncated")
			}
			return printSection(e, out, "Clusters", view.FleetCapacityTable(f))
		},
	}
}
