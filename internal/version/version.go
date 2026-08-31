// Package version carries build metadata stamped in by the linker at release time.
package version

import (
	"fmt"
	"runtime"
)

// Overridden via -ldflags at build time; the defaults are what a `go build` produces.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String is the one-line form printed by `rtz version`.
func String() string {
	return fmt.Sprintf("rtz %s (commit %s, built %s, %s/%s, %s)",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
