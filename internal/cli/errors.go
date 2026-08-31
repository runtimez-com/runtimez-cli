package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
)

// Exit codes are a contract that scripts and CI depend on (FR-23).
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
	ExitAuth    = 3
	ExitPolicy  = 4
)

// ExitError carries an explicit exit code out of a command.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func usageErrorf(format string, args ...any) error {
	return &ExitError{Code: ExitUsage, Err: fmt.Errorf(format, args...)}
}

func authErrorf(format string, args ...any) error {
	return &ExitError{Code: ExitAuth, Err: fmt.Errorf(format, args...)}
}

// codeFor maps an error to its exit code, translating the API's own failures into the
// contract rather than making every command remember to.
func codeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.Unauthorized() {
		return ExitAuth
	}
	return ExitFailure
}

// report prints one actionable line — never a stack trace — and returns the exit code.
func report(err error) int {
	if err == nil {
		return ExitOK
	}
	code := codeFor(err)
	fmt.Fprintf(os.Stderr, "error: %v\n", err)

	var apiErr *api.Error
	switch {
	// Only add the hint when the message did not already say it — a doubled instruction
	// reads like two different problems.
	case code == ExitAuth && !strings.Contains(err.Error(), "rtz login"):
		fmt.Fprintln(os.Stderr, "hint: run `rtz login` to sign in again")
	case errors.As(err, &apiErr) && apiErr.Forbidden():
		fmt.Fprintln(os.Stderr,
			"hint: API keys carry no role — org and user administration needs `rtz login`")
	}
	return code
}
