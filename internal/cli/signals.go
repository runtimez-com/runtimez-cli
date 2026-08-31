package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newSignalsCmd() *cobra.Command {
	var from, to string
	var limit int

	cmd := &cobra.Command{
		Use:   "signals",
		Short: "Golden signals: the slowest operations right now",
		Long: `The slowest operations by p99 across the cluster's traced services.

"rtz signals traces" lists the slowest individual traces instead, each with the service the
backend suspects.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			res, err := e.client.SlowOperations(cmd.Context(), orgID, clusterID, from, to, limit)
			if err != nil {
				return err
			}
			if !e.printer.Format.Structured() {
				printWindow(cmd, res.Window)
			}
			return e.printer.Print(res.Items, view.SlowOperationsTable(res.Items))
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "window start (ISO-8601)")
	cmd.Flags().StringVar(&to, "to", "", "window end (ISO-8601)")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum rows")

	traces := &cobra.Command{
		Use:   "traces",
		Short: "The slowest individual traces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			res, err := e.client.SlowTraces(cmd.Context(), orgID, clusterID, from, to, limit)
			if err != nil {
				return err
			}
			if !e.printer.Format.Structured() {
				printWindow(cmd, res.Window)
			}
			return e.printer.Print(res.Items, view.SlowTracesTable(res.Items))
		},
	}
	traces.Flags().StringVar(&from, "from", "", "window start (ISO-8601)")
	traces.Flags().StringVar(&to, "to", "", "window end (ISO-8601)")
	traces.Flags().IntVar(&limit, "limit", 20, "maximum rows")
	cmd.AddCommand(traces)

	return cmd
}

// printWindow states the range the numbers cover. A latency figure without its window is
// not interpretable — "p99 is 400ms" over an hour and over a week are different claims.
func printWindow(cmd *cobra.Command, w *api.SignalWindow) {
	if w == nil || w.From == nil || w.To == nil {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Window: %s → %s\n\n",
		w.From.Format("2006-01-02 15:04 MST"), w.To.Format("2006-01-02 15:04 MST"))
}
