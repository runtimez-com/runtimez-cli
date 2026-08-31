// Package browser opens a URL in the user's browser, and knows when it cannot.
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// Available reports whether a browser can plausibly be opened here.
//
// On Linux a missing DISPLAY/WAYLAND_DISPLAY means an SSH session or a container, which is
// the CLI's normal habitat — guessing wrong there hangs the login on a browser that will
// never appear.
func Available() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return false
		}
		_, err := exec.LookPath("xdg-open")
		return err == nil
	}
}

// Open launches the URL. It returns an error the caller can fall back from rather than
// treating a missing browser as fatal.
func Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	// Deliberately not waited on: xdg-open and open both fork, and waiting would block for
	// the lifetime of the browser.
	go func() { _ = cmd.Wait() }()
	return nil
}
