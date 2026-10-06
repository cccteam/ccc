// Package main bootstraps a development database in a Spanner emulator: it creates the
// instance and database, runs the deployment's steps (the schema migrations, then the
// role policy check), and creates the development logins. It refuses to run against
// anything but an emulator.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"slices"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/config"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/deploy"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session"
	"github.com/go-playground/errors/v5"
)

// usersPath is the committed development identities: session users with plaintext
// passwords and their role assignments. Development only — a deployed application's
// membership comes from its identity provider or its administration surface.
const usersPath = "cmd/bootstrap/users.json"

// devIdentities is the file's shape.
type devIdentities struct {
	Users []devUser `json:"users"`
}

// devRoles is an identity's role memberships by where each is held, mirroring
// accesstypes.PolicyScope: the global partition, every tenant domain (one membership
// that reaches every tenant, the seeded ones and those created later), and single
// tenant domains. Every identities file carries the three keys in this order.
type devRoles struct {
	Global      []accesstypes.Role                        `json:"global"`
	EveryDomain []accesstypes.Role                        `json:"everyDomain"`
	Domains     map[accesstypes.Domain][]accesstypes.Role `json:"domains"`
}

// devUser is one development login.
type devUser struct {
	User     accesstypes.User `json:"user"`
	Password string           `json:"password"`
	Roles    devRoles         `json:"roles"`
}

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

	settings, err := config.LoadSpannerSettings(ctx)
	if err != nil {
		return errors.Wrap(err, "config.LoadSpannerSettings()")
	}

	if err := initiator.NewSpannerInstance(ctx, settings.ProjectID, settings.InstanceID); err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerInstance(): %s", settings.InstanceID)
	}
	fmt.Printf("Created instance %s\n", settings.InstanceID)

	db, err := initiator.NewSpannerDatabase(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerDatabase(): %s", settings.DatabaseName)
	}
	if err := db.Close(); err != nil {
		return errors.Wrap(err, "initiator.SpannerDB.Close()")
	}
	fmt.Printf("Created database %s\n", settings.DatabaseName)

	// From here the bootstrap runs the deployment's own steps.
	if err := deploy.MigrateSchema(ctx, settings); err != nil {
		return errors.Wrap(err, "deploy.MigrateSchema()")
	}

	// The development seed: the same data migrations the migrate command applies with
	// -seed in test environments, so development and a pull-request environment start
	// from the same data.
	if err := deploy.SeedDevelopmentData(ctx, settings); err != nil {
		return errors.Wrap(err, "deploy.SeedDevelopmentData()")
	}
	fmt.Println("Applied the schema migrations")

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	// The default roles travel with the release: opening the data level validated the
	// staff role file against the collection, so nothing provisions them. The deploy's
	// check prints what the store holds that this release cannot use as written.
	if err := deploy.CheckRoles(ctx, data.Staff().Access(), staff.Name); err != nil {
		return errors.Wrap(err, "deploy.CheckRoles()")
	}
	fmt.Printf("Checked the %s auth's role policy\n", staff.Name)

	// The feature flags the release declares, written off where the table has no row for
	// them (the development seed's rows, applied above, keep their state), as the deploy
	// writes them.
	if err := deploy.MigrateFeatures(ctx, data.ResourceClient()); err != nil {
		return errors.Wrap(err, "deploy.MigrateFeatures()")
	}

	if err := seedIdentities(ctx, data); err != nil {
		return errors.Wrap(err, "seedIdentities()")
	}

	return nil
}

// seedIdentities creates the development logins as session users and assigns their
// roles.
func seedIdentities(ctx context.Context, data *config.DataConfiguration) error {
	raw, err := os.ReadFile(usersPath)
	if err != nil {
		return errors.Wrapf(err, "os.ReadFile(%q)", usersPath)
	}
	var identities devIdentities
	if err := json.Unmarshal(raw, &identities); err != nil {
		return errors.Wrapf(err, "json.Unmarshal(%q)", usersPath)
	}

	for _, user := range identities.Users {
		if _, err := data.Staff().Session().API().CreateSessionUser(ctx, &session.CreateUserRequest{
			Username: string(user.User),
			Password: &user.Password,
		}); err != nil {
			return errors.Wrapf(err, "session.PasswordAuthAPI.CreateSessionUser(): user %s", user.User)
		}
		fmt.Printf("Created login %s\n", user.User)

		if err := assignRoles(ctx, data.UserManager(), user.User, user.Roles); err != nil {
			return err
		}
	}

	return nil
}

// assignRoles writes the identity's memberships where each is held: the global partition
// first, then every tenant domain, then each single domain in sorted order.
func assignRoles(ctx context.Context, manager access.UserManager, user accesstypes.User, roles devRoles) error {
	if len(roles.Global) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.GlobalPolicyScope(), user, roles.Global...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in the global partition", user)
		}
		fmt.Printf("Assigned %v to %s in the global partition\n", roles.Global, user)
	}
	if len(roles.EveryDomain) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.EveryDomainPolicyScope(), user, roles.EveryDomain...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in every domain", user)
		}
		fmt.Printf("Assigned %v to %s in every domain\n", roles.EveryDomain, user)
	}
	for _, domain := range slices.Sorted(maps.Keys(roles.Domains)) {
		domainRoles := roles.Domains[domain]
		if err := manager.AddUserRoles(ctx, accesstypes.DomainPolicyScope(domain), user, domainRoles...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in domain %s", user, domain)
		}
		fmt.Printf("Assigned %v to %s in domain %s\n", domainRoles, user, domain)
	}

	return nil
}
