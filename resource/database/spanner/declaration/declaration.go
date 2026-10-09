// Package declaration publishes the declaration of the Spanner database driver's
// settings struct for the tools that read an application embedding it without loading
// its packages (impulse's checks, bedrock's stack): what spanner.Settings declares, in
// the shape the cloud package defines. It imports nothing of the driver, so a tool
// reading the declaration links none of the clients the driver opens.
package declaration

import "github.com/cccteam/ccc/cloud"

// Settings is what spanner.Settings declares: the struct's import path and name, and its
// env-tagged fields in declaration order, each as the driver writes it. The driver's
// TestSettingsDeclaration holds it to the struct, so a field renamed, retagged or
// re-documented without the declaration following fails in the resource module and
// never only in a tool reading it.
func Settings() cloud.Declaration {
	return cloud.Declaration{
		Path: "github.com/cccteam/ccc/resource/database/spanner",
		Name: "Settings",
		Fields: []cloud.Field{
			{
				Name: "ProjectID",
				Type: "string",
				Tag:  "GOOGLE_CLOUD_SPANNER_PROJECT,required",
				Doc:  "ProjectID is the Google Cloud project the Spanner instance belongs to.",
			},
			{
				Name: "InstanceID",
				Type: "string",
				Tag:  "GOOGLE_CLOUD_SPANNER_INSTANCE_ID,required",
				Doc:  "InstanceID is the Spanner instance the database is on.",
			},
			{
				Name: "DatabaseName",
				Type: "string",
				Tag:  "GOOGLE_CLOUD_SPANNER_DATABASE_NAME,required",
				Doc:  "DatabaseName is the database on the instance.",
			},
		},
	}
}
