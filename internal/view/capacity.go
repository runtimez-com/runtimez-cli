package view

import (
	"sort"
	"strconv"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// CapacitySections are the detail sections `rtz capacity --section` accepts. The names match
// the MCP runtimez_capacity_summary tool's, so the same word works in both places.
var CapacitySections = []string{"pools", "nodes", "blocked", "storage", "network", "batch", "special"}

// HeadroomTable is one row per axis (cpu, memory, pods).
func HeadroomTable(c *api.Capacity) *render.Table {
	t := &render.Table{
		Headers:      []string{"AXIS", "ALLOCATABLE", "REQUESTED", "REQ %", "USED", "USED %", "HEADROOM", "LARGEST FIT"},
		WideHeaders:  []string{"LARGEST FIT NODE"},
		EmptyMessage: "No capacity axes reported.",
	}
	for _, a := range c.Headroom {
		f := axisFormatter(a.Unit)
		t.Rows = append(t.Rows, []string{
			a.Axis, f(a.Allocatable), f(a.Requested), Pct(a.RequestsCommitmentPct),
			f(a.Used), Pct(a.UsedPct), f(a.Headroom), f(a.LargestFit),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(a.LargestFitNode)})
	}
	return t
}

func axisFormatter(unit string) func(*int64) string {
	switch strings.ToLower(unit) {
	case "millicores":
		return CPU
	case "bytes":
		return Bytes
	default:
		return Int64
	}
}

// PoolsTable is one row per node pool.
func PoolsTable(pools []api.CapacityPool) *render.Table {
	t := &render.Table{
		Headers: []string{"POOL", "SOURCE", "NODES", "CPU REQ %", "MEM REQ %", "CPU USED %", "MEM USED %",
			"CPU FREE", "MEM FREE", "PODS", "CAN GROW"},
		WideHeaders:  []string{"AUTOSCALER", "NODE CEILING", "GROW REASON"},
		EmptyMessage: "No node pools.",
	}
	for _, p := range pools {
		grow, reason := unknown, ""
		if g := p.RoomToGrow; g != nil {
			reason = g.Reason
			switch {
			case g.AtCeiling:
				grow = "at ceiling"
			case g.Nodes != nil:
				grow = "+" + strconv.FormatInt(*g.Nodes, 10) + " nodes"
			}
		}
		t.Rows = append(t.Rows, []string{
			p.Name, render.Dash(p.Source), strconv.Itoa(p.Nodes),
			Pct(p.CPURequestsCommitmentPct), Pct(p.MemoryRequestsCommitmentPct),
			Pct(p.CPUUsedPct), Pct(p.MemoryUsedPct),
			CPU(resCPU(p.Headroom)), Bytes(resMem(p.Headroom)),
			strconv.Itoa(p.Pods) + "/" + Int64(p.AllocatablePods), grow,
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(p.AutoscalerState), Int64(p.NodeCeiling), render.Dash(reason)})
	}
	return t
}

// CapacityNodesTable is one row per node.
func CapacityNodesTable(c *api.Capacity) *render.Table {
	t := &render.Table{
		Headers:      []string{"NODE", "POOL", "ZONE", "TYPE", "CAPACITY", "PODS", "CPU REQ", "CPU FREE", "MEM REQ", "MEM FREE"},
		WideHeaders:  []string{"CPU USED", "MEM USED", "EXCLUDED"},
		EmptyMessage: "No nodes.",
	}
	for _, n := range c.Nodes {
		excl := "<none>"
		if !n.Eligible {
			excl = render.Dash(n.Exclusion)
		}
		t.Rows = append(t.Rows, []string{
			n.Name, render.Dash(n.Pool), render.Dash(n.Zone), render.Dash(n.InstanceType), render.Dash(n.CapacityType),
			strconv.Itoa(n.Pods), CPU(resCPU(n.Requested)), CPU(resCPU(n.Headroom)),
			Bytes(resMem(n.Requested)), Bytes(resMem(n.Headroom)),
		})
		t.WideRows = append(t.WideRows, []string{CPU(resCPU(n.Used)), Bytes(resMem(n.Used)), excl})
	}
	return t
}

// BlockedTable lists pods the scheduler could not place, longest-waiting first.
func BlockedTable(c *api.Capacity) *render.Table {
	rows := append(c.Blocked[:0:0], c.Blocked...)
	sort.SliceStable(rows, func(i, j int) bool { return i64(rows[i].PendingSeconds) > i64(rows[j].PendingSeconds) })
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "POD", "OWNER", "PENDING", "CATEGORY", "SHORTFALL", "WHY"},
		WideHeaders:  []string{"NOMINATED", "VERIFY"},
		EmptyMessage: "No pending pods.",
	}
	for _, b := range rows {
		owner := "<none>"
		if b.OwnerKind != "" {
			owner = b.OwnerKind + "/" + b.OwnerName
		}
		short := "no"
		if b.CapacityShortfall {
			short = "yes"
		}
		t.Rows = append(t.Rows, []string{
			b.Namespace, b.Name, owner, Seconds(b.PendingSeconds), render.Dash(b.PrimaryCategory), short,
			render.Dash(truncate(b.Message, 80)),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(b.NominatedNode), render.Dash(b.VerifyCommand)})
	}
	return t
}

// StorageTable lists PVCs, fullest first, then pending claims.
func StorageTable(c *api.Capacity) *render.Table {
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "CLAIM", "PHASE", "CLASS", "SIZE", "USED", "USED %", "DAYS TO FULL", "MOUNTED BY"},
		WideHeaders:  []string{"FORECAST", "USAGE NOTE"},
		EmptyMessage: "No persistent volume claims.",
	}
	if c.Storage == nil {
		return t
	}
	pvcs := append(c.Storage.PVCs[:0:0], c.Storage.PVCs...)
	sort.SliceStable(pvcs, func(i, j int) bool { return f64(pvcs[i].UsedPct) > f64(pvcs[j].UsedPct) })
	for _, p := range pvcs {
		days := unknown
		if p.DaysToFull != nil {
			days = strconv.FormatFloat(*p.DaysToFull, 'f', 0, 64) + "d"
		}
		t.Rows = append(t.Rows, []string{
			p.Namespace, p.Name, render.Dash(p.Phase), render.Dash(p.StorageClass),
			Bytes(p.CapacityBytes), Bytes(p.UsedBytes), Pct(p.UsedPct), days, render.Dash(p.MountedBy),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(p.Forecast), render.Dash(p.UsageReason)})
	}
	for _, p := range c.Storage.Pending {
		t.Rows = append(t.Rows, []string{
			p.Namespace, p.Name, "Pending", render.Dash(p.StorageClass), unknown, unknown, unknown, unknown, "<none>",
		})
		t.WideRows = append(t.WideRows, []string{"PENDING " + Seconds(p.PendingSeconds), render.Dash(p.Reason)})
	}
	return t
}

// NetworkTable is pod-IP headroom per pool.
func NetworkTable(c *api.Capacity) *render.Table {
	t := &render.Table{
		Headers:      []string{"POOL", "NODES", "IP HEADROOM", "SLOT HEADROOM", "IPS RUN OUT FIRST", "LAUNCHABLE", "SUBNET FULL"},
		WideHeaders:  []string{"SUBNETS", "LAUNCHABLE REASON"},
		EmptyMessage: "No pool network data.",
	}
	if c.Network == nil {
		return t
	}
	for _, p := range c.Network.Pools {
		full := "no"
		if p.SubnetExhausted {
			full = "YES"
		}
		t.Rows = append(t.Rows, []string{
			p.Name, strconv.Itoa(p.Nodes), Int64(p.IPHeadroom), Int64(p.SlotHeadroom),
			strconv.Itoa(p.NodesIPsFirst) + " nodes", Int(p.LaunchableNodes), full,
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(strings.Join(p.LaunchSubnets, ",")), render.Dash(p.LaunchableReason)})
	}
	return t
}

// BatchTable lists CronJobs with their overlap verdict, then Jobs with pending pods.
func BatchTable(c *api.Capacity) *render.Table {
	t := &render.Table{
		Headers:      []string{"KIND", "NAMESPACE", "NAME", "VERDICT", "RUNNING", "PENDING", "DETAIL"},
		EmptyMessage: "No Jobs or CronJobs.",
	}
	if c.Batch == nil {
		return t
	}
	for _, cj := range c.Batch.CronJobs {
		detail := cj.Schedule
		if cj.Suspended {
			detail += " (suspended)"
		}
		if cj.VerdictReason != "" {
			detail += " — " + cj.VerdictReason
		}
		t.Rows = append(t.Rows, []string{"CronJob", cj.Namespace, cj.Name, render.Dash(cj.Verdict),
			strconv.Itoa(cj.ActiveRuns), "-", render.Dash(truncate(detail, 80))})
	}
	for _, j := range c.Batch.Jobs {
		var parts []string
		if j.CronJob != "" {
			parts = append(parts, "from CronJob "+j.CronJob)
		}
		if j.OldestPendingSeconds != nil {
			parts = append(parts, "oldest pending "+Seconds(j.OldestPendingSeconds))
		}
		t.Rows = append(t.Rows, []string{"Job", j.Namespace, j.Name, "-",
			strconv.Itoa(j.Running), strconv.Itoa(j.Pending), render.Dash(strings.Join(parts, ", "))})
	}
	return t
}

// SpecialTable lists GPUs and other extended resources.
func SpecialTable(c *api.Capacity) *render.Table {
	t := &render.Table{
		Headers:      []string{"RESOURCE", "KIND", "ALLOCATABLE", "REQUESTED", "REQ %", "FREE", "NODES", "BLOCKED PODS"},
		EmptyMessage: "No GPUs or other special resources.",
	}
	if c.Special == nil {
		return t
	}
	for _, r := range c.Special.Resources {
		t.Rows = append(t.Rows, []string{
			r.Name, render.Dash(r.Kind), Int64(r.Allocatable), strconv.FormatInt(r.Requested, 10),
			Pct(r.RequestedPct), Int64(r.Free), strconv.Itoa(r.NodesOffered), strconv.Itoa(r.BlockedPods),
		})
	}
	return t
}

// CapacityTrendTable is one row per pool: grade and days to full per axis.
func CapacityTrendTable(tr *api.CapacityTrend) *render.Table {
	t := &render.Table{
		Headers:      []string{"POOL", "GRADE", "CPU NOW", "CPU FULL IN", "MEM NOW", "MEM FULL IN"},
		WideHeaders:  []string{"CPU FULL ON", "MEM FULL ON", "GRADE REASON"},
		EmptyMessage: "No pool trends yet.",
	}
	for _, p := range tr.Pools {
		grade, gradeWhy := unknown, ""
		if p.Grade != nil {
			grade, gradeWhy = render.Dash(p.Grade.Status), p.Grade.Reason
		}
		cNow, cIn, cOn := forecastCells(p.CPU)
		mNow, mIn, mOn := forecastCells(p.Memory)
		t.Rows = append(t.Rows, []string{p.Name, grade, cNow, cIn, mNow, mIn})
		t.WideRows = append(t.WideRows, []string{cOn, mOn, render.Dash(gradeWhy)})
	}
	return t
}

// forecastCells renders a forecast; a non-OK status is printed in place of a number so a
// pool still collecting data never reads as "never fills".
func forecastCells(f *api.CapacityForecast) (now, in, on string) {
	if f == nil {
		return unknown, unknown, unknown
	}
	now = Pct(f.CurrentPct)
	switch {
	case f.Days != nil:
		in = Days(f.Days)
	case f.Status != "":
		in = f.Status
	default:
		in = unknown
	}
	return now, in, render.Dash(f.FullOn)
}

// CapacityCostTable is one row per pool, then the levers.
func CapacityCostTable(c *api.CapacityCost) *render.Table {
	t := &render.Table{
		Headers:      []string{"POOL", "SOURCE", "NODES", "PRICED", "HOURLY", "MONTHLY", "CPU REQ %", "MEM REQ %"},
		EmptyMessage: "No priced pools.",
	}
	for _, p := range c.Pools {
		t.Rows = append(t.Rows, []string{
			p.Pool, render.Dash(p.Source), strconv.Itoa(p.Nodes), strconv.Itoa(p.PricedNodes),
			Money(p.Hourly), Money(p.Monthly), Pct(p.CPURequestsPct), Pct(p.MemoryRequestsPct),
		})
	}
	return t
}

// CostLeversTable lists the cost levers, savings first.
func CostLeversTable(c *api.CapacityCost) *render.Table {
	t := &render.Table{
		Headers:      []string{"LEVER", "SAVING", "MONTHLY", "ACTS IN", "WHY"},
		WideHeaders:  []string{"BASIS"},
		EmptyMessage: "No cost levers.",
	}
	for _, l := range c.Levers {
		s := "no"
		if l.Saving {
			s = "yes"
		}
		t.Rows = append(t.Rows, []string{render.Dash(l.Label), s, Money(l.Monthly), render.Dash(l.ActsIn),
			render.Dash(truncate(l.Reason, 80))})
		t.WideRows = append(t.WideRows, []string{render.Dash(l.Basis)})
	}
	return t
}

// CapacityNamespacesTable is one row per namespace, biggest CPU share first.
func CapacityNamespacesTable(n *api.CapacityNamespaces) *render.Table {
	rows := append(n.Namespaces[:0:0], n.Namespaces...)
	sort.SliceStable(rows, func(i, j int) bool { return f64(rows[i].CPUShare) > f64(rows[j].CPUShare) })
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "PODS", "PENDING", "CPU REQ", "CPU SHARE", "MEM REQ", "MEM SHARE", "QUOTA", "FULLEST QUOTA"},
		WideHeaders:  []string{"CPU USED", "MEM USED"},
		EmptyMessage: "No namespaces.",
	}
	for _, ns := range rows {
		fullest := "<none>"
		if f := ns.Fullest; f != nil {
			fullest = f.Resource + " " + f.Used + "/" + f.Hard + " (" + Pct(f.Pct) + ")"
		}
		t.Rows = append(t.Rows, []string{
			ns.Namespace, strconv.Itoa(ns.Pods), strconv.Itoa(ns.PendingPods),
			CPU(resCPU(ns.Requested)), Pct(ns.CPUShare), Bytes(resMem(ns.Requested)), Pct(ns.MemoryShare),
			render.Dash(ns.QuotaStatus), fullest,
		})
		t.WideRows = append(t.WideRows, []string{CPU(resCPU(ns.Used)), Bytes(resMem(ns.Used))})
	}
	return t
}

// FleetCapacityTable is one row per cluster, soonest-full first.
func FleetCapacityTable(f *api.FleetCapacity) *render.Table {
	rows := append(f.Clusters[:0:0], f.Clusters...)
	sort.SliceStable(rows, func(i, j int) bool {
		return soonest(rows[i].SoonestFull) < soonest(rows[j].SoonestFull)
	})
	t := &render.Table{
		Headers:      []string{"CLUSTER", "STATUS", "GRADE", "CPU REQ %", "MEM REQ %", "PENDING", "SHORTFALL", "FULLS FIRST", "IN"},
		WideHeaders:  []string{"CLUSTER ID", "AUTOSCALER", "NOTE"},
		EmptyMessage: "No clusters.",
	}
	for _, c := range rows {
		first, in := "<none>", unknown
		if s := c.SoonestFull; s != nil {
			first = s.Pool + " " + s.Axis
			if s.Days != nil {
				in = Days(s.Days)
			} else {
				in = render.Dash(s.Status)
			}
		}
		note := c.Reason
		if c.FromEarlierSync {
			note = strings.TrimSpace("from an earlier sync " + note)
		}
		t.Rows = append(t.Rows, []string{
			render.Dash(c.Name), render.Dash(c.Status), render.Dash(c.Grade),
			axisPct(c.CPU), axisPct(c.Memory), Int(c.Pending), Int(c.CapacityShortfall), first, in,
		})
		t.WideRows = append(t.WideRows, []string{c.ClusterID, render.Dash(c.AutoscalerState), render.Dash(note)})
	}
	return t
}

func axisPct(a *api.FleetAxis) string {
	if a == nil {
		return unknown
	}
	return Pct(a.RequestsCommitmentPct)
}

func soonest(s *api.FleetSoonestFull) int {
	if s == nil || s.Days == nil {
		return 1 << 30
	}
	return *s.Days
}

func i64(v *int64) int64 {
	if v == nil {
		return -1
	}
	return *v
}

func f64(v *float64) float64 {
	if v == nil {
		return -1
	}
	return *v
}
