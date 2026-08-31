package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newTraceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "trace",
		Aliases: []string{"traces"},
		Short:   "Query distributed traces",
	}
	cmd.AddCommand(newTraceListCmd(), newTraceGetCmd(), newTraceAnalyzeCmd(), newTraceLogsCmd())
	return cmd
}

func newTraceListCmd() *cobra.Command {
	var since, from, to, query string
	var limit int
	var errorsOnly bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List traces in a window",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, _, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			start, end, err := timeWindow(since, from, to)
			if err != nil {
				return err
			}

			page, err := e.client.ListTraces(cmd.Context(), api.TraceListRequest{
				StartDate: api.EpochMs(start), EndDate: api.EpochMs(end),
				Query: query, Limit: limit, ClusterID: clusterID,
				SortBy: "durationNano", SortOrder: "desc",
			})
			if err != nil {
				return err
			}

			rows := page.Data
			if errorsOnly {
				var kept []api.TraceRow
				for _, r := range rows {
					if r.HasError {
						kept = append(kept, r)
					}
				}
				rows = kept
			}

			if e.printer.Format.Structured() {
				return e.printer.Print(rows, nil)
			}
			fmt.Fprintln(cmd.OutOrStdout(), windowLine(start, end))
			fmt.Fprintln(cmd.OutOrStdout())
			return e.printer.Print(rows, view.TracesTable(rows))
		},
	}

	cmd.Flags().StringVar(&since, "since", "1h", "look back this far: 15m, 2h, 7d")
	cmd.Flags().StringVar(&from, "from", "", "window start (RFC3339)")
	cmd.Flags().StringVar(&to, "to", "", "window end (RFC3339)")
	cmd.Flags().StringVarP(&query, "query", "q", "", "trace filter query")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum traces")
	cmd.Flags().BoolVar(&errorsOnly, "errors", false, "only traces with an error")
	return cmd
}

func newTraceGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <trace-id>",
		Short: "Show one trace's spans as a tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, _, _, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			data, err := e.client.Trace(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(data, nil)
			}
			printSpanTree(cmd, args[0], data)
			return nil
		},
	}
}

// span is the subset of a span row the tree needs.
type span struct {
	id       string
	parentID string
	name     string
	service  string
	duration float64
	hasError bool
	ts       string
}

// printSpanTree renders parent/child nesting.
//
// The payload is an open map whose span list key varies, so the shape is discovered rather
// than assumed — a hard-coded key that changes server-side would print an empty trace and
// look like the trace itself was empty.
func printSpanTree(cmd *cobra.Command, traceID string, data map[string]any) {
	out := cmd.OutOrStdout()
	spans := extractSpans(data)
	if len(spans) == 0 {
		fmt.Fprintf(out, "Trace %s returned no spans this client could read.\n", traceID)
		fmt.Fprintln(out, "Use -o json to see the raw payload.")
		return
	}

	fmt.Fprintf(out, "Trace %s — %d spans\n\n", traceID, len(spans))

	children := map[string][]span{}
	byID := map[string]bool{}
	for _, s := range spans {
		byID[s.id] = true
	}
	var roots []span
	for _, s := range spans {
		// A span whose parent is absent from this trace is a root for display purposes;
		// otherwise a partial trace would render as nothing at all.
		if s.parentID == "" || !byID[s.parentID] {
			roots = append(roots, s)
		} else {
			children[s.parentID] = append(children[s.parentID], s)
		}
	}
	sortSpans(roots)
	for _, r := range roots {
		printSpan(out, r, children, 0)
	}
}

func sortSpans(s []span) {
	sort.SliceStable(s, func(i, j int) bool { return s[i].ts < s[j].ts })
}

func printSpan(out interface{ Write([]byte) (int, error) }, s span, children map[string][]span, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}
	// The error marker goes in a trailing column, not the leading one: a leading space for
	// "no error" makes a root span look indented, which is the one thing this tree exists
	// to communicate.
	mark := ""
	if s.hasError {
		mark = "ERROR"
	}
	fmt.Fprintf(out, "%s%-*s  %-22s %8.2fms  %s\n",
		indent, 40-len(indent), truncateLine(s.name, 40-len(indent)),
		truncateLine(s.service, 22), s.duration, mark)

	kids := children[s.id]
	sortSpans(kids)
	for _, c := range kids {
		printSpan(out, c, children, depth+1)
	}
}

// extractSpans finds the span list wherever the payload keeps it.
func extractSpans(data map[string]any) []span {
	var raw []any
	for _, key := range []string{"spans", "data", "items", "result"} {
		if v, ok := data[key].([]any); ok && len(v) > 0 {
			raw = v
			break
		}
	}
	if raw == nil {
		// Some payloads nest one level deeper.
		for _, v := range data {
			if l, ok := v.([]any); ok && len(l) > 0 {
				if _, isMap := l[0].(map[string]any); isMap {
					raw = l
					break
				}
			}
		}
	}

	out := make([]span, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		s := span{
			id:       firstString(m, "id", "spanID", "spanId"),
			parentID: firstString(m, "parentId", "parentSpanID", "parentSpanId"),
			name:     firstString(m, "name", "spanName", "operation"),
			service:  firstString(m, "serviceName", "service"),
			ts:       firstString(m, "timestamp", "ts", "startTime"),
			hasError: firstBool(m, "hasError", "isError"),
		}
		// durationNano and duration are both seen; normalise to milliseconds either way.
		if v, ok := firstFloat(m, "durationNano"); ok {
			s.duration = v / 1e6
		} else if v, ok := firstFloat(m, "duration", "durationMs", "durationMillis"); ok {
			s.duration = v
		}
		if s.id != "" || s.name != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func firstFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		}
	}
	return 0, false
}

func firstBool(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if v, ok := m[k].(bool); ok {
			return v
		}
	}
	return false
}

func newTraceAnalyzeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "analyze <trace-id>",
		Short: "Ask the backend to interpret one trace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, _, _, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			res, err := e.client.AnalyzeTrace(cmd.Context(), args[0], "")
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(res, nil)
			}
			printLooseMap(cmd, res)
			return nil
		},
	}
}

func newTraceLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <trace-id>",
		Short: "Show the logs correlated to one trace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, _, _, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			records, err := e.client.LogsForTrace(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return e.printer.Print(records, view.LogsTable(records))
		},
	}
}

// printLooseMap renders an open-ended payload as key/value rows, sorted so repeated runs
// are diffable.
func printLooseMap(cmd *cobra.Command, m map[string]any) {
	t := &render.Table{Headers: []string{"FIELD", "VALUE"}, EmptyMessage: "Nothing returned."}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Rows = append(t.Rows, []string{k, truncateLine(fmt.Sprintf("%v", m[k]), 100)})
	}
	_ = (&render.Printer{Out: cmd.OutOrStdout(), Format: render.FormatTable}).Print(m, t)
}
