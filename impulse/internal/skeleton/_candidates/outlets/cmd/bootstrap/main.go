// Package main bootstraps a development database in a Spanner emulator: it creates the
// instance and database, runs the deployment's migration steps (schema, roles), seeds
// the development tenants, and creates the development logins. It refuses to run
// against anything but an emulator.
//
// The order matters: the tenants are seeded before the roles because the domain
// universe MigrateRoles reconciles across is read from the Tenants table — tenancy is
// data, not a compiled-in list.
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
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/config"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/deploy"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session"
	"github.com/go-playground/errors/v5"
)

// usersPath is the committed development identities: the staff logins with plaintext
// passwords and their role assignments, the members (the portal's people, who sign in
// through the directory and so get no password here, only their roles in the members
// store), and the service accounts the machine outlet's API key binds requests to.
// Development only — a deployed application's membership comes from its identity
// provider or its administration surface.
const usersPath = "cmd/bootstrap/users.json"

// devIdentities is the file's shape.
type devIdentities struct {
	Users []devUser `json:"users"`
	// Members are the portal's people: the directory proves who they are (under the
	// skipAuth build tag, APP_USERNAME does), and the application assigns their roles,
	// so a member here is role assignments in the members store and nothing more.
	Members []devMember `json:"members"`
	// ServiceAccounts are machine identities: they hold roles like any user but get
	// no login — an outlet's API-key middleware binds requests to them.
	ServiceAccounts []devServiceAccount `json:"serviceAccounts"`
}

// devRoles is an identity's role assignments per scope; the global partition is its
// own key, mirroring accesstypes.Scope.
type devRoles struct {
	Global  []accesstypes.Role                        `json:"global"`
	Domains map[accesstypes.Domain][]accesstypes.Role `json:"domains"`
}

// devUser is one development login.
type devUser struct {
	User     accesstypes.User `json:"user"`
	Password string           `json:"password"`
	Roles    devRoles         `json:"roles"`
}

// devMember is one development member: role assignments in the members store, keyed by
// the username the directory will present.
type devMember struct {
	User  accesstypes.User `json:"user"`
	Roles devRoles         `json:"roles"`
}

// devServiceAccount is one machine identity: role assignments without a login.
type devServiceAccount struct {
	User  accesstypes.User `json:"user"`
	Roles devRoles         `json:"roles"`
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
	fmt.Println("Applied the schema migrations")

	if err := deploy.SeedDevelopmentData(ctx, settings); err != nil {
		return errors.Wrap(err, "deploy.SeedDevelopmentData()")
	}
	fmt.Printf("Seeded the development tenants from %s\n", deploy.DevSeedSource)

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	domains, err := data.Domains(ctx)
	if err != nil {
		return errors.Wrap(err, "config.DataConfiguration.Domains()")
	}
	if err := deploy.MigrateRoles(ctx, data.UserManager(), staff.RolesPath, domains...); err != nil {
		return errors.Wrap(err, "deploy.MigrateRoles()")
	}
	fmt.Printf("Provisioned roles from %s across %v\n", staff.RolesPath, domains)

	if err := deploy.MigrateRoles(ctx, data.Members().Access().UserManager(), members.RolesPath, domains...); err != nil {
		return errors.Wrap(err, "deploy.MigrateRoles()")
	}
	fmt.Printf("Provisioned roles from %s across %v\n", members.RolesPath, domains)

	if err := seedIdentities(ctx, data); err != nil {
		return errors.Wrap(err, "seedIdentities()")
	}

	return nil
}

// seedIdentities creates the development logins as staff session users and assigns
// every identity — logins, members, and service accounts — its roles in its auth's store.
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

	for _, member := range identities.Members {
		fmt.Printf("Seeding member %s\n", member.User)
		if err := assignRoles(ctx, data.Members().Access().UserManager(), member.User, member.Roles); err != nil {
			return err
		}
	}

	for _, account := range identities.ServiceAccounts {
		fmt.Printf("Seeding service account %s\n", account.User)
		if err := assignRoles(ctx, data.UserManager(), account.User, account.Roles); err != nil {
			return err
		}
	}

	return nil
}

// assignRoles grants the identity its roles per scope: the global partition first, then
// each domain in sorted order.
func assignRoles(ctx context.Context, manager access.UserManager, user accesstypes.User, roles devRoles) error {
	if len(roles.Global) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.GlobalScope(), user, roles.Global...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in the global scope", user)
		}
		fmt.Printf("Assigned %v to %s in the global scope\n", roles.Global, user)
	}
	for _, domain := range slices.Sorted(maps.Keys(roles.Domains)) {
		domainRoles := roles.Domains[domain]
		if err := manager.AddUserRoles(ctx, accesstypes.DomainScope(domain), user, domainRoles...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in domain %s", user, domain)
		}
		fmt.Printf("Assigned %v to %s in domain %s\n", domainRoles, user, domain)
	}

	return nil
}
