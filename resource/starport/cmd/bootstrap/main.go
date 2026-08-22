// Package main bootstraps a starport database in a Spanner emulator for development
// and demo purposes: it creates the instance and database, applies the schema
// migrations, provisions roles and demo users through the access engine, and loads
// the demo data.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/starport/pkg/router"
	"github.com/cccteam/ccc/resource/starport/pkg/stations"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
)

// Environment variables configuring the bootstrap. All are optional; unset variables
// fall back to the defaults below. SPANNER_EMULATOR_HOST must additionally point at a
// running emulator: the bootstrap refuses to run against real infrastructure.
const (
	envProjectID  = "STARPORT_SPANNER_PROJECT_ID"
	envInstanceID = "STARPORT_SPANNER_INSTANCE_ID"
	envDatabase   = "STARPORT_SPANNER_DATABASE"
	envAccessPath = "STARPORT_BOOTSTRAP_ACCESS_PATH"
	envDataPath   = "STARPORT_BOOTSTRAP_DATA_PATH"

	defaultProjectID  = "starport-demo"
	defaultInstanceID = "starport"
	defaultDatabase   = "starport"
	defaultAccessPath = "config/demo_access.json"
	defaultDataPath   = "schema/demoseed"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	if os.Getenv("SPANNER_EMULATOR_HOST") == "" {
		return errors.New("SPANNER_EMULATOR_HOST must be set: the bootstrap only targets a Spanner emulator")
	}

	db, err := bootstrapInstanceWithSchema(ctx)
	if err != nil {
		return errors.Wrap(err, "bootstrapInstanceWithSchema()")
	}
	defer db.Close()

	if err := provisionAccess(ctx); err != nil {
		return errors.Wrap(err, "provisionAccess()")
	}

	if err := bootstrapData(db); err != nil {
		return errors.Wrap(err, "bootstrapData()")
	}

	return nil
}

func bootstrapInstanceWithSchema(ctx context.Context) (*initiator.SpannerDB, error) {
	projectID := envOr(envProjectID, defaultProjectID)
	instanceID := envOr(envInstanceID, defaultInstanceID)
	databaseName := envOr(envDatabase, defaultDatabase)

	if err := initiator.NewSpannerInstance(ctx, projectID, instanceID); err != nil {
		return nil, errors.Wrapf(err, "failed to create instance %s", instanceID)
	}
	fmt.Printf("Created instance %s\n", instanceID)

	db, err := initiator.NewSpannerDatabase(ctx, projectID, instanceID, databaseName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create database %s", databaseName)
	}
	fmt.Printf("Created database %s\n", databaseName)

	if err := db.MigrateUp("file://schema/migrations"); err != nil {
		return nil, errors.Wrap(err, "failed to apply schema migrations")
	}
	fmt.Println("Applied schema migrations")

	return db, nil
}

// accessConfig is the committed demo access configuration: the role definitions
// MigrateRoles provisions, plus the demo users' per-domain role assignments.
type accessConfig struct {
	access.RoleConfig
	Users []userAssignment `json:"users"`
}

type userAssignment struct {
	User   accesstypes.User   `json:"user"`
	Domain accesstypes.Domain `json:"domain"`
	Roles  []accesstypes.Role `json:"roles"`
}

// provisionAccess migrates the role configuration into the access engine's policy
// store (created by the schema migrations), validated against the generated permission
// collection (unknown resources, unregistered permissions, and Update grants on
// immutable fields fail the bootstrap), then assigns the demo users their roles per
// domain. The stations supply the domain universe the roles are reconciled across.
func provisionAccess(ctx context.Context) error {
	path := envOr(envAccessPath, defaultAccessPath)
	if path == "" {
		return nil
	}

	conf, err := parseAccessConfig(path)
	if err != nil {
		return errors.Wrap(err, "parseAccessConfig()")
	}

	spannerClient, err := spanner.NewClient(ctx, databasePath())
	if err != nil {
		return errors.Wrap(err, "spanner.NewClient()")
	}
	defer spannerClient.Close()

	store, err := spannerstore.New(spannerClient)
	if err != nil {
		return errors.Wrap(err, "spannerstore.New()")
	}

	client, err := access.New(store)
	if err != nil {
		return errors.Wrap(err, "access.New()")
	}
	defer client.Close()

	if err := access.MigrateRoles(ctx, client.UserManager(), router.Collection(), &conf.RoleConfig, stations.Domains()...); err != nil {
		return errors.Wrap(err, "access.MigrateRoles()")
	}
	fmt.Printf("Provisioned roles from %s\n", path)

	for _, assignment := range conf.Users {
		if err := client.UserManager().AddUserRoles(ctx, assignment.Domain, assignment.User, assignment.Roles...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in domain %s", assignment.User, assignment.Domain)
		}
		fmt.Printf("Assigned %v to user %s in domain %s\n", assignment.Roles, assignment.User, assignment.Domain)
	}

	return nil
}

func parseAccessConfig(path string) (*accessConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "os.Open(%q)", path)
	}
	defer file.Close()

	var conf accessConfig
	if err := json.NewDecoder(file).Decode(&conf); err != nil {
		return nil, errors.Wrapf(err, "json.Decoder.Decode(%q)", path)
	}

	return &conf, nil
}

func bootstrapData(db *initiator.SpannerDB) error {
	path := envOr(envDataPath, defaultDataPath)
	if path == "" {
		return nil
	}

	begin := time.Now()
	if err := db.MigrateUp("file://" + path); err != nil {
		return errors.Wrapf(err, "failed to load demo data from %q", path)
	}
	fmt.Printf("Loaded demo data in %s\n", time.Since(begin))

	return nil
}

// databasePath returns the fully qualified Spanner database path the access engine's
// policy store connects to.
func databasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s",
		envOr(envProjectID, defaultProjectID), envOr(envInstanceID, defaultInstanceID), envOr(envDatabase, defaultDatabase))
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
