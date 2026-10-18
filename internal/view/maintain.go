package view

import (
	"sort"
	"strconv"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// MaintainItemsTable lists lifecycle items, soonest deadline first. Items with no known
// deadline sort last: an unknown deadline is not a far one, but it is not the next one either.
func MaintainItemsTable(items []api.MaintainItem) *render.Table {
	rows := append([]api.MaintainItem(nil), items...)
	sort.SliceStable(rows, func(i, j int) bool { return daysKey(rows[i].DaysLeft) < daysKey(rows[j].DaysLeft) })
	t := &render.Table{
		Headers:      []string{"CLUSTER", "TYPE", "ITEM", "VERSION", "DEADLINE", "DAYS LEFT", "SUPPORT", "WHERE"},
		WideHeaders:  []string{"KIND", "CONFIDENCE", "OWNER", "PLATFORM", "SOURCE"},
		EmptyMessage: "No Maintain items.",
	}
	for _, it := range rows {
		platform := "no"
		if it.Platform {
			platform = "yes"
		}
		t.Rows = append(t.Rows, []string{
			render.Dash(it.ClusterName), render.Dash(it.ItemType), render.Dash(it.DisplayName),
			render.Dash(it.Version), render.Dash(it.Deadline), Days(it.DaysLeft),
			render.Dash(it.SupportState), render.Dash(it.Location),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(it.DeadlineKind), render.Dash(it.Confidence),
			render.Dash(it.Owner), platform, render.Dash(it.SourceURL)})
	}
	return t
}

func daysKey(d *int) int {
	if d == nil {
		return 1 << 30
	}
	return *d
}

// InventoryTable lists running packages, most-used first (the backend's order).
func InventoryTable(inv *api.MaintainInventory) *render.Table {
	t := &render.Table{
		Headers:      []string{"ECOSYSTEM", "PACKAGE", "VERSIONS", "WORKLOADS", "PODS", "EOL", "DAYS LEFT"},
		EmptyMessage: "No packages match.",
	}
	for _, it := range inv.Items {
		v := strings.Join(it.Versions, ",")
		if it.VersionsCapped {
			v += ",…"
		}
		eol, days := "<none>", "<none>"
		if it.EOL != nil {
			eol = strings.TrimSpace(it.EOL.SupportState + " " + it.EOL.Deadline)
			days = Days(it.EOL.DaysLeft)
		}
		t.Rows = append(t.Rows, []string{
			render.Dash(it.Ecosystem), it.Name, render.Dash(truncate(v, 40)),
			strconv.Itoa(it.Workloads), strconv.Itoa(it.Pods), render.Dash(eol), days,
		})
	}
	return t
}

// DependentsTable lists the workloads that connect to a data service.
func DependentsTable(d *api.DataServiceDependents) *render.Table {
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "KIND", "NAME", "VIA", "PORT", "SOCKETS", "LAST SEEN"},
		WideHeaders:  []string{"ROLE", "POOLER", "CLUSTER"},
		EmptyMessage: "No dependents observed.",
	}
	for _, x := range d.Dependents {
		t.Rows = append(t.Rows, []string{
			x.Namespace, x.Kind, x.Name, render.Dash(x.Via), strconv.Itoa(x.Port),
			strconv.FormatInt(x.Sockets, 10), Since(x.LastSeen),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(x.Role), render.Dash(x.Pooler), render.Dash(x.ClusterName)})
	}
	return t
}

// ImpactTable lists each dependent's verdict for the upgrade, worst first.
func ImpactTable(im *api.DataServiceImpact) *render.Table {
	rows := append(im.Dependents[:0:0], im.Dependents...)
	sort.SliceStable(rows, func(i, j int) bool { return verdictRank(rows[i].Verdict) > verdictRank(rows[j].Verdict) })
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "KIND", "NAME", "VERDICT", "DRIVERS", "WHY"},
		WideHeaders:  []string{"VIA", "CLUSTER"},
		EmptyMessage: "No dependents observed.",
	}
	for _, x := range rows {
		drivers := make([]string, 0, len(x.Drivers))
		for _, d := range x.Drivers {
			drivers = append(drivers, strings.TrimSpace(d.Driver+" "+d.Version))
		}
		t.Rows = append(t.Rows, []string{
			x.Namespace, x.Kind, x.Name, render.Dash(x.Verdict),
			render.Dash(truncate(strings.Join(drivers, ", "), 40)), render.Dash(truncate(x.Reason, 80)),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(strings.Join(x.Via, ",")), render.Dash(x.ClusterName)})
	}
	return t
}

// verdictRank orders impact verdicts so the ones needing action come first. UNKNOWN ranks
// above READY: not knowing whether an app survives is not a pass.
func verdictRank(v string) int {
	switch strings.ToUpper(v) {
	case "BUMP_DRIVER":
		return 3
	case "UNKNOWN":
		return 2
	case "REVIEW":
		return 1
	}
	return 0
}
