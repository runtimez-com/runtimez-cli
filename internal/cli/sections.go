package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// printSection prints a titled table. The count is stated even when it is zero: "Pending
// pods (0)" is a finding, whereas an omitted section reads as "not checked".
func printSection(e *env, w io.Writer, title string, t *render.Table) error {
	fmt.Fprintf(w, "\n%s (%d)\n", title, len(t.Rows))
	return e.printer.Print(nil, t)
}

// warnStatus states a section the backend could not fully read. status "" or "OK" is silent;
// anything else is a "we could not see", never health.
func warnStatus(w io.Writer, what, status, reason string) {
	if s := strings.ToUpper(strings.TrimSpace(status)); s == "" || s == "OK" {
		return
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		fmt.Fprintf(w, "warning: %s: %s — %s\n", what, status, reason)
		return
	}
	fmt.Fprintf(w, "warning: %s: %s\n", what, status)
}

// printNotes prints the backend's caveats under a section.
func printNotes(w io.Writer, notes []string) {
	for _, n := range notes {
		if n = strings.TrimSpace(n); n != "" {
			fmt.Fprintf(w, "  note: %s\n", n)
		}
	}
}

// splitRef parses "<namespace>/<name>" or "<namespace>/<kind>/<name>".
func splitRef(ref string, parts int, shape string) ([]string, error) {
	p := strings.Split(ref, "/")
	if len(p) != parts {
		return nil, usageErrorf("expected %s, got %q", shape, ref)
	}
	for _, s := range p {
		if strings.TrimSpace(s) == "" {
			return nil, usageErrorf("expected %s, got %q", shape, ref)
		}
	}
	return p, nil
}
