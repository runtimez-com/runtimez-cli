package view

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/k8s"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// RiskTable lists scored workloads, worst first. Score is 0-100 and higher is worse.
func RiskTable(items []api.WorkloadRiskItem) *render.Table {
	sorted := append([]api.WorkloadRiskItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })

	t := &render.Table{
		Headers:      []string{"NAMESPACE", "NAME", "KIND", "SCORE", "LEVEL", "TOP FACTOR"},
		WideHeaders:  []string{"FACTORS", "CATEGORY"},
		EmptyMessage: "No workloads scored.",
	}
	for _, w := range sorted {
		top, category := "", ""
		if len(w.Factors) > 0 {
			top, category = w.Factors[0].Title, w.Factors[0].Category
		}
		t.Rows = append(t.Rows, []string{
			w.Namespace, w.Name, render.Dash(w.Kind),
			strconv.Itoa(w.Score), render.Dash(w.Level), render.Dash(top),
		})
		t.WideRows = append(t.WideRows, []string{strconv.Itoa(len(w.Factors)), render.Dash(category)})
	}
	return t
}

// FindingsTable lists policy or compliance findings, worst first.
func FindingsTable(findings []api.Finding) *render.Table {
	sorted := append([]api.Finding(nil), findings...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, _ := api.SeverityRank(sorted[i].Severity)
		b, _ := api.SeverityRank(sorted[j].Severity)
		return a > b
	})

	t := &render.Table{
		Headers:      []string{"SEVERITY", "RULE", "TITLE", "RESOURCE"},
		WideHeaders:  []string{"CATEGORY", "RECOMMENDATION"},
		EmptyMessage: "No findings.",
	}
	for _, f := range sorted {
		t.Rows = append(t.Rows, []string{
			render.Dash(f.Severity), render.Dash(f.RuleID),
			truncate(f.Title, 60), render.Dash(f.ResourceName),
		})
		t.WideRows = append(t.WideRows, []string{
			render.Dash(f.Category), truncate(f.Recommendation, 60),
		})
	}
	return t
}

// CveTable lists vulnerabilities, worst first, then by whether a fix exists — an unfixable
// critical and a one-bump critical are different work.
func CveTable(findings []api.CveFinding) *render.Table {
	sorted := append([]api.CveFinding(nil), findings...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, _ := api.SeverityRank(sorted[i].Severity)
		b, _ := api.SeverityRank(sorted[j].Severity)
		if a != b {
			return a > b
		}
		return (sorted[i].FixedVersion != "") && (sorted[j].FixedVersion == "")
	})

	t := &render.Table{
		Headers:      []string{"SEVERITY", "CVE", "PACKAGE", "INSTALLED", "FIXED IN", "RESOURCE"},
		WideHeaders:  []string{"IMAGE", "TITLE"},
		EmptyMessage: "No vulnerabilities.",
	}
	for _, f := range sorted {
		fixed := f.FixedVersion
		if fixed == "" {
			fixed = "<no fix>"
		}
		t.Rows = append(t.Rows, []string{
			render.Dash(f.Severity), f.CveID, render.Dash(f.PackageName),
			render.Dash(f.InstalledVersion), fixed, render.Dash(f.ResourceName),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(f.Target), truncate(f.Title, 60)})
	}
	return t
}

// SlowOperationsTable lists operations by p99.
func SlowOperationsTable(items []api.SlowOperation) *render.Table {
	t := &render.Table{
		Headers:      []string{"SERVICE", "OPERATION", "P99", "P50", "CALLS", "ERR%"},
		WideHeaders:  []string{"NAMESPACE", "KIND", "SPAN KIND"},
		EmptyMessage: "No traced operations in this window.",
	}
	for _, o := range items {
		t.Rows = append(t.Rows, []string{
			render.Dash(o.ServiceName), truncate(o.Operation, 50),
			ms(o.P99Ms), ms(o.P50Ms),
			strconv.FormatInt(o.CallCount, 10), pct(o.ErrorRatePct),
		})
		t.WideRows = append(t.WideRows, []string{
			render.Dash(o.Namespace), render.Dash(o.WorkloadKind), render.Dash(o.SpanKind),
		})
	}
	return t
}

// SlowTracesTable lists individual slow traces.
func SlowTracesTable(items []api.SlowTrace) *render.Table {
	t := &render.Table{
		Headers:      []string{"TRACE ID", "ROOT", "TOTAL", "SERVICES", "ERR", "SUSPECT"},
		WideHeaders:  []string{"WALL CLOCK", "STARTED"},
		EmptyMessage: "No traces in this window.",
	}
	for _, s := range items {
		root := s.RootService
		if s.RootOperation != "" {
			root += " " + s.RootOperation
		}
		errMark := ""
		if s.HasError {
			errMark = "yes"
		}
		suspect := s.SuspectService
		if s.SuspectNamespace != "" {
			suspect = s.SuspectNamespace + "/" + suspect
		}
		t.Rows = append(t.Rows, []string{
			s.TraceID, truncate(root, 45), ms(s.TotalMs),
			strconv.FormatInt(s.ServiceCount, 10), render.Dash(errMark), render.Dash(suspect),
		})
		t.WideRows = append(t.WideRows, []string{ms(s.WallClockMs), render.Dash(s.StartTime)})
	}
	return t
}

// ChangesTable lists change events, newest first as the API returns them.
func ChangesTable(items []api.ChangeEvent) *render.Table {
	t := &render.Table{
		Headers:      []string{"WHEN", "TYPE", "KIND", "NAMESPACE", "NAME", "SUMMARY"},
		WideHeaders:  []string{"FIELD MANAGER", "REVISION"},
		EmptyMessage: "No changes recorded in this window.",
	}
	for _, c := range items {
		when := "<unknown>"
		if c.ChangedAt != nil {
			when = ageOf(*c.ChangedAt)
		}
		summary := c.ChangeSummary
		if summary == "" && len(c.FieldChanges) > 0 {
			summary = fmt.Sprintf("%d field(s): %s", len(c.FieldChanges), c.FieldChanges[0].Path)
		}
		rev := c.Revision
		if c.PrevRevision != "" {
			rev = c.PrevRevision + " -> " + c.Revision
		}
		t.Rows = append(t.Rows, []string{
			when, render.Dash(c.ChangeType), render.Dash(c.Kind),
			render.Dash(c.Namespace), c.Name, truncate(summary, 50),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(c.FieldManager), render.Dash(rev)})
	}
	return t
}

func ms(v float64) string {
	if v >= 1000 {
		return strconv.FormatFloat(v/1000, 'f', 2, 64) + "s"
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + "ms"
}

func pct(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64) + "%"
}

func ageOf(t time.Time) string { return k8s.Duration(time.Since(t)) }
