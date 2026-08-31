package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseSince turns "15m", "2h", "7d" into a duration. Plain digits are minutes, matching
// what people type when they are in a hurry.
func parseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Minute, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, usageErrorf("cannot read --since %q — try 15m, 2h or 7d", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, usageErrorf("cannot read --since %q — try 15m, 2h or 7d", s)
	}
	return d, nil
}

// timeWindow resolves --since/--from/--to into an explicit range.
//
// Every window is resolved to concrete instants before the request goes out, so the output
// can state exactly what it covered. A relative window that is never printed leaves the
// reader guessing whether "p99 400ms" meant the last hour or the last week.
func timeWindow(since, from, to string) (time.Time, time.Time, error) {
	end := time.Now()
	if to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return time.Time{}, time.Time{}, usageErrorf("--to must be RFC3339 (got %q)", to)
		}
		end = t
	}
	if from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return time.Time{}, time.Time{}, usageErrorf("--from must be RFC3339 (got %q)", from)
		}
		if !t.Before(end) {
			return time.Time{}, time.Time{}, usageErrorf("--from is not before --to")
		}
		return t, end, nil
	}

	d, err := parseSince(since)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if d == 0 {
		d = time.Hour
	}
	return end.Add(-d), end, nil
}

func windowLine(from, to time.Time) string {
	return fmt.Sprintf("Window: %s → %s (%s)",
		from.Format("2006-01-02 15:04:05 MST"), to.Format("15:04:05 MST"),
		durationShort(to.Sub(from)))
}
