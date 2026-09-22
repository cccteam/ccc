// Package deploy holds the database steps a deployment runs and the development
// bootstrap reuses: applying the schema migrations and reconciling the role
// configuration into the permission engine's policy store.
package deploy

import (
	"context"
	"encoding/json"
	"os"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	consolerouter "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/console/pkg/router"
	portalrouter "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/portal/pkg/router"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/config"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
	"github.com/golang-migrate/migrate/v4"
)

// MigrationsSource is where the schema migrations live, relative to the module root.
const MigrationsSource = "file://schema/migrations"

// DevSeedSource is the development data (the tenants a developer signs in to), applied
// by cmd/bootstrap only, relative to the module root.
const DevSeedSource = "file://schema/devseed"

// MigrateSchema connects to the existing database and applies every pending schema
// migration. Creating the database is not its business: a deployment's database exists
// before its first migration runs, and cmd/bootstrap creates the emulator's.
func MigrateSchema(ctx context.Context, settings config.SpannerSettings) error {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	// A database already at the latest migration is not a failure: the migrator
	// reports it as migrate.ErrNoChange.
	if err := migrator.MigrateUpSchema(ctx, MigrationsSource); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errors.Wrap(err, "initiator.SpannerMigrator.MigrateUpSchema()")
	}

	return nil
}

// SeedDevelopmentData applies the development seed to the database as a data
// migration.
func SeedDevelopmentData(ctx context.Context, settings config.SpannerSettings) error {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	if err := migrator.MigrateUpData(ctx, DevSeedSource); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errors.Wrap(err, "initiator.SpannerMigrator.MigrateUpData()")
	}

	return nil
}

// MigrateRoles reconciles the committed role configuration into the policy store,
// validated against the generated permission collection, across the given tenant
// domains — the roster read from the Tenants table.
func MigrateRoles(ctx context.Context, manager access.UserManager, rolesPath string, domains ...accesstypes.Domain) error {
	roles, err := loadRoles(rolesPath)
	if err != nil {
		return err
	}

	collection, err := Collection()
	if err != nil {
		return err
	}

	if err := access.MigrateRoles(ctx, manager, collection, roles, domains...); err != nil {
		return errors.Wrap(err, "access.MigrateRoles()")
	}

	return nil
}

// Collection is the application's whole permission registry: the union of every site's
// generated collection. The sites share one policy store — a login is one identity and a
// role is one set of powers across the application — so the roles are reconciled against
// everything any site registers. A resource both sites serve is declared identically in
// both and appears once; access.UnionCollection refuses sites that disagree on one.
func Collection() (access.PermissionCollection, error) {
	collection, err := access.UnionCollection(consolerouter.Collection(), portalrouter.Collection())
	if err != nil {
		return nil, errors.Wrap(err, "access.UnionCollection(console, portal)")
	}

	return collection, nil
}

func loadRoles(path string) (*access.RoleConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "os.ReadFile(%q)", path)
	}

	var roles access.RoleConfig
	if err := json.Unmarshal(raw, &roles); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(%q)", path)
	}

	return &roles, nil
}
