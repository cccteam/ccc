package integration

// The bootstrap-parity helpers: they provision the SHIPPED role configuration
// (schema/roles/crew.json and members.json) and the SHIPPED personas
// (cmd/bootstrap/users.json) through the real permission engines over the SHIPPED demo
// world, so what a human sees logging into the running demo is exactly what the suites
// pin; the demo product and the regression suite cannot drift apart.
//
// Provisioning is expensive (MigrateRoles writes one mutation per grant row), so the
// deploy path runs ONCE, for the shared world the read-only suites use, and every
// mutating suite gets its own seeded database served over the shared engines.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

// demoRoles mirrors the bootstrap's per-persona role assignment shape.
type demoRoles struct {
	Global  []accesstypes.Role                        `json:"global"`
	Domains map[accesstypes.Domain][]accesstypes.Role `json:"domains"`
}

// demoUser is one persona as cmd/bootstrap/users.json declares it.
type demoUser struct {
	User     accesstypes.User `json:"user"`
	Password string           `json:"password"`
	Roles    demoRoles        `json:"roles"`
}

// demoIdentities mirrors the bootstrap's identities file.
type demoIdentities struct {
	Users           []demoUser `json:"users"`
	ServiceAccounts []struct {
		User  accesstypes.User `json:"user"`
		Roles demoRoles        `json:"roles"`
	} `json:"serviceAccounts"`
}

// clientRoles is what the directory assigns Client Cleo: the groups APP_ROLES names in
// development, swept across the global scope and every sector. The parity world assigns
// them directly, since no login runs here.
var clientRoles = demoRoles{
	Global:  []accesstypes.Role{"client-account"},
	Domains: map[accesstypes.Domain][]accesstypes.Role{anvil: {"client-portal"}, bastion: {"client-portal"}, cinder: {"client-portal"}},
}

func loadRoleConfig(path string) (*access.RoleConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var roles access.RoleConfig
	if err := json.Unmarshal(raw, &roles); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return &roles, nil
}

func loadIdentities() (*demoIdentities, error) {
	raw, err := os.ReadFile(usersPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", usersPath, err)
	}
	var identities demoIdentities
	if err := json.Unmarshal(raw, &identities); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", usersPath, err)
	}

	return &identities, nil
}

// demoUsers lists the personas the bootstrap creates logins for.
func demoUsers(t *testing.T) []demoUser {
	t.Helper()

	identities, err := loadIdentities()
	if err != nil {
		t.Fatal(err)
	}

	return identities.Users
}

// openEngine opens a real engine over one auth's store prefix.
func openEngine(db *initiator.SpannerDB, prefix string) (*access.Client, error) {
	store, err := spannerstore.New(db.Client, spannerstore.WithPrefix(prefix))
	if err != nil {
		return nil, fmt.Errorf("spannerstore.New(): %w", err)
	}
	client, err := access.New(store)
	if err != nil {
		return nil, fmt.Errorf("access.New(): %w", err)
	}

	return client, nil
}

// provisionDemoAccess migrates both shipped role files through the production deploy
// path, assigns the personas and the droid their roles in the crew store and the client
// its directory roles in the members store, and waits for both snapshots.
func provisionDemoAccess(ctx context.Context, crewEngine, membersEngine *access.Client) error {
	crewRoles, err := loadRoleConfig(crewRolesPath)
	if err != nil {
		return err
	}
	if err := access.MigrateRoles(ctx, crewEngine.UserManager(), router.Collection(), crewRoles, sectors...); err != nil {
		return fmt.Errorf("access.MigrateRoles(crew): %w", err)
	}
	identities, err := loadIdentities()
	if err != nil {
		return err
	}
	for _, user := range identities.Users {
		if err := assignDemoRoles(ctx, crewEngine, user.User, user.Roles); err != nil {
			return err
		}
	}
	for _, account := range identities.ServiceAccounts {
		if err := assignDemoRoles(ctx, crewEngine, account.User, account.Roles); err != nil {
			return err
		}
	}

	membersRoles, err := loadRoleConfig(membersRolesPath)
	if err != nil {
		return err
	}
	if err := access.MigrateRoles(ctx, membersEngine.UserManager(), router.Collection(), membersRoles, sectors...); err != nil {
		return fmt.Errorf("access.MigrateRoles(members): %w", err)
	}
	if err := assignDemoRoles(ctx, membersEngine, clientUser, clientRoles); err != nil {
		return err
	}

	if err := waitForDecision(ctx, crewEngine, droidUser, cinder, accesstypes.Execute, "IngestDroidReports"); err != nil {
		return err
	}

	return waitForDecision(ctx, membersEngine, clientUser, cinder, accesstypes.Execute, "StandDownMission")
}

func assignDemoRoles(ctx context.Context, client *access.Client, user accesstypes.User, roles demoRoles) error {
	if len(roles.Global) > 0 {
		if err := client.UserManager().AddUserRoles(ctx, accesstypes.GlobalScope(), user, roles.Global...); err != nil {
			return fmt.Errorf("AddUserRoles(%s, global): %w", user, err)
		}
	}
	for domain, domainRoles := range roles.Domains {
		if err := client.UserManager().AddUserRoles(ctx, accesstypes.DomainScope(domain), user, domainRoles...); err != nil {
			return fmt.Errorf("AddUserRoles(%s, %s): %w", user, domain, err)
		}
	}

	return nil
}

// waitForDecision blocks until the engine's snapshot reflects the migrated policy: the
// store writes signal a reload, but the swap is asynchronous, so it polls the
// last-provisioned identity's authority until it stops being Denied.
func waitForDecision(ctx context.Context, client *access.Client, user accesstypes.User, domain accesstypes.Domain, perm accesstypes.Permission, res accesstypes.Resource) error {
	checker := client.ForUser(user)
	deadline := time.Now().Add(30 * time.Second)
	for {
		decisions, err := checker.Check(ctx, accesstypes.NewEnvironment().WithNow(time.Now()), accesstypes.DomainScope(domain), perm, res)
		if err == nil && !decisions[res].IsDenied() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("policy snapshot never became visible; last decisions for %s: %v (err %w)", user, decisions, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// demoAccessClient returns the shared provisioned crew engine. The demo policy is the same
// for every suite and no suite changes it, so one engine serves them all while each
// mutating suite keeps its own database for the rows it changes.
func demoAccessClient(t *testing.T) *access.Client {
	t.Helper()

	_, _, client := sharedWorld(t)

	return client
}

// newAccessClient opens an unprovisioned crew-store engine over db, closed with the test.
func newAccessClient(t *testing.T, db *initiator.SpannerDB) *access.Client {
	t.Helper()

	client, err := openEngine(db, crew.TablePrefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("access.Client.Close() error = %v", err)
		}
	})

	return client
}

// demoWorld prepares a fresh seeded database and the application over it, with the shared
// provisioned engines, for suites that MUTATE the seeded rows. Read-only suites use
// sharedWorld instead.
func demoWorld(t *testing.T) (context.Context, *initiator.SpannerDB, http.Handler) {
	t.Helper()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	sharedWorld(t)

	return ctx, db, newTestAppWithEngines(db, sharedCrew, sharedMembers)
}

// The shared world: one seeded database and one pair of provisioned engines for every
// read-only bootstrap-parity suite in the binary, built on first use and torn down by
// TestMain after the run.
var (
	sharedOnce    sync.Once
	sharedDB      *initiator.SpannerDB
	sharedCrew    *access.Client
	sharedMembers *access.Client
	sharedApp     http.Handler
	sharedErr     error
)

// sharedWorld returns the shared seeded database, its application over the real engines,
// and the crew engine itself. Suites using it must not mutate seeded rows.
func sharedWorld(t *testing.T) (*initiator.SpannerDB, http.Handler, *access.Client) {
	t.Helper()

	sharedOnce.Do(func() {
		ctx := context.Background()
		db, err := container.CreateDatabase(ctx, "shared-demo-world")
		if err != nil {
			sharedErr = fmt.Errorf("initiator.SpannerContainer.CreateDatabase(): %w", err)

			return
		}
		sharedDB = db
		if err := db.MigrateUp(migrationsSource, demoSeedSource); err != nil {
			sharedErr = fmt.Errorf("initiator.SpannerDB.MigrateUp(): %w", err)

			return
		}
		if sharedCrew, err = openEngine(db, crew.TablePrefix); err != nil {
			sharedErr = err

			return
		}
		if sharedMembers, err = openEngine(db, members.TablePrefix); err != nil {
			sharedErr = err

			return
		}
		if err := provisionDemoAccess(ctx, sharedCrew, sharedMembers); err != nil {
			sharedErr = err

			return
		}
		sharedApp = newTestAppWithEngines(db, sharedCrew, sharedMembers)
	})
	if sharedErr != nil {
		t.Fatalf("shared demo world: %v", sharedErr)
	}

	return sharedDB, sharedApp, sharedCrew
}

// closeSharedWorld tears the shared world down; TestMain calls it after m.Run.
func closeSharedWorld() {
	if sharedCrew != nil {
		_ = sharedCrew.Close()
	}
	if sharedMembers != nil {
		_ = sharedMembers.Close()
	}
	if sharedDB != nil {
		_ = sharedDB.DropDatabase(context.Background())
		_ = sharedDB.Close()
	}
}
