// Command ax is the Assess command-line interface.
package main

import (
	"os"

	"github.com/lisk0vian/assess/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
