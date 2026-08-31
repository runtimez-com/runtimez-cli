// Package view builds the presentation of API data: which columns a kind gets, and how a
// workload's detail reads.
//
// It exists so the flag commands and the TUI cannot drift. Both are renderers over the same
// column definitions here; a column added for one appears in the other by construction
// rather than by remembering to.
package view

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/k8s"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// tableFor picks the column set for a kind. Anything without a dedicated renderer gets the
// generic one rather than no output.
func TableFor(kind string, rows []k8s.Resource, showNamespace bool) *render.Table {
	switch kind {
	case "Pod":
		return podTable(rows, showNamespace)
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		return workloadTable(rows, showNamespace)
	case "Service":
		return serviceTable(rows, showNamespace)
	case "Ingress":
		return ingressTable(rows, showNamespace)
	case "Node":
		return nodeTable(rows)
	default:
		return genericTable(rows, showNamespace)
	}
}

// nsCols prepends the NAMESPACE column only when the listing spans namespaces.
func nsCols(showNamespace bool, headers ...string) []string {
	if showNamespace {
		return append([]string{"NAMESPACE"}, headers...)
	}
	return headers
}

func nsRow(showNamespace bool, namespace string, cells ...string) []string {
	if showNamespace {
		return append([]string{namespace}, cells...)
	}
	return cells
}

func podTable(rows []k8s.Resource, showNS bool) *render.Table {
	t := &render.Table{
		Headers:     nsCols(showNS, "NAME", "STATUS", "RESTARTS", "AGE"),
		WideHeaders: []string{"NODE", "CONTROLLED BY"},
	}
	for _, r := range rows {
		p := k8s.ParsePod(r)
		t.Rows = append(t.Rows, nsRow(showNS, p.Namespace,
			p.Name, p.Phase, strconv.Itoa(p.Restarts), k8s.Age(p.CreatedAt)))
		owner := "<none>"
		if p.OwnerKind != "" {
			owner = p.OwnerKind + "/" + p.OwnerName
		}
		t.WideRows = append(t.WideRows, []string{render.Dash(p.NodeName), owner})
	}
	return t
}

func workloadTable(rows []k8s.Resource, showNS bool) *render.Table {
	t := &render.Table{
		Headers:     nsCols(showNS, "NAME", "READY", "AGE"),
		WideHeaders: []string{"KIND", "IMAGE"},
	}
	for _, r := range rows {
		w := k8s.ParseWorkload(r)
		t.Rows = append(t.Rows, nsRow(showNS, w.Namespace, w.Name, w.ReadyRatio(), k8s.Age(w.CreatedAt)))
		t.WideRows = append(t.WideRows, []string{w.Kind, render.Dash(w.Image)})
	}
	return t
}

func serviceTable(rows []k8s.Resource, showNS bool) *render.Table {
	t := &render.Table{
		Headers: nsCols(showNS, "NAME", "TYPE", "CLUSTER-IP", "EXTERNAL-IP", "PORTS", "AGE"),
	}
	for _, r := range rows {
		s := k8s.ParseService(r)
		t.Rows = append(t.Rows, nsRow(showNS, s.Namespace,
			s.Name, s.Type, s.ClusterIP, render.Dash(s.ExternalIP), render.Dash(s.Ports), k8s.Age(s.CreatedAt)))
	}
	return t
}

func ingressTable(rows []k8s.Resource, showNS bool) *render.Table {
	t := &render.Table{
		Headers:     nsCols(showNS, "NAME", "CLASS", "HOSTS", "AGE"),
		WideHeaders: []string{"PATHS"},
	}
	for _, r := range rows {
		i := k8s.ParseIngress(r)
		t.Rows = append(t.Rows, nsRow(showNS, i.Namespace,
			i.Name, render.Dash(i.Class), render.Dash(strings.Join(i.Hosts, ",")), k8s.Age(i.CreatedAt)))
		t.WideRows = append(t.WideRows, []string{render.Dash(strings.Join(i.Paths, " "))})
	}
	return t
}

func nodeTable(rows []k8s.Resource) *render.Table {
	t := &render.Table{
		Headers:     []string{"NAME", "STATUS", "ROLES", "AGE", "VERSION"},
		WideHeaders: []string{"INTERNAL-IP", "OS", "KERNEL", "RUNTIME"},
	}
	for _, r := range rows {
		n := k8s.ParseNode(r)
		t.Rows = append(t.Rows, []string{
			n.Name, n.NodeStatus(), strings.Join(n.Roles, ","), k8s.Age(n.CreatedAt), render.Dash(n.Version),
		})
		t.WideRows = append(t.WideRows, []string{
			render.Dash(n.InternalIP), render.Dash(n.OS), render.Dash(n.Kernel), render.Dash(n.Runtime),
		})
	}
	return t
}

func genericTable(rows []k8s.Resource, showNS bool) *render.Table {
	t := &render.Table{
		Headers:     nsCols(showNS, "KIND", "NAME", "AGE"),
		WideHeaders: []string{"CONTROLLED BY", "UID"},
	}
	for _, r := range rows {
		t.Rows = append(t.Rows, nsRow(showNS, r.Namespace, r.Kind, r.Name, k8s.Age(r.CreatedAt)))
		owner := "<none>"
		if r.OwnerKind != "" {
			owner = r.OwnerKind + "/" + r.OwnerName
		}
		t.WideRows = append(t.WideRows, []string{owner, render.Dash(r.UID)})
	}
	return t
}

func Detail(w io.Writer, d *api.WorkloadDetail) {
	wl := k8s.ParseWorkload(*d.Workload)

	fmt.Fprintf(w, "%s/%s\n", wl.Namespace, wl.Name)
	fmt.Fprintf(w, "  Kind:     %s\n", wl.Kind)
	fmt.Fprintf(w, "  Ready:    %s\n", wl.ReadyRatio())
	fmt.Fprintf(w, "  Image:    %s\n", render.Dash(wl.Image))
	fmt.Fprintf(w, "  Age:      %s\n", k8s.Age(wl.CreatedAt))
	if labels := d.Workload.LabelsOf(); len(labels) > 0 {
		fmt.Fprintf(w, "  Labels:   %s\n", formatLabels(labels))
	}

	section(w, "Pods", len(d.Pods))
	if len(d.Pods) > 0 {
		t := &render.Table{Headers: []string{"NAME", "STATUS", "RESTARTS", "NODE", "AGE"}}
		for _, row := range d.Pods {
			p := k8s.ParsePod(row)
			t.Rows = append(t.Rows, []string{
				p.Name, p.Phase, strconv.Itoa(p.Restarts), render.Dash(p.NodeName), k8s.Age(p.CreatedAt),
			})
		}
		printIndented(w, t)
	}

	section(w, "Events", len(d.Events))
	if len(d.Events) > 0 {
		t := &render.Table{Headers: []string{"TYPE", "REASON", "COUNT", "AGE", "MESSAGE"}}
		for _, ev := range d.Events {
			t.Rows = append(t.Rows, []string{
				render.Dash(ev.Type), render.Dash(ev.Reason), strconv.Itoa(ev.Count),
				k8s.Age(ev.When()), truncate(ev.Message, 80),
			})
		}
		printIndented(w, t)
	}

	relatedSection(w, "Services", d.Services, func(r k8s.Resource) []string {
		s := k8s.ParseService(r)
		return []string{s.Name, s.Type, s.ClusterIP, render.Dash(s.Ports)}
	}, []string{"NAME", "TYPE", "CLUSTER-IP", "PORTS"})

	relatedSection(w, "Ingresses", d.Ingresses, func(r k8s.Resource) []string {
		i := k8s.ParseIngress(r)
		return []string{i.Name, render.Dash(i.Class), render.Dash(strings.Join(i.Hosts, ","))}
	}, []string{"NAME", "CLASS", "HOSTS"})

	relatedSection(w, "PersistentVolumeClaims", d.PVCs, func(r k8s.Resource) []string {
		return []string{r.Name, k8s.Age(r.CreatedAt)}
	}, []string{"NAME", "AGE"})

	relatedSection(w, "ReplicaSets", d.ReplicaSets, func(r k8s.Resource) []string {
		rs := k8s.ParseWorkload(r)
		return []string{rs.Name, rs.ReadyRatio(), k8s.Age(rs.CreatedAt)}
	}, []string{"NAME", "READY", "AGE"})
}

func relatedSection(w io.Writer, title string, rows []k8s.Resource,
	cells func(k8s.Resource) []string, headers []string) {

	section(w, title, len(rows))
	if len(rows) == 0 {
		return
	}
	t := &render.Table{Headers: headers}
	for _, r := range rows {
		t.Rows = append(t.Rows, cells(r))
	}
	printIndented(w, t)
}

// section prints a heading that states the count even when it is zero — "Events (0)" is a
// finding, whereas an omitted section reads as "not checked".
func section(w io.Writer, title string, n int) {
	fmt.Fprintf(w, "\n%s (%d)\n", title, n)
}

// printIndented renders a table indented under its heading.
func printIndented(w io.Writer, t *render.Table) {
	var buf strings.Builder
	_ = (&render.Printer{Out: &buf, Format: render.FormatTable}).Print(nil, t)
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
}

func formatLabels(labels map[string]any) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	// Map iteration order is random; sorting keeps repeated runs diffable.
	sortStrings(parts)
	return strings.Join(parts, ",")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
