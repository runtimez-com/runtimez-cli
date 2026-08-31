package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newLogsCmd() *cobra.Command {
	var (
		since, from, to string
		service         string
		filterQuery     string
		level           string
		limit           int
		follow          bool
		pollInterval    time.Duration
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Search logs across the cluster",
		Long: `Search logs without a kubeconfig.

-q takes the same filter language as the reduction rules: free text, field:value,
@attributes, wildcards, ranges, and AND/OR/NOT. An unparseable query is rejected by the
server rather than silently matching everything.`,
		Example: `  rtz logs --since 15m --level ERROR
  rtz logs -s checkout -q 'status:5* AND @http.method:POST'
  rtz logs --follow --level ERROR`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, _, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			start, end, err := timeWindow(since, from, to)
			if err != nil {
				return err
			}

			req := api.LogSearch{
				StartDate:      api.EpochMs(start),
				EndDate:        api.EpochMs(end),
				ServiceName:    service,
				ClusterID:      clusterID,
				Limit:          limit,
				SeverityFilter: level,
				FilterQuery:    filterQuery,
			}

			if follow {
				if e.printer.Format.Structured() {
					return usageErrorf("--follow streams a table; drop -o for it, or poll without --follow")
				}
				return followLogs(cmd, e, req, pollInterval)
			}

			page, err := e.client.SearchLogs(cmd.Context(), req)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(page.Data, nil)
			}
			fmt.Fprintln(cmd.OutOrStdout(), windowLine(start, end))
			fmt.Fprintln(cmd.OutOrStdout())
			if err := e.printer.Print(page.Data, view.LogsTable(page.Data)); err != nil {
				return err
			}
			if page.Total > int64(len(page.Data)) {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"showing %d of %d matching lines — raise --limit or narrow the window\n",
					len(page.Data), page.Total)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "1h", "look back this far: 15m, 2h, 7d")
	cmd.Flags().StringVar(&from, "from", "", "window start (RFC3339); overrides --since")
	cmd.Flags().StringVar(&to, "to", "", "window end (RFC3339)")
	cmd.Flags().StringVarP(&service, "service", "s", "", "limit to one service")
	cmd.Flags().StringVarP(&filterQuery, "query", "q", "", "filter query (field:value, @attr, AND/OR/NOT)")
	cmd.Flags().StringVar(&level, "level", "", "severity filter, comma separated (ERROR,WARN)")
	cmd.Flags().IntVar(&limit, "limit", 200, "maximum lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "poll for new lines until interrupted")
	cmd.Flags().DurationVar(&pollInterval, "poll", 5*time.Second, "how often --follow polls")
	return cmd
}

// followLogs polls for new lines.
//
// This is a poll, not a stream: the backend has no log-tailing endpoint. Dedupe is by the
// last timestamp seen, and the window start advances with it — without that, every poll
// would reprint the whole window.
func followLogs(cmd *cobra.Command, e *env, req api.LogSearch, interval time.Duration) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(cmd.ErrOrStderr(),
		"following (polling every %s; the API has no log stream) — ctrl-c to stop\n", interval)

	seen := map[string]bool{}
	cursor := req.StartDate

	for {
		req.StartDate = cursor
		req.EndDate = api.EpochMs(time.Now())

		page, err := e.client.SearchLogs(cmd.Context(), req)
		if err != nil {
			return err
		}

		// Results arrive newest-first; print oldest-first so a tail reads like a tail.
		for i := len(page.Data) - 1; i >= 0; i-- {
			r := page.Data[i]
			// The dedupe key has to include the body: two lines from one service can share
			// a timestamp at millisecond resolution, and dropping one would silently hide it.
			key := r.TS + "|" + r.ServiceName + "|" + r.Body
			if seen[key] {
				continue
			}
			seen[key] = true
			fmt.Fprintf(out, "%s  %-5s  %-24s  %s\n",
				r.TS, r.Level, truncateLine(r.ServiceName, 24), r.Body)
		}

		// Bound the dedupe set so a long tail does not grow without limit.
		if len(seen) > 20000 {
			seen = map[string]bool{}
		}
		cursor = req.EndDate

		select {
		case <-cmd.Context().Done():
			return nil
		case <-time.After(interval):
		}
	}
}
