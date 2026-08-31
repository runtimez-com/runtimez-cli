package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/k8s"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newGetCmd() *cobra.Command {
	var namespace string
	var selector string

	cmd := &cobra.Command{
		Use:     "get <kind> [name]",
		Aliases: []string{"g"},
		Short:   "List resources synced from a cluster",
		Long: `List resources the runtimez agent has synced, without a kubeconfig.

Kinds accept the usual short forms (po, deploy, sts, ds, svc, ing, no). "all" lists every
kind. With no -n, every namespace is listed.`,
		Example: `  rtz get pods -n payments
  rtz get deploy
  rtz get svc -o wide
  rtz get nodes -o json | jq -r '.[].name'`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}
			clusterID, err := e.requireCluster()
			if err != nil {
				return err
			}
			kind, err := resolveKind(args[0])
			if err != nil {
				return err
			}

			rows, err := e.client.Resources(cmd.Context(), orgID, clusterID, kind, namespace)
			if err != nil {
				return err
			}

			// A name argument filters client-side: the API has no name parameter, and doing
			// it here keeps `get deploy web` working the way muscle memory expects.
			if len(args) == 2 {
				rows = filterByName(rows, args[1])
			}
			if selector != "" {
				rows, err = filterBySelector(rows, selector)
				if err != nil {
					return err
				}
			}

			if notice := truncationNotice(len(rows), api.ResourceRowLimit); notice != "" && !e.printer.Format.Structured() {
				fmt.Fprintln(cmd.ErrOrStderr(), notice)
			}
			return e.printer.Print(rows, view.TableFor(kind, rows, namespace == ""))
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "limit to one namespace (default: all)")
	cmd.Flags().StringVarP(&selector, "selector", "l", "", "filter on labels, e.g. app=web,tier=front")
	return cmd
}

func filterByName(rows []k8s.Resource, name string) []k8s.Resource {
	out := make([]k8s.Resource, 0, 1)
	for _, r := range rows {
		if r.Name == name {
			out = append(out, r)
		}
	}
	return out
}

// filterBySelector applies equality-only label matching. Set-based selectors (in, notin)
// are deliberately not faked: silently ignoring them would return the wrong rows.
func filterBySelector(rows []k8s.Resource, selector string) ([]k8s.Resource, error) {
	want := map[string]string{}
	for _, part := range strings.Split(selector, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return nil, usageErrorf(
				"selector %q is not supported — only equality selectors (key=value, comma separated)", part)
		}
		want[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	var out []k8s.Resource
	for _, r := range rows {
		labels := r.LabelsOf()
		match := true
		for k, v := range want {
			got, _ := labels[k].(string)
			if got != v {
				match = false
				break
			}
		}
		if match {
			out = append(out, r)
		}
	}
	return out, nil
}

func newSearchCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Find workloads by name substring",
		Long: `Substring search over synced workload names.

The backend searches Deployment, StatefulSet and DaemonSet names only — pods, services and
other kinds are not matched. Use ` + "`rtz get`" + ` for those.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}
			clusterID, err := e.requireCluster()
			if err != nil {
				return err
			}

			hits, err := e.client.SearchResources(cmd.Context(), orgID, clusterID, args[0], limit)
			if err != nil {
				return err
			}
			t := &render.Table{Headers: []string{"KIND", "NAMESPACE", "NAME"}}
			for _, h := range hits {
				t.Rows = append(t.Rows, []string{h.Kind, h.Namespace, h.Name})
			}

			if len(hits) == limit && !e.printer.Format.Structured() {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: stopped at the --limit of %d; there may be more matches\n", limit)
			}
			return e.printer.Print(hits, t)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "maximum matches to return")
	return cmd
}

func newNamespacesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "namespaces",
		Aliases: []string{"ns"},
		Short:   "List namespaces in the selected cluster",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}
			clusterID, err := e.requireCluster()
			if err != nil {
				return err
			}
			names, err := e.client.Namespaces(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			sort.Strings(names)
			t := &render.Table{Headers: []string{"NAMESPACE"}}
			for _, n := range names {
				t.Rows = append(t.Rows, []string{n})
			}
			return e.printer.Print(names, t)
		},
	}
}

func newCountsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "counts",
		Short: "Count synced resources by kind",
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
			clusterID, err := e.requireCluster()
			if err != nil {
				return err
			}
			counts, err := e.client.Counts(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}

			kinds := make([]string, 0, len(counts))
			for k := range counts {
				kinds = append(kinds, k)
			}
			// Biggest first: on a real cluster this is the interesting order, and ties break
			// by name so repeated runs are stable.
			sort.Slice(kinds, func(i, j int) bool {
				if counts[kinds[i]] != counts[kinds[j]] {
					return counts[kinds[i]] > counts[kinds[j]]
				}
				return kinds[i] < kinds[j]
			})

			t := &render.Table{Headers: []string{"KIND", "COUNT"}}
			var total int64
			for _, k := range kinds {
				t.Rows = append(t.Rows, []string{k, strconv.FormatInt(counts[k], 10)})
				total += counts[k]
			}
			if len(kinds) > 0 {
				t.Rows = append(t.Rows, []string{"TOTAL", strconv.FormatInt(total, 10)})
			}
			return e.printer.Print(counts, t)
		},
	}
}
