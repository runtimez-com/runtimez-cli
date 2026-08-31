package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newChangesCmd() *cobra.Command {
	var sinceHours, page, size int

	cmd := &cobra.Command{
		Use:   "changes [namespace/name]",
		Short: "What changed in the cluster, newest first",
		Long: `Observed changes to cluster resources — the first question worth asking when
something breaks.

With a workload reference, narrows to that workload's own history.`,
		Example: `  rtz changes --since 24
  rtz changes payments/checkout`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}

			res := (*api.Changes)(nil)
			if len(args) == 1 {
				ns, name, err := splitWorkloadRef(args[0], "")
				if err != nil {
					return err
				}
				res, err = e.client.WorkloadChanges(cmd.Context(), orgID, clusterID, ns, name, page, size)
				if err != nil {
					return err
				}
			} else {
				res, err = e.client.Changes(cmd.Context(), orgID, clusterID, sinceHours, page, size)
				if err != nil {
					return err
				}
			}

			if e.printer.Format.Structured() {
				return e.printer.Print(res, nil)
			}
			if err := e.printer.Print(res.Items, view.ChangesTable(res.Items)); err != nil {
				return err
			}
			// This endpoint pages, so saying which slice you are looking at prevents
			// reading page 1 of 40 as the whole history.
			if res.Total > int64(len(res.Items)) {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"showing %d of %d changes (page %d) — use --page to see more\n",
					len(res.Items), res.Total, res.Page)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&sinceHours, "since", 168, "look back this many hours (cluster-wide listing only)")
	cmd.Flags().IntVar(&page, "page", 0, "page number, zero-based")
	cmd.Flags().IntVar(&size, "size", 20, "rows per page")
	return cmd
}
