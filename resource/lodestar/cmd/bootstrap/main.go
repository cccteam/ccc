// Package main bootstraps a Lodestar database in a Spanner emulator for development
// and demo purposes: it creates the instance and database, applies the schema
// migrations, loads the demo data, and provisions roles and demo personas through the
// access engine.
//
// The order matters: demo data loads before role provisioning because the domain
// universe MigrateRoles reconciles across is read from the seeded Sectors tenant
// table — tenancy is data here, not a compiled-in list.
package main

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"slices"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
)

// Environment variables configuring the bootstrap. All are optional; unset variables
// fall back to the defaults below. SPANNER_EMULATOR_HOST must additionally point at a
// running emulator: the bootstrap refuses to run against real infrastructure.
const (
	envProjectID  = "LODESTAR_SPANNER_PROJECT_ID"
	envInstanceID = "LODESTAR_SPANNER_INSTANCE_ID"
	envDatabase   = "LODESTAR_SPANNER_DATABASE"
	envAccessPath = "LODESTAR_BOOTSTRAP_ACCESS_PATH"
	envDataPath   = "LODESTAR_BOOTSTRAP_DATA_PATH"

	defaultProjectID  = "lodestar-demo"
	defaultInstanceID = "lodestar"
	defaultDatabase   = "lodestar"
	defaultAccessPath = "cmd/bootstrap/demo_access.json"
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

	if err := bootstrapData(db); err != nil {
		return errors.Wrap(err, "bootstrapData()")
	}

	if err := provisionAccess(ctx); err != nil {
		return errors.Wrap(err, "provisionAccess()")
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
// MigrateRoles provisions — including the conditional grants that make the personas'
// views differ — plus the demo personas with their login credentials and per-domain
// role assignments, and the service accounts with role assignments only.
type accessConfig struct {
	access.RoleConfig
	Users []demoUser `json:"users"`
	// ServiceAccounts are machine identities: they hold roles like any user but get
	// no session login — the droid outlet's API-key middleware binds requests
	// to them.
	ServiceAccounts []serviceAccount `json:"serviceAccounts"`
}

// demoUser is a seeded login: a session user (created through the session library, so
// the password is stored as a real Argon2 hash) plus the user's role assignments. The
// plaintext password is committed deliberately — these are fictional demo credentials
// for an emulator-only application.
type demoUser struct {
	User     accesstypes.User `json:"user"`
	Password string           `json:"password"`
	Roles    demoUserRoles    `json:"roles"`
}

// demoUserRoles expresses the global partition structurally — its own JSON key,
// never a magic domain-map entry — mirroring accesstypes.Scope.
type demoUserRoles struct {
	Global  []accesstypes.Role                        `json:"global"`
	Domains map[accesstypes.Domain][]accesstypes.Role `json:"domains"`
}

// serviceAccount is a seeded machine identity: role assignments without a login.
type serviceAccount struct {
	User  accesstypes.User `json:"user"`
	Roles demoUserRoles    `json:"roles"`
}

// provisionAccess migrates the role configuration into the access engine's policy
// store, validated against the generated permission collection (unknown resources,
// unregistered permissions, Update grants on immutable fields, and malformed or
// ill-typed grant conditions all fail the bootstrap), then seeds the demo personas as
// session users and assigns them their roles per domain. The Sectors tenant table
// — seeded by the demo data — supplies the domain universe the roles are reconciled
// across.
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

	domains, err := tenantDomains(ctx, spannerClient)
	if err != nil {
		return errors.Wrap(err, "tenantDomains()")
	}

	store, err := spannerstore.New(spannerClient)
	if err != nil {
		return errors.Wrap(err, "spannerstore.New()")
	}

	client, err := access.New(store)
	if err != nil {
		return errors.Wrap(err, "access.New()")
	}
	defer client.Close()

	if err := access.MigrateRoles(ctx, client.UserManager(), router.Collection(), &conf.RoleConfig, domains...); err != nil {
		return errors.Wrap(err, "access.MigrateRoles()")
	}
	fmt.Printf("Provisioned roles from %s across %v\n", path, domains)

	if err := seedDemoUsers(ctx, spannerClient, client.UserManager(), conf.Users); err != nil {
		return errors.Wrap(err, "seedDemoUsers()")
	}

	if err := seedServiceAccounts(ctx, client.UserManager(), conf.ServiceAccounts); err != nil {
		return errors.Wrap(err, "seedServiceAccounts()")
	}

	return nil
}

// tenantDomains reads the domain universe from the seeded Sectors tenant table.
func tenantDomains(ctx context.Context, spannerClient *spanner.Client) ([]accesstypes.Domain, error) {
	iter := spannerClient.Single().Query(ctx, spanner.Statement{SQL: "SELECT Id FROM Sectors ORDER BY Id"})
	defer iter.Stop()

	var domains []accesstypes.Domain
	for {
		row, err := iter.Next()
		if err != nil {
			if stderrors.Is(err, iterator.Done) {
				break
			}

			return nil, errors.Wrap(err, "spanner.RowIterator.Next()")
		}
		var id string
		if err := row.Columns(&id); err != nil {
			return nil, errors.Wrap(err, "spanner.Row.Columns()")
		}
		domains = append(domains, accesstypes.Domain(id))
	}
	if len(domains) == 0 {
		return nil, errors.New("the Sectors table is empty; the demo data must seed the tenants before roles are provisioned")
	}

	return domains, nil
}

// seedDemoUsers creates the demo personas as session users (giving them real password
// logins) and assigns them their roles per domain.
func seedDemoUsers(ctx context.Context, spannerClient *spanner.Client, manager access.UserManager, users []demoUser) error {
	cookieKey, err := config.EphemeralCookieKey()
	if err != nil {
		return errors.Wrap(err, "config.EphemeralCookieKey()")
	}

	// The session manager is only used to create users; the cookie key never signs a
	// served session cookie.
	passwordAuth, err := config.NewPasswordAuth(spannerClient, cookieKey)
	if err != nil {
		return errors.Wrap(err, "config.NewPasswordAuth()")
	}

	for _, user := range users {
		if _, err := passwordAuth.API().CreateSessionUser(ctx, &session.CreateUserRequest{
			Username: string(user.User),
			Password: &user.Password,
		}); err != nil {
			return errors.Wrapf(err, "session.PasswordAuthAPI.CreateSessionUser(): user %s", user.User)
		}
		fmt.Printf("Created session user %s\n", user.User)

		if err := assignRoles(ctx, manager, user.User, user.Roles); err != nil {
			return err
		}
	}

	return nil
}

// seedServiceAccounts assigns the machine identities their roles. No session user is
// created: a service account has no password login, only the role assignments the
// droid outlet's API-key identity checks against.
func seedServiceAccounts(ctx context.Context, manager access.UserManager, accounts []serviceAccount) error {
	for _, account := range accounts {
		fmt.Printf("Seeding service account %s\n", account.User)
		if err := assignRoles(ctx, manager, account.User, account.Roles); err != nil {
			return err
		}
	}

	return nil
}

// assignRoles grants the user their roles per scope: the global partition first, then
// each domain in sorted order.
func assignRoles(ctx context.Context, manager access.UserManager, user accesstypes.User, roles demoUserRoles) error {
	if len(roles.Global) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.GlobalScope(), user, roles.Global...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in the global scope", user)
		}
		fmt.Printf("Assigned %v to user %s in the global scope\n", roles.Global, user)
	}
	for _, domain := range slices.Sorted(maps.Keys(roles.Domains)) {
		domainRoles := roles.Domains[domain]
		if err := manager.AddUserRoles(ctx, accesstypes.DomainScope(domain), user, domainRoles...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): user %s in domain %s", user, domain)
		}
		fmt.Printf("Assigned %v to user %s in domain %s\n", domainRoles, user, domain)
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
