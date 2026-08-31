package view

import (
	"strconv"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// LogsTable renders log lines. The body is deliberately last and unclipped in wide mode —
// the message is the reason anyone ran the command.
func LogsTable(records []api.LogRecord) *render.Table {
	t := &render.Table{
		Headers:      []string{"TIME", "LEVEL", "SERVICE", "MESSAGE"},
		WideHeaders:  []string{"TRACE ID", "EXCEPTION"},
		EmptyMessage: "No log lines match this query in this window.",
	}
	for _, r := range records {
		t.Rows = append(t.Rows, []string{
			shortTime(r.TS), render.Dash(r.Level), render.Dash(r.ServiceName), truncate(r.Body, 100),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(r.TraceID), render.Dash(r.ExceptionType)})
	}
	return t
}

// TracesTable renders the trace index.
func TracesTable(rows []api.TraceRow) *render.Table {
	t := &render.Table{
		Headers:      []string{"TRACE ID", "SERVICE", "OPERATION", "DURATION", "ERR", "STARTED"},
		WideHeaders:  []string{"STATUS", "SPAN ID"},
		EmptyMessage: "No traces match this query in this window.",
	}
	for _, r := range rows {
		errMark := ""
		if r.HasError {
			errMark = "yes"
		}
		t.Rows = append(t.Rows, []string{
			r.TraceID, render.Dash(r.ServiceName), truncate(r.Name, 45),
			ms(r.DurationMs()), render.Dash(errMark), shortTime(r.Timestamp),
		})
		t.WideRows = append(t.WideRows, []string{strconv.Itoa(r.StatusCode), render.Dash(r.SpanID)})
	}
	return t
}

// MetricsTable renders a time-series result as one row per series, since a terminal table
// cannot show a curve. The last value plus the range is what a table can honestly convey.
func MetricsTable(res *api.MetricsResult) *render.Table {
	t := &render.Table{
		Headers:      []string{"SERIES", "LAST", "MIN", "MAX", "POINTS"},
		EmptyMessage: "No data for this metric in this window.",
	}
	if res == nil {
		return t
	}
	unit := res.EffectiveUnit
	for _, s := range res.Series {
		if len(s.Points) == 0 {
			continue
		}
		last := s.Points[len(s.Points)-1].V
		min, max := s.Points[0].V, s.Points[0].V
		for _, p := range s.Points {
			if p.V < min {
				min = p.V
			}
			if p.V > max {
				max = p.V
			}
		}
		label := s.Label
		if label == "" {
			label = groupLabel(s.GroupKey)
		}
		t.Rows = append(t.Rows, []string{
			render.Dash(label), num(last, unit), num(min, unit), num(max, unit), strconv.Itoa(len(s.Points)),
		})
	}
	return t
}

func groupLabel(key map[string]string) string {
	if len(key) == 0 {
		return "all"
	}
	parts := make([]string, 0, len(key))
	for k, v := range key {
		parts = append(parts, k+"="+v)
	}
	sortStrings(parts)
	return strings.Join(parts, ",")
}

func num(v float64, unit string) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if len(s) > 10 {
		s = strconv.FormatFloat(v, 'f', 3, 64)
	}
	if unit != "" {
		return s + " " + unit
	}
	return s
}

// shortTime trims an ISO timestamp to what fits a terminal column without losing the part
// that distinguishes one line from the next.
func shortTime(ts string) string {
	if ts == "" {
		return "<unknown>"
	}
	if i := strings.IndexByte(ts, 'T'); i > 0 && len(ts) > i+9 {
		return ts[i+1 : i+9]
	}
	if len(ts) > 19 {
		return ts[:19]
	}
	return ts
}
