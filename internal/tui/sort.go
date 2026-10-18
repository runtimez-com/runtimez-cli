package tui

import (
	"sort"
	"strconv"
	"strings"
)

// Sorting works on the rendered cells, because those are what the operator is looking at.
// The comparator recovers the type the text encodes — an age, a size, a count, a severity —
// so a column sorts the way it reads rather than the way ASCII orders it. "9m" before
// "10m" and CRITICAL before LOW are both wrong under a plain string sort.

// severityRank orders the words that have an inherent order but not an alphabetical one.
var severityRank = map[string]int{
	"NONE": 0, "CLEAN": 0, "OK": 0, "PASS": 0, "PASSED": 0,
	"INFO": 1, "LOW": 2, "MEDIUM": 3, "MODERATE": 3,
	"HIGH": 4, "CRITICAL": 5, "FAIL": 5, "FAILED": 5, "BLOCKED": 5,
}

// unitScale covers the suffixes k8s and telemetry values carry.
var unitScale = map[string]float64{
	"s": 1, "m": 60, "h": 3600, "d": 86400, // durations, as k8s.Duration writes them
	"ms": 0.001, "us": 0.000001, "ns": 0.000000001,
	"ki": 1 << 10, "mi": 1 << 20, "gi": 1 << 30, "ti": 1 << 40,
	"k": 1e3, "M": 1e6, "G": 1e9, "T": 1e12,
	"%": 1,
}

// sortValue turns a cell into something orderable: a number when one can be recovered,
// otherwise the lowercased text.
func sortValue(cell string) (float64, string, bool) {
	s := strings.TrimSpace(cell)
	if s == "" || s == "-" || s == "<none>" || s == "<unknown>" {
		// Absent values sort last in either direction rather than pretending to be zero.
		return 0, "￿", false
	}
	if r, ok := severityRank[strings.ToUpper(s)]; ok {
		return float64(r), "", true
	}
	if n, ok := parseQuantity(s); ok {
		return n, "", true
	}
	// "3/5" and "12/12" sort by how many are up, which is what the eye is scanning for.
	if num, _, cut := strings.Cut(s, "/"); cut {
		if n, ok := parseQuantity(num); ok {
			return n, "", true
		}
	}
	return 0, strings.ToLower(s), false
}

// parseQuantity reads a number with an optional unit suffix: 42, 1.5, 370Mi, 3h, 98%.
func parseQuantity(s string) (float64, bool) {
	i := 0
	for i < len(s) && (s[i] == '-' || s[i] == '+' || s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, false
	}
	suffix := strings.TrimSpace(s[i:])
	if suffix == "" {
		return n, true
	}
	// Byte units are case-insensitive in practice; SI ones are not (m is minutes, M is mega).
	if scale, ok := unitScale[strings.ToLower(suffix)]; ok && strings.ContainsAny(suffix, "iI") {
		return n * scale, true
	}
	if scale, ok := unitScale[suffix]; ok {
		return n * scale, true
	}
	if scale, ok := unitScale[strings.ToLower(suffix)]; ok {
		return n * scale, true
	}
	return 0, false
}

// sortRows orders the visible index list by one column. The sort is stable, so rows that
// tie stay in the order the server returned them instead of shuffling on every refresh.
func sortRows(rows [][]string, visible []int, col int, desc bool) {
	if col < 0 {
		return
	}
	cell := func(i int) string {
		r := rows[i]
		if col >= len(r) {
			return ""
		}
		return r[col]
	}
	sort.SliceStable(visible, func(a, b int) bool {
		na, sa, oka := sortValue(cell(visible[a]))
		nb, sb, okb := sortValue(cell(visible[b]))
		var less bool
		switch {
		case oka && okb:
			if na == nb {
				return false
			}
			less = na < nb
		case oka != okb:
			// Numbers before text, both directions, so a stray "<none>" cannot displace
			// the ordering of the real values.
			return oka
		default:
			if sa == sb {
				return false
			}
			less = sa < sb
		}
		if desc {
			return !less
		}
		return less
	})
}
