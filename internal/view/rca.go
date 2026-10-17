package view

import (
	"fmt"
	"io"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// WorkloadRCA renders one workload's root-cause evidence.
//
// Lifted out of internal/cli so `rtz rca` and the TUI's :rca screen are one definition
// rather than two that drift — the same reason internal/view exists at all.
func WorkloadRCA(w io.Writer, r *api.WorkloadRCA) {
	if r == nil {
		fmt.Fprintln(w, "no root-cause data for this workload")
		return
	}

	fmt.Fprintf(w, "%s/%s (%s)\n", r.Namespace, r.Name, render.Dash(r.Kind))
	fmt.Fprintf(w, "  Restarts:    %d\n", r.RestartCount)
	if r.K8sReason != "" {
		fmt.Fprintf(w, "  Reason:      %s\n", r.K8sReason)
	}
	if r.OOMKilled {
		fmt.Fprintln(w, "  OOMKilled:   yes")
	}
	if r.ExitCode != nil {
		fmt.Fprintf(w, "  Exit code:   %d\n", *r.ExitCode)
	}
	if r.TerminatedAt != "" {
		fmt.Fprintf(w, "  Terminated:  %s\n", r.TerminatedAt)
	}
	if r.TerminationSummary != "" {
		fmt.Fprintf(w, "  Summary:     %s\n", r.TerminationSummary)
	}

	// App health is only meaningful when traces reached the backend; printing 0% error and
	// 0ms p99 for a workload with no telemetry would read as a perfectly healthy service.
	if r.AppHealthAvailable {
		fmt.Fprintf(w, "  Requests:    %d, %.2f%% errors, p99 %dms\n",
			r.RequestCount, r.ErrorRatePct, r.P99LatencyMs)
	} else {
		fmt.Fprintln(w, "  App health:  no trace data for this workload")
	}

	if len(r.WarningEvents) > 0 {
		fmt.Fprintf(w, "\nWarning events (%d)\n", len(r.WarningEvents))
		for _, ev := range r.WarningEvents {
			fmt.Fprintf(w, "  %s  %s  %s\n",
				render.Dash(ev.LastTimestamp), render.Dash(ev.Reason), truncate(ev.Message, 70))
		}
	}

	fmt.Fprintf(w, "\nLog tail (%d lines)\n", len(r.LogTailLines))
	if !r.TermLogsAvailable {
		fmt.Fprintln(w, "  termination logs not collected for this workload")
	}
	for _, l := range r.LogTailLines {
		fmt.Fprintf(w, "  %s\n", truncate(l, 100))
	}

	if len(r.SpansInFlight) > 0 {
		fmt.Fprintf(w, "\nSpans in flight at failure (%d)\n", len(r.SpansInFlight))
		for _, s := range r.SpansInFlight {
			fmt.Fprintf(w, "  %s  %s  %s  %dms\n",
				s.TraceID, truncate(s.SpanName, 40), render.Dash(s.StatusCode), s.DurationMs)
		}
	}

	rcaEdges(w, "Upstream", r.Upstream)
	rcaEdges(w, "Downstream", r.Downstream)
}

func rcaEdges(w io.Writer, title string, edges []api.DependencyEdge) {
	if len(edges) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s (%d)\n", title, len(edges))
	for _, e := range edges {
		fmt.Fprintf(w, "  %s/%s %s  %d bytes\n",
			render.Dash(e.Namespace), render.Dash(e.OwnerName), render.Dash(e.OwnerKind), e.Bytes)
	}
}

// WorkloadRCAText is the string form, for the TUI's body pane.
func WorkloadRCAText(r *api.WorkloadRCA) string {
	var b strings.Builder
	WorkloadRCA(&b, r)
	return b.String()
}
