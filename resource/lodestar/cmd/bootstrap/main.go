// Package main bootstraps the Lodestar demo database: it creates the database where it is
// missing, runs the deployment's migration steps (the schema, then the role check), seeds
// the demo world, and creates the demo personas. The target is the environment's, as it is
// for the Spanner client library: with SPANNER_EMULATOR_HOST set it is the emulator the
// Procfile starts, whose instance the bootstrap also creates; without it, the project the
// application credentials reach, whose instance must already exist. A database that
// already exists is refused unless -reset is given, which empties its data and seeds it
// again without touching the schema.
//
// The order matters: the demo world is seeded before the data configuration opens because
// the tenant roster is read from the Sectors table at startup. Tenancy is data, not a
// compiled-in list. The roles need no step of their own: each auth's default roles travel
// with the binary and are validated when the auth opens, and the role check only reports
// what the store holds that the release cannot use.
//
// Demonstrates: bootstrap.target, bootstrap.reset.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"slices"

	"cloud.google.com/go/spanner"
	databaseadmin "cloud.google.com/go/spanner/admin/database/apiv1"
	"cloud.google.com/go/spanner/admin/database/apiv1/databasepb"
	instanceadmin "cloud.google.com/go/spanner/admin/instance/apiv1"
	"cloud.google.com/go/spanner/admin/instance/apiv1/instancepb"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/cccteam/ccc/resource/lodestar/pkg/deploy"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session"
	"github.com/go-playground/errors/v5"
	"google.golang.org/grpc/codes"
)

// usersPath is the committed demo cast: the crew personas with plaintext passwords and
// their role assignments, and the service accounts the droids outlet's API key binds
// requests to. The plaintext passwords are deliberate: these are fictional demo
// credentials for an application that is never published, whether its database is the
// emulator or a private test instance, and the README says so.
const usersPath = "cmd/bootstrap/users.json"

// target is where the bootstrap's database lives.
type target string

const (
	emulatorTarget target = "the Spanner emulator"
	projectTarget  target = "the project the application credentials reach"
)

// devIdentities is the file's shape.
type devIdentities struct {
	Users []devUser `json:"users"`
	// ServiceAccounts are machine identities: they hold roles like any user but get no
	// login. The droids outlet's API-key middleware binds requests to them.
	ServiceAccounts []devServiceAccount `json:"serviceAccounts"`
}

// devRoles is an identity's role assignments by where each is held: the global
// partition, every sector, and one sector at a time, each its own key, mirroring
// accesstypes.PolicyScope. A role held in every sector reaches a sector created after
// the bootstrap too.
type devRoles struct {
	Global      []accesstypes.Role                        `json:"global"`
	EveryDomain []accesstypes.Role                        `json:"everyDomain"`
	Domains     map[accesstypes.Domain][]accesstypes.Role `json:"domains"`
}

// devUser is one persona: a crew login and its roles.
type devUser struct {
	User     accesstypes.User `json:"user"`
	Password string           `json:"password"`
	Roles    devRoles         `json:"roles"`
}

// devServiceAccount is one machine identity: role assignments without a login.
//
// Demonstrates: outlet.api-key.
type devServiceAccount struct {
	User  accesstypes.User `json:"user"`
	Roles devRoles         `json:"roles"`
}

func main() {
	reset := flag.Bool("reset", false, "empty the existing database's data and seed it again; the schema stays")
	flag.Parse()

	if err := run(context.Background(), *reset); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, reset bool) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	settings, err := config.LoadSpannerSettings(ctx)
	if err != nil {
		return errors.Wrap(err, "config.LoadSpannerSettings()")
	}

	where := targetOf(os.Getenv("SPANNER_EMULATOR_HOST"))
	fmt.Printf("Bootstrapping %s in %s\n", settings.DatabasePath(), where)

	if where == emulatorTarget {
		if err := ensureInstance(ctx, settings); err != nil {
			return err
		}
	}

	existed, err := ensureDatabase(ctx, settings)
	if err != nil {
		return err
	}
	if existed && !reset {
		return errors.Newf("database %s already exists: run with -reset to empty its data and seed it again, or drop it first", settings.DatabasePath())
	}

	// From here the bootstrap runs the deployment's own steps.
	if err := deploy.MigrateSchema(ctx, settings); err != nil {
		return errors.Wrap(err, "deploy.MigrateSchema()")
	}
	fmt.Println("Applied the schema migrations")

	if existed {
		if err := resetData(ctx, settings); err != nil {
			return err
		}
		fmt.Println("Emptied the database's data; the schema stays")
	}

	// The file stores are emptied before the seed (files.go): no row holds a file yet.
	if err := emptyFileStores(ctx); err != nil {
		return err
	}

	if err := deploy.SeedDevelopmentData(ctx, settings); err != nil {
		return errors.Wrap(err, "deploy.SeedDevelopmentData()")
	}
	fmt.Printf("Seeded the demo world from %s\n", deploy.DevSeedSource)

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	// Each auth's default roles are the binary's, validated when the auth opened; the
	// check reports the role file's warnings and anything the store holds that this
	// release cannot use. The members roles say what a client may do; who holds them is
	// the directory's business (RoleSync), so no member is seeded here, and in
	// development APP_ROLES names the groups every simulated login is in.
	if err := deploy.CheckRoles(ctx, data.Crew().Access(), crew.Name); err != nil {
		return errors.Wrap(err, "deploy.CheckRoles(crew)")
	}
	if err := deploy.CheckRoles(ctx, data.Members().Access(), members.Name); err != nil {
		return errors.Wrap(err, "deploy.CheckRoles(members)")
	}

	// The feature flags the binary declares, written off where the table has no row
	// for them, as the deploy writes them.
	if err := deploy.MigrateFeatures(ctx, data.ResourceClient()); err != nil {
		return errors.Wrap(err, "deploy.MigrateFeatures()")
	}

	if err := seedIdentities(ctx, data); err != nil {
		return errors.Wrap(err, "seedIdentities()")
	}

	return nil
}

// targetOf picks the target the way the Spanner client library does: an emulator host in
// the environment means the emulator, none means the real project.
func targetOf(emulatorHost string) target {
	if emulatorHost != "" {
		return emulatorTarget
	}

	return projectTarget
}

// ensureInstance creates the emulator's instance when it is missing. A real instance is
// never created here: db-initiator's instance call carries no configuration or size, and
// a real one is provisioned by whoever owns the project.
func ensureInstance(ctx context.Context, settings config.SpannerSettings) error {
	admin, err := instanceadmin.NewInstanceAdminClient(ctx)
	if err != nil {
		return errors.Wrap(err, "instance.NewInstanceAdminClient()")
	}
	defer admin.Close()

	name := fmt.Sprintf("projects/%s/instances/%s", settings.ProjectID, settings.InstanceID)
	_, err = admin.GetInstance(ctx, &instancepb.GetInstanceRequest{Name: name})
	switch {
	case err == nil:
		fmt.Printf("Instance %s exists\n", settings.InstanceID)

		return nil
	case spanner.ErrCode(err) != codes.NotFound:
		return errors.Wrapf(err, "instance.InstanceAdminClient.GetInstance(): %s", name)
	}

	if err := initiator.NewSpannerInstance(ctx, settings.ProjectID, settings.InstanceID); err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerInstance(): %s", settings.InstanceID)
	}
	fmt.Printf("Created instance %s\n", settings.InstanceID)

	return nil
}

// ensureDatabase creates the database when it is missing and reports whether it already
// existed.
func ensureDatabase(ctx context.Context, settings config.SpannerSettings) (bool, error) {
	admin, err := databaseadmin.NewDatabaseAdminClient(ctx)
	if err != nil {
		return false, errors.Wrap(err, "database.NewDatabaseAdminClient()")
	}
	defer admin.Close()

	_, err = admin.GetDatabase(ctx, &databasepb.GetDatabaseRequest{Name: settings.DatabasePath()})
	switch {
	case err == nil:
		fmt.Printf("Database %s exists\n", settings.DatabaseName)

		return true, nil
	case spanner.ErrCode(err) != codes.NotFound:
		return false, errors.Wrapf(err, "database.DatabaseAdminClient.GetDatabase(): %s", settings.DatabasePath())
	}

	db, err := initiator.NewSpannerDatabase(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return false, errors.Wrapf(err, "initiator.NewSpannerDatabase(): %s", settings.DatabaseName)
	}
	if err := db.Close(); err != nil {
		return false, errors.Wrap(err, "initiator.SpannerDB.Close()")
	}
	fmt.Printf("Created database %s\n", settings.DatabaseName)

	return false, nil
}

// resetData empties the existing database's data through the deploy package, over a
// client of its own: the data level's clients are not open yet.
func resetData(ctx context.Context, settings config.SpannerSettings) error {
	client, err := spanner.NewClient(ctx, settings.DatabasePath())
	if err != nil {
		return errors.Wrap(err, "spanner.NewClient()")
	}
	defer client.Close()

	if err := deploy.ResetDevelopmentData(ctx, client, deploy.MigrationsSource); err != nil {
		return errors.Wrap(err, "deploy.ResetDevelopmentData()")
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
		if _, err := data.Crew().Session().API().CreateSessionUser(ctx, &session.CreateUserRequest{
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

	for _, account := range identities.ServiceAccounts {
		fmt.Printf("Seeding service account %s\n", account.User)
		if err := assignRoles(ctx, data.UserManager(), account.User, account.Roles); err != nil {
			return err
		}
	}

	return nil
}

// assignRoles writes the identity's memberships where each is held: the global partition
// first, then every sector, then each sector in sorted order.
func assignRoles(ctx context.Context, manager access.UserManager, user accesstypes.User, roles devRoles) error {
	if len(roles.Global) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.GlobalPolicyScope(), user, roles.Global...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in the global scope", user)
		}
		fmt.Printf("Assigned %v to %s in the global scope\n", roles.Global, user)
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
