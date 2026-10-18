package tui

import (
	"strconv"
	"strings"
)

// tone is what an operator should feel about a cell. Colour is derived from it rather than
// chosen at each call site, so "red" means the same thing on every screen.
type tone int

const (
	toneNone tone = iota
	toneOK
	toneWarn
	toneBad
	toneMuted // finished, or otherwise no longer worth attention
)

// statusWords maps the vocabulary the API actually returns. Anything unrecognised stays
// uncoloured — inventing a colour for a word we have not seen would assert a health
// judgement we did not make.
var statusWords = map[string]tone{
	"RUNNING": toneOK, "READY": toneOK, "ACTIVE": toneOK, "HEALTHY": toneOK,
	"BOUND": toneOK, "OK": toneOK, "PASS": toneOK, "PASSED": toneOK, "TRUE": toneOK,
	"AVAILABLE": toneOK, "LOW": toneOK, "NONE": toneOK, "CLEAN": toneOK,

	"SUCCEEDED": toneMuted, "COMPLETED": toneMuted, "COMPLETE": toneMuted,

	"PENDING": toneWarn, "CONTAINERCREATING": toneWarn, "PODINITIALIZING": toneWarn,
	"INIT": toneWarn, "PROGRESSING": toneWarn, "WARN": toneWarn, "WARNING": toneWarn,
	"MEDIUM": toneWarn, "DEGRADED": toneWarn, "NOTREADY": toneWarn, "TERMINATING": toneWarn,
	"UNSCHEDULABLE": toneWarn, "PARTIAL": toneWarn, "STALE": toneWarn,

	"FAILED": toneBad, "ERROR": toneBad, "CRASHLOOPBACKOFF": toneBad,
	"IMAGEPULLBACKOFF": toneBad, "ERRIMAGEPULL": toneBad, "OOMKILLED": toneBad,
	"EVICTED": toneBad, "CRITICAL": toneBad, "HIGH": toneBad, "FAIL": toneBad,
	"FAILING": toneBad, "FALSE": toneBad, "BLOCKED": toneBad, "UNKNOWN": toneBad,
}

// statusColumns are the headers whose value is a verdict rather than a measurement.
var statusColumns = map[string]bool{
	"STATUS": true, "PHASE": true, "STATE": true, "HEALTH": true, "SEVERITY": true,
	"VERDICT": true, "RESULT": true, "RISK": true, "LEVEL": true, "TIER": true,
	"READINESS": true, "CONDITION": true,
}

// cellTone reads one cell in the light of its column header.
//
// Deliberately NOT coloured: any column named SCORE. A risk score is worse when it is
// higher and an upgrade-readiness score is better when it is higher, so one rule would be
// wrong on one of them — and a confidently wrong colour is read as a fact.
func cellTone(header, value string) tone {
	v := strings.TrimSpace(value)
	if v == "" || v == "-" || v == "<none>" || v == "<unknown>" {
		return toneNone
	}
	h := strings.ToUpper(strings.TrimSpace(header))

	switch {
	case statusColumns[h]:
		return statusWords[strings.ToUpper(v)]

	case h == "RESTARTS":
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			return toneNone
		case n == 0:
			return toneNone
		case n < 5:
			return toneWarn
		default:
			return toneBad
		}

	case h == "READY":
		return readyTone(v)

	case h == "ERRORS", h == "ERROR RATE", h == "FAILURES":
		if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64); err == nil && f > 0 {
			return toneWarn
		}
		return toneNone
	}
	return toneNone
}

// readyTone reads a "3/5" ratio. Zero of something wanted is an outage; short of the
// target is a wobble; matching is fine.
func readyTone(v string) tone {
	num, den, ok := strings.Cut(v, "/")
	if !ok {
		return toneNone
	}
	r, err1 := strconv.Atoi(strings.TrimSpace(num))
	d, err2 := strconv.Atoi(strings.TrimSpace(den))
	if err1 != nil || err2 != nil || d <= 0 {
		return toneNone
	}
	switch {
	case r >= d:
		return toneOK
	case r == 0:
		return toneBad
	default:
		return toneWarn
	}
}

// rowMuted reports whether a whole row has finished its life — a completed Job pod, say.
// Those stay listed (they are evidence) but must not compete for attention with what is
// still running.
func rowMuted(headers, row []string) bool {
	for i, h := range headers {
		if i >= len(row) || !statusColumns[strings.ToUpper(strings.TrimSpace(h))] {
			continue
		}
		if statusWords[strings.ToUpper(strings.TrimSpace(row[i]))] == toneMuted {
			return true
		}
	}
	return false
}
