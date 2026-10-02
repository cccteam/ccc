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
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
	"github.com/impulseframework/harbor/pkg/config"
	"github.com/impulseframework/harbor/pkg/router"
)

// MigrationsSource is where the schema migrations live, relative to the module root.
const MigrationsSource = "file://schema/migrations"

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
	// has nothing to apply and returns nil.
	if err := migrator.MigrateUpSchema(ctx, MigrationsSource); err != nil {
		return errors.Wrap(err, "initiator.SpannerMigrator.MigrateUpSchema()")
	}

	return nil
}

// Table names one of the two migrations tables the migrate command reports and forces:
// the schema migrations' and the data migrations'.
type Table string

// The two tables, as the command's flags and output name them.
const (
	SchemaTable Table = "schema"
	DataTable   Table = "data"
)

// Versions reads what the schema and data migrations tables say about the database: no
// version, clean at a version, or dirty at one with the progress the runner recorded.
// Nothing is applied.
func Versions(ctx context.Context, settings config.SpannerSettings) (schema, data initiator.Version, err error) {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return initiator.Version{}, initiator.Version{}, errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	schema, err = migrator.SchemaVersion(ctx)
	if err != nil {
		return initiator.Version{}, initiator.Version{}, errors.Wrap(err, "initiator.SpannerMigrator.SchemaVersion()")
	}
	data, err = migrator.DataVersion(ctx)
	if err != nil {
		return initiator.Version{}, initiator.Version{}, errors.Wrap(err, "initiator.SpannerMigrator.DataVersion()")
	}

	return schema, data, nil
}

// Force sets one migrations table to a version, clean, with no progress recorded; -1
// leaves the table with no version. It is for the states the runner refuses to guess at
// (a database the old library left dirty with no progress recorded, an in-flight
// operation Spanner no longer has, a file changed in its applied part): a person reads
// the database's state, decides what it really holds, and forces that version, and the
// next run continues from it.
func Force(ctx context.Context, settings config.SpannerSettings, table Table, version int) error {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	switch table {
	case SchemaTable:
		if err := migrator.ForceSchema(ctx, version); err != nil {
			return errors.Wrap(err, "initiator.SpannerMigrator.ForceSchema()")
		}
	case DataTable:
		if err := migrator.ForceData(ctx, version); err != nil {
			return errors.Wrap(err, "initiator.SpannerMigrator.ForceData()")
		}
	default:
		return errors.Newf("table %q is neither %s nor %s", table, SchemaTable, DataTable)
	}

	return nil
}

// MigrateRoles reconciles one auth's committed role configuration (its roles file) into
// its policy store (its user manager), validated against the generated permission
// collection, across the given tenant domains (none for a global-only application).
func MigrateRoles(ctx context.Context, manager access.UserManager, rolesPath string, domains ...accesstypes.Domain) error {
	roles, err := loadRoles(rolesPath)
	if err != nil {
		return err
	}

	if err := access.MigrateRoles(ctx, manager, router.Collection(), roles, domains...); err != nil {
		return errors.Wrap(err, "access.MigrateRoles()")
	}

	return nil
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
