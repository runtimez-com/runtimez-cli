package view

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/k8s"
)

// unknown is what a cell says when the backend did not know the value. It is deliberately
// different from "0": an unread figure rendered as zero reads as "nothing there".
const unknown = "?"

// CPU renders millicores the way kubectl top does: whole cores above 10, else millicores.
func CPU(m *int64) string {
	if m == nil {
		return unknown
	}
	v := *m
	if v < 0 {
		return "-" + CPU(ptr(-v))
	}
	if v >= 10000 {
		return strconv.FormatFloat(float64(v)/1000, 'f', 1, 64)
	}
	return strconv.FormatInt(v, 10) + "m"
}

// Bytes renders a byte count in binary units.
func Bytes(b *int64) string {
	if b == nil {
		return unknown
	}
	v := *b
	if v < 0 {
		return "-" + Bytes(ptr(-v))
	}
	const unit = 1024
	if v < unit {
		return strconv.FormatInt(v, 10) + "B"
	}
	div, exp := int64(unit), 0
	for n := v / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(v)/float64(div), 'f', 1, 64) + []string{"Ki", "Mi", "Gi", "Ti", "Pi"}[exp]
}

// Pct renders a percentage the backend already computed (0-100).
func Pct(p *float64) string {
	if p == nil {
		return unknown
	}
	return strconv.FormatFloat(*p, 'f', 0, 64) + "%"
}

// Money renders a dollar figure; nil means unpriced, which is not $0.
func Money(v *float64) string {
	if v == nil {
		return "unpriced"
	}
	return "$" + strconv.FormatFloat(*v, 'f', 2, 64)
}

// Int renders an optional count.
func Int(v *int) string {
	if v == nil {
		return unknown
	}
	return strconv.Itoa(*v)
}

// Int64 renders an optional count.
func Int64(v *int64) string {
	if v == nil {
		return unknown
	}
	return strconv.FormatInt(*v, 10)
}

// Days renders a days-left figure; negative means already past.
func Days(v *int) string {
	if v == nil {
		return unknown
	}
	if *v < 0 {
		return fmt.Sprintf("%dd ago", -*v)
	}
	return strconv.Itoa(*v) + "d"
}

// Since renders how long ago a timestamp was.
func Since(t *time.Time) string {
	if t == nil || t.IsZero() {
		return unknown
	}
	return k8s.Duration(time.Since(*t))
}

// Seconds renders an elapsed second count.
func Seconds(s *int64) string {
	if s == nil {
		return unknown
	}
	return k8s.Duration(time.Duration(*s) * time.Second)
}

// Bool renders an optional flag.
func Bool(b *bool) string {
	if b == nil {
		return unknown
	}
	if *b {
		return "yes"
	}
	return "no"
}

// WorstReason is the most severe finding on a row, or nil when there is none.
func WorstReason(rs []api.AddonReason) *api.AddonReason {
	var worst *api.AddonReason
	worstRank := -1
	for i := range rs {
		r, _ := api.SeverityRank(rs[i].Severity)
		if r > worstRank {
			worst, worstRank = &rs[i], r
		}
	}
	return worst
}

// Counts renders a map of counts as "a=1 b=2", keys sorted, so output is stable.
func Counts(m map[string]int) string {
	if len(m) == 0 {
		return "<none>"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func ptr[T any](v T) *T { return &v }

func resCPU(r *api.Res) *int64 {
	if r == nil {
		return nil
	}
	return r.CPUMillicores
}

func resMem(r *api.Res) *int64 {
	if r == nil {
		return nil
	}
	return r.MemoryBytes
}
