// Package declaration publishes the declaration of the PostgreSQL database driver's
// settings struct for the tools that read an application embedding it without loading
// its packages (impulse's checks, bedrock's stack): what postgres.Settings declares, in
// the shape the cloud package defines. It imports nothing of the driver, so a tool
// reading the declaration links none of the clients the driver opens.
package declaration

import "github.com/cccteam/ccc/cloud"

// Settings is what postgres.Settings declares: the struct's import path and name, and
// its env-tagged fields in declaration order, each as the driver writes it. The driver's
// TestSettingsDeclaration holds it to the struct, so a field renamed, retagged or
// re-documented without the declaration following fails in the resource module and
// never only in a tool reading it.
func Settings() cloud.Declaration {
	return cloud.Declaration{
		Path: "github.com/cccteam/ccc/resource/database/postgres",
		Name: "Settings",
		Fields: []cloud.Field{
			{
				Name: "Host",
				Type: "string",
				Tag:  "APP_POSTGRES_HOST,required",
				Doc: "Host is the server's host name or address, or the directory holding its Unix\n" +
					"socket (/cloudsql/<connection name> on Cloud Run).",
			},
			{
				Name: "Port",
				Type: "string",
				Tag:  "APP_POSTGRES_PORT,default=5432",
				Doc:  "Port is the server's port.",
			},
			{
				Name: "Database",
				Type: "string",
				Tag:  "APP_POSTGRES_DATABASE,required",
				Doc:  "Database is the database on the server.",
			},
			{
				Name: "User",
				Type: "string",
				Tag:  "APP_POSTGRES_USER,required",
				Doc:  "User is the role the application connects as.",
			},
			{
				Name: "Password",
				Type: "string",
				Tag:  "APP_POSTGRES_PASSWORD",
				Doc: "Password is the role's password, a secret that belongs in a secret store and never\n" +
					"in a committed file; empty where the server authenticates the connection another\n" +
					"way (the peer of a Unix socket, IAM on Cloud SQL).",
			},
			{
				Name: "SSLMode",
				Type: "string",
				Tag:  "APP_POSTGRES_SSL_MODE,default=require",
				Doc: "SSLMode is how the connection is protected, as PostgreSQL spells it (disable,\n" +
					"allow, prefer, require, verify-ca, verify-full): require unless set, and disable\n" +
					"against a local container.",
			},
		},
	}
}
