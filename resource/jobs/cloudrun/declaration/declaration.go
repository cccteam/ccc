// Package declaration publishes the declaration of the Cloud Run job driver's settings
// struct for the tools that read an application embedding it without loading its
// packages (impulse's checks, bedrock's stack): what cloudrun.Settings declares, in the
// shape the cloud package defines. It imports nothing of the driver, so a tool reading
// the declaration links none of the clients the driver opens.
package declaration

import "github.com/cccteam/ccc/cloud"

// Settings is what cloudrun.Settings declares: the struct's import path and name, and
// its env-tagged fields in declaration order, each as the driver writes it. The driver's
// TestSettingsDeclaration holds it to the struct, so a field renamed, retagged or
// re-documented without the declaration following fails in the resource module and
// never only in a tool reading it.
func Settings() cloud.Declaration {
	return cloud.Declaration{
		Path: "github.com/cccteam/ccc/resource/jobs/cloudrun",
		Name: "Settings",
		Fields: []cloud.Field{
			{
				Name: "Template",
				Type: "string",
				Tag:  "APP_JOBS_TEMPLATE",
				Doc: "Template is the application's template job as the Cloud Run API names it\n" +
					"(projects/<project>/locations/<location>/jobs/<job>): the job the stack owns and\n" +
					"never runs, which the pipeline copies per build into a job named after it with the\n" +
					"build's version. Empty, as in development and in a pull-request stack that sets\n" +
					"none, no job process is configured, and every start is refused saying so.",
			},
		},
	}
}
