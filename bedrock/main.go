// Command bedrock is the infrastructure companion of impulse: it derives what an Impulse
// application needs from the application's own code and renders the application stack
// that provisions it, checks the committed stack against the code, and puts domain
// registrations and secret version pins into the placement.
package main

import (
	"os"
	// The time zone database travels with the binary: a maintenance window is read on
	// the client's wall clock in the pipeline's images and on the check's runner alike,
	// whatever zone files they carry.
	_ "time/tzdata"

	"github.com/cccteam/ccc/bedrock/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
