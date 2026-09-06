package integration

// The bootstrap-parity helpers: they provision the SHIPPED demo role config
// (cmd/bootstrap/demo_access.json) through the real permission engine — spannerstore →
// access.Client → MigrateRoles → ForUser — over the SHIPPED demo data, so what a human
// sees logging into the running demo is exactly what the suites pin; the demo product
// and the regression suite cannot drift apart.
//
// Provisioning is expensive: MigrateRoles writes one Spanner mutation per grant row,
// and the shipped config expands to a few thousand rows across the Administrator and
// nineteen sector roles at four scopes — near a minute on the emulator, and far worse
// when many suites provision at once (recorded as a finding). So the deploy path runs
// ONCE, for the shared world the read-only suites use, and every mutating suite's own
// world is seeded by cloning the shared world's provisioned tables.

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
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

const demoAccessPath = "../../cmd/bootstrap/demo_access.json"

// demoRoles mirrors the bootstrap's per-persona role assignment shape.
type demoRoles struct {
	Global  []accesstypes.Role                        `json:"global"`
	Domains map[accesstypes.Domain][]accesstypes.Role `json:"domains"`
}

// demoAccessConfig mirrors the bootstrap's committed shape: the RoleConfig
// MigrateRoles provisions plus per-persona and per-service-account role assignments.
type demoAccessConfig struct {
	access.RoleConfig
	Users []struct {
		User  accesstypes.User `json:"user"`
		Roles demoRoles        `json:"roles"`
	} `json:"users"`
	ServiceAccounts []struct {
		User  accesstypes.User `json:"user"`
		Roles demoRoles        `json:"roles"`
	} `json:"serviceAccounts"`
}

func loadDemoAccess() (*demoAccessConfig, error) {
	raw, err := os.ReadFile(demoAccessPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", demoAccessPath, err)
	}
	var conf demoAccessConfig
	if err := json.Unmarshal(raw, &conf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", demoAccessPath, err)
	}

	return &conf, nil
}

// openAccessClient opens the real engine over a database.
func openAccessClient(db *initiator.SpannerDB) (*access.Client, error) {
	store, err := spannerstore.New(db.Client)
	if err != nil {
		return nil, fmt.Errorf("spannerstore.New(): %w", err)
	}
	client, err := access.New(store)
	if err != nil {
		return nil, fmt.Errorf("access.New(): %w", err)
	}

	return client, nil
}

// provisionDemoAccess migrates the shipped role config, personas, and service accounts
// through the production deploy path into the engine and waits for the snapshot.
func provisionDemoAccess(ctx context.Context, client *access.Client) error {
	conf, err := loadDemoAccess()
	if err != nil {
		return err
	}

	if err := access.MigrateRoles(ctx, client.UserManager(), router.Collection(), &conf.RoleConfig, anvil, bastion, cinder); err != nil {
		return fmt.Errorf("access.MigrateRoles(): %w", err)
	}
	for _, user := range conf.Users {
		if err := assignDemoRoles(ctx, client, user.User, user.Roles); err != nil {
			return err
		}
	}
	for _, account := range conf.ServiceAccounts {
		if err := assignDemoRoles(ctx, client, account.User, account.Roles); err != nil {
			return err
		}
	}

	return waitForDemoPolicy(ctx, client)
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

// waitForDemoPolicy blocks until the engine's snapshot reflects the migrated policy:
// the store writes signal a reload, but the swap is asynchronous, so it polls the
// last-provisioned identity's authority (the droid's ingest grant at Cinder) until it
// stops being Denied.
func waitForDemoPolicy(ctx context.Context, client *access.Client) error {
	checker := client.ForUser("droid-r7")
	deadline := time.Now().Add(30 * time.Second)
	for {
		decisions, err := checker.Check(ctx, accesstypes.NewEnvironment().WithNow(time.Now()),
			accesstypes.DomainScope(cinder), accesstypes.Execute, "IngestDroidReports")
		if err == nil && !decisions["IngestDroidReports"].IsDenied() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("policy snapshot never became visible; last decisions: %v (err %w)", decisions, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// demoAccessClient returns the shared provisioned engine. The demo policy is the same for
// every suite and no suite changes it, so one engine serves them all while each mutating
// suite keeps its own database for the rows it changes: the engine answers from its policy
// snapshot, and the rows live wherever the application's database is. Provisioning once per
// binary is what keeps the suite inside its budget, since MigrateRoles is thousands of
// sequential store queries and the emulator answers them slowly on a shared runner. A
// suite that changes policy opens its own engine with newAccessClient.
func demoAccessClient(t *testing.T) *access.Client {
	t.Helper()

	_, _, client := sharedWorld(t)

	return client
}

// newAccessClient opens an unprovisioned engine over db, closed with the test.
func newAccessClient(t *testing.T, db *initiator.SpannerDB) *access.Client {
	t.Helper()

	client, err := openAccessClient(db)
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
// provisioned engine — for suites that MUTATE the seeded rows. Read-only suites use
// sharedWorld instead.
func demoWorld(t *testing.T) (context.Context, *initiator.SpannerDB, http.Handler) {
	t.Helper()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	return ctx, db, newTestAppWithAccess(db, demoAccessClient(t))
}

// The shared world: one seeded database and one provisioned engine for every
// read-only bootstrap-parity suite in the binary, built on first use and torn down by
// TestMain after the run.
var (
	sharedOnce sync.Once
	sharedDB   *initiator.SpannerDB
	sharedAcc  *access.Client
	sharedApp  http.Handler
	sharedErr  error
)

// sharedWorld returns the shared seeded database, its application over the real
// engine, and the engine itself. Suites using it must not mutate seeded rows.
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
		client, err := openAccessClient(db)
		if err != nil {
			sharedErr = err

			return
		}
		sharedAcc = client
		if err := provisionDemoAccess(ctx, client); err != nil {
			sharedErr = err

			return
		}
		sharedApp = newTestAppWithAccess(db, client)
	})
	if sharedErr != nil {
		t.Fatalf("shared demo world: %v", sharedErr)
	}

	return sharedDB, sharedApp, sharedAcc
}

// closeSharedWorld tears the shared world down; TestMain calls it after m.Run.
func closeSharedWorld() {
	if sharedAcc != nil {
		_ = sharedAcc.Close()
	}
	if sharedDB != nil {
		_ = sharedDB.DropDatabase(context.Background())
		_ = sharedDB.Close()
	}
}
