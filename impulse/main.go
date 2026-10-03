// Command impulse is the command-line tool for Impulse applications: apps built on the
// cccteam libraries with ccc/resource at the center.
package main

import (
	"os"

	"github.com/cccteam/ccc/impulse/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
