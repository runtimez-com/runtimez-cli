package view

import (
	"fmt"
	"sort"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// The functions here render one row's detail as prose, for the TUI's row pane. They read
// only what the list response already carries, so opening a row costs no request.

// ReasonsText is a row's findings, worst first, each with the command that verifies it.
func ReasonsText(title, state, why string, rs []api.AddonReason) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n  State: %s", title, render.Dash(state))
	if why = strings.TrimSpace(why); why != "" {
		fmt.Fprintf(&b, " — %s", why)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "\nFindings (%d)\n", len(rs))
	if len(rs) == 0 {
		b.WriteString("  No findings.\n")
		return b.String()
	}
	for _, r := range sortReasons(rs) {
		fmt.Fprintf(&b, "\n  %s  %s\n", render.Dash(r.Severity), render.Dash(r.Title))
		if r.RuleID != "" {
			fmt.Fprintf(&b, "    rule:   %s\n", r.RuleID)
		}
		if obj := strings.TrimSpace(r.ObjectKind + " " + r.ObjectRef); obj != "" {
			fmt.Fprintf(&b, "    object: %s\n", obj)
		}
		if r.Message != "" {
			fmt.Fprintf(&b, "    %s\n", oneLine(r.Message))
		}
		if r.Recommendation != "" {
			fmt.Fprintf(&b, "    fix:    %s\n", oneLine(r.Recommendation))
		}
		if r.VerifyCommand != "" {
			fmt.Fprintf(&b, "    verify: %s\n", r.VerifyCommand)
		}
	}
	return b.String()
}

// sortReasons is worst first; ReasonsTable draws in the same order.
func sortReasons(rs []api.AddonReason) []api.AddonReason {
	rows := append([]api.AddonReason(nil), rs...)
	sort.SliceStable(rows, func(i, j int) bool {
		a, _ := api.SeverityRank(rows[i].Severity)
		b, _ := api.SeverityRank(rows[j].Severity)
		return a > b
	})
	return rows
}

// MaintainItemText is one Maintain item.
func MaintainItemText(it api.MaintainItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", render.Dash(it.DisplayName), it.Version)
	for _, kv := range [][2]string{
		{"cluster", it.ClusterName}, {"where", it.Location}, {"type", it.ItemType},
		{"deadline", strings.TrimSpace(it.Deadline + " " + it.DeadlineKind)}, {"days left", Days(it.DaysLeft)},
		{"support", it.SupportState}, {"confidence", it.Confidence}, {"owner", it.Owner},
		{"source", strings.TrimSpace(it.Source + " " + it.SourceURL)}, {"note", it.Note},
	} {
		if strings.TrimSpace(kv[1]) != "" {
			fmt.Fprintf(&b, "  %-11s %s\n", kv[0]+":", kv[1])
		}
	}
	return b.String()
}

// BlockedText is one pending pod and why the scheduler left it.
func BlockedText(p api.BlockedPod) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s\n", p.Namespace, p.Name)
	if p.OwnerKind != "" {
		fmt.Fprintf(&b, "  owner:     %s/%s\n", p.OwnerKind, p.OwnerName)
	}
	fmt.Fprintf(&b, "  pending:   %s\n", Seconds(p.PendingSeconds))
	fmt.Fprintf(&b, "  category:  %s", render.Dash(p.PrimaryCategory))
	if p.CapacityShortfall {
		b.WriteString(" (capacity shortfall)")
	}
	b.WriteString("\n")
	if p.NominatedNode != "" {
		fmt.Fprintf(&b, "  nominated: %s\n", p.NominatedNode)
	}
	if p.Message != "" {
		fmt.Fprintf(&b, "\n  %s\n", oneLine(p.Message))
	}
	if p.VerifyCommand != "" {
		fmt.Fprintf(&b, "\n  verify: %s\n", p.VerifyCommand)
	}
	return b.String()
}

// AppImpactTexts is one detail per AppImpactTable row, in the same order.
func AppImpactTexts(r *api.UpgradeReadiness) []string {
	if r.AppImpact == nil {
		return nil
	}
	var out []string
	add := func(ns, change string, it api.AppImpactItem) {
		var b strings.Builder
		fmt.Fprintf(&b, "%s — %s\n  namespace: %s\n  severity:  %s\n", render.Dash(it.Title), change, ns, render.Dash(it.Severity))
		for _, kv := range [][2]string{
			{"applies at", it.AppliesAtVersion}, {"add-on", it.Addon}, {"component", it.Component}, {"rule", it.RuleID},
		} {
			if kv[1] != "" {
				fmt.Fprintf(&b, "  %-10s %s\n", kv[0]+":", kv[1])
			}
		}
		if it.Impact != "" {
			fmt.Fprintf(&b, "\n  %s\n", oneLine(it.Impact))
		}
		if it.Recommendation != "" {
			fmt.Fprintf(&b, "\n  fix: %s\n", oneLine(it.Recommendation))
		}
		if it.VerifyCommand != "" {
			fmt.Fprintf(&b, "  verify: %s\n", it.VerifyCommand)
		}
		if len(it.Objects) > 0 {
			fmt.Fprintf(&b, "\n  objects (%d):\n", len(it.Objects))
			for _, o := range it.Objects {
				fmt.Fprintf(&b, "    %s\n", o)
			}
		}
		out = append(out, b.String())
	}
	for _, a := range r.AppImpact.Applications {
		for _, it := range a.MustChange {
			add(a.Namespace, "must change", it)
		}
		for _, it := range a.Affected {
			add(a.Namespace, "affected", it)
		}
	}
	return out
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
