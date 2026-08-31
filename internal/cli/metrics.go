package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

// requireEntityType turns a required-parameter omission into a usable message. The backend
// answers a missing entityType with a 500, which reads as "the server is broken" rather
// than "you left out a flag".
func requireEntityType(v string) error {
	if strings.TrimSpace(v) == "" {
		return usageErrorf(
			"--entity-type is required by this API (e.g. K8S_POD, K8S_NODE) — " +
				"the metric-names, entities and tag endpoints all scope by it")
	}
	return nil
}

func newMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "metrics",
		Aliases: []string{"metric"},
		Short:   "Query time-series metrics",
	}
	cmd.AddCommand(newMetricsQueryCmd(), newMetricsListCmd(), newMetricsTagsCmd(), newMetricsEntitiesCmd())
	return cmd
}

func newMetricsQueryCmd() *cobra.Command {
	var (
		entityType, metricName string
		since, from, to        string
		aggType                string
		groupBy                []string
		filters                []string
		bucketSeconds          int
	)

	cmd := &cobra.Command{
		Use:     "query <metric-name>",
		Aliases: []string{"q"},
		Short:   "Query one metric as a time series",
		Long: `Query a metric over a window.

A terminal cannot draw a curve, so each series is summarised as last/min/max plus the point
count. Use -o json for the raw points.`,
		Example: `  rtz metrics query k8s.pod.cpu.usage --since 1h
  rtz metrics query k8s.node.memory.usage --group-by k8s.node.name --agg max`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			metricName = args[0]
			if err := requireEntityType(entityType); err != nil {
				return err
			}
			e, orgID, _, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			start, end, err := timeWindow(since, from, to)
			if err != nil {
				return err
			}

			parsed, err := parseTagFilters(filters)
			if err != nil {
				return err
			}

			q := api.MetricsQuery{
				EntityType: entityType,
				MetricName: metricName,
				From:       start.UTC().Format(time.RFC3339),
				To:         end.UTC().Format(time.RFC3339),
				AggType:    aggType,
				GroupBy:    groupBy,
				Filters:    parsed,
			}
			if bucketSeconds > 0 {
				q.BucketSeconds = &bucketSeconds
			}

			res, err := e.client.QueryMetrics(cmd.Context(), orgID, q)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(res, nil)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, windowLine(start, end))
			// A rate rewrite changes what the numbers mean, so it cannot stay implicit.
			if res.IsRate {
				fmt.Fprintf(out, "Aggregated as a per-second RATE (unit %s), bucket %ds\n",
					render.Dash(res.EffectiveUnit), res.BucketSeconds)
			} else {
				fmt.Fprintf(out, "Unit %s, bucket %ds\n", render.Dash(res.EffectiveUnit), res.BucketSeconds)
			}
			fmt.Fprintln(out)
			return e.printer.Print(res, view.MetricsTable(res))
		},
	}

	cmd.Flags().StringVar(&entityType, "entity-type", "", "entity type, e.g. K8S_POD (required)")
	cmd.Flags().StringVar(&since, "since", "1h", "look back this far: 15m, 2h, 7d")
	cmd.Flags().StringVar(&from, "from", "", "window start (RFC3339)")
	cmd.Flags().StringVar(&to, "to", "", "window end (RFC3339)")
	cmd.Flags().StringVar(&aggType, "agg", "avg", "avg, sum, min, max, p50, p95 or p99")
	cmd.Flags().StringSliceVar(&groupBy, "group-by", nil, "tag paths to group by")
	cmd.Flags().StringArrayVar(&filters, "filter", nil, "tag filter key=value (repeatable)")
	cmd.Flags().IntVar(&bucketSeconds, "bucket", 0, "bucket size in seconds (0 = auto)")
	return cmd
}

// parseTagFilters accepts key=value. Anything else is rejected rather than dropped: a filter
// that silently does not apply returns more data than asked for, which reads as a bigger
// problem than there is.
func parseTagFilters(raw []string) ([]api.TagFilter, error) {
	out := make([]api.TagFilter, 0, len(raw))
	for _, f := range raw {
		k, v, ok := strings.Cut(f, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, usageErrorf("cannot read --filter %q — expected key=value", f)
		}
		out = append(out, api.TagFilter{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
	}
	return out, nil
}

func newMetricsListCmd() *cobra.Command {
	var entityType string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "names"},
		Short:   "List available metric names",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireEntityType(entityType); err != nil {
				return err
			}
			e, orgID, _, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			names, err := e.client.MetricNames(cmd.Context(), orgID, entityType)
			if err != nil {
				return err
			}
			t := &render.Table{
				Headers:      []string{"METRIC", "ENTITY TYPE", "UNIT", "KIND"},
				EmptyMessage: "No metrics reported for this organization.",
			}
			for _, m := range names {
				t.Rows = append(t.Rows, []string{
					render.Dash(str(m["metricName"])), render.Dash(str(m["entityType"])),
					render.Dash(str(m["unit"])), render.Dash(str(m["metricKind"])),
				})
			}
			sort.Slice(t.Rows, func(i, j int) bool { return t.Rows[i][0] < t.Rows[j][0] })
			return e.printer.Print(names, t)
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", "entity type to scope by (required)")
	return cmd
}

func newMetricsTagsCmd() *cobra.Command {
	var entityType, metricName, tagKey string

	cmd := &cobra.Command{
		Use:   "tags",
		Short: "List tag keys, or the values of one key",
		Long:  "With --key, lists that key's values. Without it, lists the available keys.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireEntityType(entityType); err != nil {
				return err
			}
			e, orgID, _, err := clusterEnv(cmd)
			if err != nil {
				return err
			}

			var items []string
			header := "TAG KEY"
			if tagKey != "" {
				header = "VALUE"
				items, err = e.client.TagValues(cmd.Context(), orgID, entityType, metricName, tagKey)
			} else {
				items, err = e.client.TagKeys(cmd.Context(), orgID, entityType, metricName)
			}
			if err != nil {
				return err
			}

			sort.Strings(items)
			t := &render.Table{Headers: []string{header}, EmptyMessage: "Nothing reported."}
			for _, v := range items {
				t.Rows = append(t.Rows, []string{v})
			}
			return e.printer.Print(items, t)
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", "entity type to scope by (required)")
	cmd.Flags().StringVar(&metricName, "metric", "", "limit to one metric")
	cmd.Flags().StringVar(&tagKey, "key", "", "list this key's values instead of the keys")
	return cmd
}

func newMetricsEntitiesCmd() *cobra.Command {
	var entityType string

	cmd := &cobra.Command{
		Use:   "entities",
		Short: "List entities of one type that have reported metrics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireEntityType(entityType); err != nil {
				return err
			}
			e, orgID, _, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			rows, err := e.client.Entities(cmd.Context(), orgID, entityType)
			if err != nil {
				return err
			}
			t := &render.Table{
				Headers:      []string{"ENTITY ID", "NAME"},
				EmptyMessage: "No entities of this type have reported metrics.",
			}
			for _, m := range rows {
				t.Rows = append(t.Rows, []string{
					render.Dash(str(m["entityId"])), render.Dash(str(m["name"])),
				})
			}
			sort.Slice(t.Rows, func(i, j int) bool { return t.Rows[i][0] < t.Rows[j][0] })
			return e.printer.Print(rows, t)
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", "entity type to list (required)")
	return cmd
}
