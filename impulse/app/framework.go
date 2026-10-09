// framework.go lists the framework's settings structs: the structs a framework module
// declares with env tags, which an application embeds in a configuration struct instead
// of declaring the variables itself. The scan expands an embedded one into the variables
// its declaration lists, as if the application had declared them.

package app

import (
	"github.com/cccteam/ccc/cloud"
	"github.com/cccteam/ccc/cloud/gcp/declaration"
)

// frameworkSettings are the framework's settings structs, as the modules declaring them
// publish their declarations: the cloud driver's, from the package beside the driver
// that holds nothing of it.
//
// The scan reads the declaration of the cloud version impulse is built with, so an
// application on an older cloud version is checked against a newer declaration. The
// upgrade ledger moves cloud and impulse together, so the two agree for an application
// that is current.
var frameworkSettings = []cloud.Declaration{declaration.Settings()}
