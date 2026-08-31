// Command rtz is the runtimez CLI.
package main

import (
	"os"

	"github.com/runtimez-com/runtimez-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
