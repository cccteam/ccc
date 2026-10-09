// Package declaration publishes the declaration of the Firestore live driver's settings
// struct for the tools that read an application embedding it without loading its
// packages (impulse's checks, bedrock's stack): what firestore.Settings declares, in the
// shape the cloud package defines. It imports nothing of the driver, so a tool reading
// the declaration links none of the clients the driver opens.
package declaration

import "github.com/cccteam/ccc/cloud"

// Settings is what firestore.Settings declares: the struct's import path and name, and
// its env-tagged fields in declaration order, each as the driver writes it. The driver's
// TestSettingsDeclaration holds it to the struct, so a field renamed, retagged or
// re-documented without the declaration following fails in the resource module and
// never only in a tool reading it.
func Settings() cloud.Declaration {
	return cloud.Declaration{
		Path: "github.com/cccteam/ccc/resource/live/firestore",
		Name: "Settings",
		Fields: []cloud.Field{
			{
				Name: "ProjectID",
				Type: "string",
				Tag:  "GOOGLE_CLOUD_FIRESTORE_PROJECT",
				Doc: "ProjectID is the Google Cloud project the database belongs to. A deployment that\n" +
					"names the database names its project too, or Open refuses: the database is not\n" +
					"assumed to be in the application's Spanner project, which, where environments\n" +
					"share a Spanner instance, is the shared instance's and not the environment's.\n" +
					"Against the emulator it may stay empty, and the project Open is handed stands in,\n" +
					"since the emulator takes any project id.",
			},
			{
				Name: "DatabaseID",
				Type: "string",
				Tag:  "APP_FIRESTORE_DATABASE",
				Doc: "DatabaseID is the Firestore database, by id: how a deployment hands the database\n" +
					"to the application.",
			},
			{
				Name: "APIKey",
				Type: "string",
				Tag:  "APP_FIREBASE_API_KEY",
				Doc: "APIKey is the Firebase web API key the browser initializes the SDK with; unused\n" +
					"against the emulator.",
			},
			{
				Name: "EmulatorHost",
				Type: "string",
				Tag:  "FIRESTORE_EMULATOR_HOST",
				Doc: "EmulatorHost is the Firestore emulator's host:port, the development stack's. Set,\n" +
					"the service talks to the emulator with its owner credential, answers the token\n" +
					"route with the host and no token, and revokes nothing.",
			},
		},
	}
}
