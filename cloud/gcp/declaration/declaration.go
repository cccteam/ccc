// Package declaration publishes the declaration of the Google Cloud driver's settings
// struct for the tools that read an application embedding it without loading its
// packages (impulse's checks, bedrock's stack): what gcp.Settings declares, in the shape
// the cloud package defines. It imports nothing of the driver, so a tool reading the
// declaration links none of the cloud clients the driver opens.
package declaration

import "github.com/cccteam/ccc/cloud"

// Settings is what gcp.Settings declares: the struct's import path and name, and its
// env-tagged fields in declaration order, each as the driver writes it. The driver's
// TestSettingsDeclaration holds it to the struct, so a field renamed, retagged or
// re-documented without the declaration following fails in the cloud module and never
// only in a tool reading it.
func Settings() cloud.Declaration {
	return cloud.Declaration{
		Path: "github.com/cccteam/ccc/cloud/gcp",
		Name: "Settings",
		Fields: []cloud.Field{
			{
				Name: "LoggingProject",
				Type: "string",
				Tag:  "GOOGLE_CLOUD_LOGGING_PROJECT",
				Doc: "LoggingProject is the Google Cloud project request logs ship to and spans are\n" +
					"recorded in. Empty logs to the console and exports no span: development.",
			},
			{
				Name: "TraceSampling",
				Type: "string",
				Tag:  "APP_TRACE_SAMPLING,default=edge",
				Doc: "TraceSampling says which spans are recorded: every one (all), or the ones a request\n" +
					"Google's edge sampled starts (edge).",
			},
		},
	}
}
