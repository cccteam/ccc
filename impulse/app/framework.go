// framework.go lists the framework's settings structs: the structs a framework module
// declares with env tags, which an application embeds in a configuration struct instead
// of declaring the variables itself. The scan expands an embedded one into the variables
// its declaration lists, as if the application had declared them.

package app

import (
	"github.com/cccteam/ccc/cloud"
	gcpdeclaration "github.com/cccteam/ccc/cloud/gcp/declaration"
	postgresdeclaration "github.com/cccteam/ccc/resource/database/postgres/declaration"
	spannerdeclaration "github.com/cccteam/ccc/resource/database/spanner/declaration"
	cloudrundeclaration "github.com/cccteam/ccc/resource/jobs/cloudrun/declaration"
	firestoredeclaration "github.com/cccteam/ccc/resource/live/firestore/declaration"
)

// frameworkSettings are the framework's settings structs, as the modules declaring them
// publish their declarations: the cloud driver's, the database drivers' (Spanner,
// PostgreSQL), the job driver's (Cloud Run) and the live driver's (Firestore), each from
// the package beside the driver that holds nothing of it.
//
// The scan reads the declarations of the cloud and resource versions impulse is built
// with, so an application on an older version is checked against a newer declaration.
// The upgrade ledger moves the framework and impulse together, so they agree for an
// application that is current.
var frameworkSettings = []cloud.Declaration{
	gcpdeclaration.Settings(),
	spannerdeclaration.Settings(),
	postgresdeclaration.Settings(),
	cloudrundeclaration.Settings(),
	firestoredeclaration.Settings(),
}
