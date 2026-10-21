package resource

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/signin"
	"github.com/cccteam/spxscan/spxapi"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// The tenant roster tests' table: the fixture's Stations, keyed by Id.
const (
	stationsTable = "Stations"
	stationsKey   = "Id"
)

// tenantDatabase creates a database on the shared emulator migrated with the tenant
// record's table, seeded with the given keys, dropped when the test ends, and the
// resource client over it.
func tenantDatabase(t *testing.T, name string, seed ...accesstypes.Domain) (*initiator.SpannerDB, Client) {
	t.Helper()

	container := spannerEmulator(t)
	ctx := context.Background()
	db, err := container.CreateDatabase(ctx, name)
	if err != nil {
		t.Fatalf("initiator.SpannerContainer.CreateDatabase() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.DropDatabase(context.Background()); err != nil {
			t.Error(err)
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.MigrateUp("file://testdata/tenants/schema"); err != nil {
		t.Fatalf("initiator.SpannerDB.MigrateUp() error = %v", err)
	}
	insertStations(t, db, seed...)

	return db, NewSpannerClient(db.Client)
}

// insertStations writes the rows a tenant record's create path commits.
func insertStations(t *testing.T, db *initiator.SpannerDB, ids ...accesstypes.Domain) {
	t.Helper()

	mutations := make([]*spanner.Mutation, 0, len(ids))
	for _, id := range ids {
		mutations = append(mutations, spanner.Insert(stationsTable, []string{stationsKey, "Name"}, []any{string(id), "Station " + string(id)}))
	}
	if len(mutations) == 0 {
		return
	}
	if _, err := db.Apply(t.Context(), mutations); err != nil {
		t.Fatalf("spanner.Client.Apply() error = %v", err)
	}
}

// deleteStations removes the rows a tenant record's delete path commits.
func deleteStations(t *testing.T, db *initiator.SpannerDB, ids ...accesstypes.Domain) {
	t.Helper()

	mutations := make([]*spanner.Mutation, 0, len(ids))
	for _, id := range ids {
		mutations = append(mutations, spanner.Delete(stationsTable, spanner.Key{string(id)}))
	}
	if _, err := db.Apply(t.Context(), mutations); err != nil {
		t.Fatalf("spanner.Client.Apply() error = %v", err)
	}
}

// fakeTenantSignals is a TenantSignals for the tests: the application's one signal channel
// as the live service will offer it, fanning every Signal of a kind out to every
// Subscribe of it, on the signaler's goroutine, and remembering the stops.
type fakeTenantSignals struct {
	mu       sync.Mutex
	subs     map[SignalKind]map[int]func()
	next     int
	stops    int
	signaled []SignalKind
	err      error
}

func (f *fakeTenantSignals) Signal(_ context.Context, kind SignalKind) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	f.signaled = append(f.signaled, kind)
	onSignals := make([]func(), 0, len(f.subs[kind]))
	for _, onSignal := range f.subs[kind] {
		onSignals = append(onSignals, onSignal)
	}
	f.mu.Unlock()
	for _, onSignal := range onSignals {
		onSignal()
	}

	return nil
}

func (f *fakeTenantSignals) Subscribe(kind SignalKind, onSignal func()) (func(), error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subs == nil {
		f.subs = make(map[SignalKind]map[int]func())
	}
	if f.subs[kind] == nil {
		f.subs[kind] = make(map[int]func())
	}
	id := f.next
	f.next++
	f.subs[kind][id] = onSignal

	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.subs[kind], id)
		f.stops++
	}, nil
}

// Subscribed counts the live subscriptions to the kind.
func (f *fakeTenantSignals) Subscribed(kind SignalKind) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.subs[kind])
}

// Stops counts the subscriptions ended so far.
func (f *fakeTenantSignals) Stops() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.stops
}

// countingClient counts the read-only transactions the roster opens: every read of the
// roster goes through one, so the count is the number of reads.
type countingClient struct {
	Client
	reads atomic.Int64
}

func (c *countingClient) ReadOnlyTransaction() ReadOnlyTransactionCloser {
	c.reads.Add(1)

	return c.Client.ReadOnlyTransaction()
}

// failingClient is a client whose reads fail while fail is set: the statement runs
// against a table that does not exist, so the roster's reread fails the way a lost
// database does.
type failingClient struct {
	Client
	fail     atomic.Bool
	failures atomic.Int64
}

func (c *failingClient) ReadOnlyTransaction() ReadOnlyTransactionCloser {
	txn := c.Client.ReadOnlyTransaction()
	if !c.fail.Load() {
		return txn
	}
	c.failures.Add(1)

	return &failingTransaction{ReadOnlyTransactionCloser: txn}
}

type failingTransaction struct {
	ReadOnlyTransactionCloser
}

func (f *failingTransaction) SpannerReadOnlyTransaction() spxapi.Querier {
	return failingQuerier{real: f.ReadOnlyTransactionCloser.SpannerReadOnlyTransaction()}
}

// failingQuerier runs a statement against a table that does not exist, whatever it is
// handed, so the iterator fails on its first row.
type failingQuerier struct {
	real spxapi.Querier
}

func (q failingQuerier) Query(ctx context.Context, _ spanner.Statement) *spanner.RowIterator {
	return q.real.Query(ctx, spanner.Statement{SQL: "SELECT Id FROM NoSuchTable"})
}

// TestTenantRoster_Start pins the start over the emulator: the seeded rows are the set,
// a read that fails fails the start and leaves the set empty.
func TestTenantRoster_Start(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		seed  []accesstypes.Domain
		table string
		// wantHas and wantNot are checked after Start; wantErr says Start must fail.
		wantHas     []accesstypes.Domain
		wantNot     []accesstypes.Domain
		wantDomains []accesstypes.Domain
		wantErr     bool
	}{
		{
			name:        "the seeded rows are the set",
			seed:        []accesstypes.Domain{"beta", "alpha"},
			table:       stationsTable,
			wantHas:     []accesstypes.Domain{"alpha", "beta"},
			wantNot:     []accesstypes.Domain{"gamma", ""},
			wantDomains: []accesstypes.Domain{"alpha", "beta"},
		},
		{
			name:        "an empty table is an empty set, not a failure",
			table:       stationsTable,
			wantNot:     []accesstypes.Domain{"alpha"},
			wantDomains: []accesstypes.Domain{},
		},
		{
			name:        "a failed read fails the start",
			seed:        []accesstypes.Domain{"alpha"},
			table:       "NoSuchTable",
			wantNot:     []accesstypes.Domain{"alpha"},
			wantDomains: []accesstypes.Domain{},
			wantErr:     true,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, client := tenantDatabase(t, "roster-start-"+strconv.Itoa(i), tt.seed...)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			r := NewTenantRoster(client, tt.table, stationsKey)
			err := r.Start(ctx)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Start() error = %v, wantErr %v", err, tt.wantErr)
			}
			for _, domain := range tt.wantHas {
				if !r.Has(domain) {
					t.Errorf("Has(%q) = false, want true", domain)
				}
			}
			for _, domain := range tt.wantNot {
				if r.Has(domain) {
					t.Errorf("Has(%q) = true, want false", domain)
				}
			}
			domains, err := r.Domains(ctx)
			if err != nil {
				t.Fatalf("Domains() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDomains, domains); diff != "" {
				t.Errorf("Domains() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTenantRoster_follows pins how the roster keeps up with the table: a write this
// instance made is visible at once through Add and Remove; a second roster over the
// same database reloads on the tenants signal the first's write path sends, showing
// the row the first wrote and dropping the one it deleted; without a signal the
// backstop brings the row in; and a failed reread keeps the last set until a reread
// succeeds.
func TestTenantRoster_follows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// signals says the rosters share a signal channel; backstop is the reread
		// interval of the second roster.
		signals  bool
		backstop time.Duration
		// failReads says the second roster's reads fail during the first signal.
		failReads bool
		within    time.Duration
	}{
		{name: "the signal reloads the second roster at once", signals: true, backstop: time.Hour, within: 2 * time.Second},
		{name: "the backstop brings the row in without a signal", backstop: 50 * time.Millisecond, within: 2 * time.Second},
		{name: "a failed reread keeps the last set until the next succeeds", signals: true, backstop: time.Hour, failReads: true, within: 2 * time.Second},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, client := tenantDatabase(t, "roster-follows-"+strconv.Itoa(i), "alpha")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var (
				signals *fakeTenantSignals
				opts    []TenantRosterOption
			)
			if tt.signals {
				signals = &fakeTenantSignals{}
				opts = append(opts, WithTenantSignals(signals))
			}
			secondClient := &failingClient{Client: client}
			first := NewTenantRoster(client, stationsTable, stationsKey, append(opts, WithTenantBackstop(time.Hour))...)
			second := NewTenantRoster(secondClient, stationsTable, stationsKey, append(opts, WithTenantBackstop(tt.backstop))...)
			if err := first.Start(ctx); err != nil {
				t.Fatalf("first.Start() error = %v", err)
			}
			if err := second.Start(ctx); err != nil {
				t.Fatalf("second.Start() error = %v", err)
			}
			if tt.signals {
				if got := signals.Subscribed(KindTenants); got != 2 {
					t.Fatalf("subscriptions to %s = %d, want 2: every roster subscribes to the one watch", KindTenants, got)
				}
			}

			// The first instance's create path: the commit, then Add, then the signal.
			insertStations(t, db, "beta")
			first.Add("beta")
			if !first.Has("beta") {
				t.Fatal("first.Has(beta) = false right after Add; a write must be visible at once")
			}
			if second.Has("beta") {
				t.Fatal("second.Has(beta) = true before any reload")
			}
			secondClient.fail.Store(tt.failReads)
			if tt.signals {
				if err := signals.Signal(ctx, KindTenants); err != nil {
					t.Fatalf("Signal() error = %v", err)
				}
			}
			if tt.failReads {
				// The reread ran and failed; the set is the one Start loaded.
				if !eventually(t, tt.within, func() bool { return secondClient.failures.Load() > 0 }) {
					t.Fatal("the signal did not reach the second roster's reread")
				}
				if second.Has("beta") || !second.Has("alpha") {
					t.Fatalf("after a failed reread the set changed: Has(alpha) = %v, Has(beta) = %v", second.Has("alpha"), second.Has("beta"))
				}
				secondClient.fail.Store(false)
				if err := signals.Signal(ctx, KindTenants); err != nil {
					t.Fatalf("Signal() error = %v", err)
				}
			}
			if !eventually(t, tt.within, func() bool { return second.Has("beta") }) {
				t.Fatalf("the second roster did not show beta within %s", tt.within)
			}

			// The first instance's delete path: the commit, then Remove, then the signal.
			deleteStations(t, db, "beta")
			first.Remove("beta")
			if first.Has("beta") {
				t.Fatal("first.Has(beta) = true right after Remove")
			}
			if tt.signals {
				if err := signals.Signal(ctx, KindTenants); err != nil {
					t.Fatalf("Signal() error = %v", err)
				}
			}
			if !eventually(t, tt.within, func() bool { return !second.Has("beta") }) {
				t.Fatalf("the second roster did not drop beta within %s", tt.within)
			}
			if !second.Has("alpha") {
				t.Error("second.Has(alpha) = false; the seeded row went missing")
			}
		})
	}
}

// TestTenantRoster_HasPerformsNoRead pins that Has answers from the set alone: after
// the start, any number of lookups, known and unknown, opens no transaction.
func TestTenantRoster_HasPerformsNoRead(t *testing.T) {
	t.Parallel()

	_, client := tenantDatabase(t, "roster-no-read", "alpha")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	counting := &countingClient{Client: client}
	r := NewTenantRoster(counting, stationsTable, stationsKey, WithTenantBackstop(time.Hour))
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got := counting.reads.Load(); got != 1 {
		t.Fatalf("reads after Start() = %d, want 1", got)
	}

	for range 1000 {
		if !r.Has("alpha") {
			t.Fatal("Has(alpha) = false")
		}
		if r.Has("unknown") {
			t.Fatal("Has(unknown) = true")
		}
	}
	if got := counting.reads.Load(); got != 1 {
		t.Errorf("reads after 2000 lookups = %d, want 1: Has performs no read", got)
	}
}

// TestTenantRoster_concurrentHas pins the set's safety under the race detector: lookups
// never wait on, and never tear under, the writers (Add, Remove and the rereads a tight
// backstop and a stream of signals drive).
func TestTenantRoster_concurrentHas(t *testing.T) {
	t.Parallel()

	_, client := tenantDatabase(t, "roster-concurrent", "alpha")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	signals := &fakeTenantSignals{}
	r := NewTenantRoster(client, stationsTable, stationsKey, WithTenantSignals(signals), WithTenantBackstop(time.Millisecond))
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for time.Now().Before(deadline) {
				if !r.Has("alpha") {
					t.Error("Has(alpha) = false during a reload; the seeded row is never dropped")

					return
				}
				r.Has("gamma")
			}
		})
	}
	wg.Go(func() {
		for time.Now().Before(deadline) {
			r.Add("gamma")
			r.Remove("gamma")
			if err := signals.Signal(ctx, KindTenants); err != nil {
				t.Errorf("Signal() error = %v", err)

				return
			}
		}
	})
	wg.Wait()
}

// TestTenantRoster_stopsWithContext pins that Start's refresh ends with the context:
// the subscription is stopped, and neither a signal nor the backstop reads again.
func TestTenantRoster_stopsWithContext(t *testing.T) {
	t.Parallel()

	_, client := tenantDatabase(t, "roster-stops", "alpha")
	ctx, cancel := context.WithCancel(t.Context())

	signals := &fakeTenantSignals{}
	counting := &countingClient{Client: client}
	r := NewTenantRoster(counting, stationsTable, stationsKey, WithTenantSignals(signals), WithTenantBackstop(20*time.Millisecond))
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !eventually(t, 2*time.Second, func() bool { return counting.reads.Load() > 1 }) {
		t.Fatal("the backstop never reread the table while the context was live")
	}

	cancel()
	if !eventually(t, 2*time.Second, func() bool { return signals.Stops() == 1 }) {
		t.Fatalf("subscription stops = %d, want 1 after the context ended", signals.Stops())
	}
	reads := counting.reads.Load()
	if err := signals.Signal(t.Context(), KindTenants); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := counting.reads.Load(); got != reads {
		t.Errorf("reads after the context ended = %d, want %d: neither the signal nor the backstop reads again", got, reads)
	}
	if !r.Has("alpha") {
		t.Error("Has(alpha) = false after the context ended; the last set stays")
	}
}

// stationColumns names the declaration the roster tests' fixture carries: the
// directory-only flag on Stations and the StationDirectories rows.
var stationColumns = &TenantDeclarationColumns{
	DirectoryOnly:    "DirectoryOnly",
	DirectoriesTable: "StationDirectories",
	TenantColumn:     "StationId",
	MethodColumn:     "Method",
	ConnectionColumn: "Connection",
}

// declareStation writes a station's declaration the way the records' write paths commit
// it: the flag on the row and one StationDirectories row per directory.
func declareStation(t *testing.T, db *initiator.SpannerDB, id accesstypes.Domain, directoryOnly bool, directories ...signin.Directory) {
	t.Helper()

	mutations := make([]*spanner.Mutation, 0, 1+len(directories))
	mutations = append(mutations, spanner.Update(stationsTable, []string{stationsKey, "DirectoryOnly"}, []any{string(id), directoryOnly}))
	for i, d := range directories {
		mutations = append(mutations, spanner.Insert("StationDirectories", []string{"Id", "StationId", "Method", "Connection"}, []any{string(id) + "-" + strconv.Itoa(i), string(id), string(d.Method), d.Connection}))
	}
	if _, err := db.Apply(t.Context(), mutations); err != nil {
		t.Fatalf("spanner.Client.Apply() error = %v", err)
	}
}

// TestTenantRoster_declarations pins the declaration half of the copy over the
// emulator: a roster built with WithTenantDeclarations reads each station's flag and
// directories in the snapshot of its keys, answers Declarations from the copy (a
// station declaring nothing answers an empty declaration, one the set does not hold
// nil), and a roster built without the option answers nil for every station; after a
// write, Reload brings the copy current at once, as the records' generated write paths
// call it.
func TestTenantRoster_declarations(t *testing.T) {
	t.Parallel()

	workspace := signin.Directory{Method: sessioninfo.MethodGoogle, Connection: "frontier.example"}
	entra := signin.Directory{Method: sessioninfo.MethodAzure, Connection: "6f2c1b7e-0000-4000-8000-000000000001"}

	tests := []struct {
		name     string
		declared bool
		// declare writes the declarations before Start; redeclare after it, before Reload.
		declare   func(t *testing.T, db *initiator.SpannerDB)
		redeclare func(t *testing.T, db *initiator.SpannerDB)
		want      map[accesstypes.Domain]*signin.TenantDeclaration
	}{
		{
			name:     "each station's flag and directories, ordered by method and connection",
			declared: true,
			declare: func(t *testing.T, db *initiator.SpannerDB) {
				declareStation(t, db, "alpha", true, workspace, entra)
				declareStation(t, db, "beta", false, workspace)
			},
			want: map[accesstypes.Domain]*signin.TenantDeclaration{
				"alpha": {DirectoryOnly: true, Directories: []signin.Directory{entra, workspace}},
				"beta":  {Directories: []signin.Directory{workspace}},
				"gamma": {},
				"delta": nil,
			},
		},
		{
			name:     "a roster built without the option declares nothing for every station",
			declared: false,
			declare: func(t *testing.T, db *initiator.SpannerDB) {
				declareStation(t, db, "alpha", true, workspace)
			},
			want: map[accesstypes.Domain]*signin.TenantDeclaration{"alpha": nil, "beta": nil, "gamma": nil, "delta": nil},
		},
		{
			name:     "Reload after a write brings the copy current",
			declared: true,
			declare: func(t *testing.T, db *initiator.SpannerDB) {
				declareStation(t, db, "alpha", false, workspace)
			},
			redeclare: func(t *testing.T, db *initiator.SpannerDB) {
				declareStation(t, db, "alpha", true)
				declareStation(t, db, "gamma", false, entra)
			},
			want: map[accesstypes.Domain]*signin.TenantDeclaration{
				"alpha": {DirectoryOnly: true, Directories: []signin.Directory{workspace}},
				"beta":  {},
				"gamma": {Directories: []signin.Directory{entra}},
				"delta": nil,
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, client := tenantDatabase(t, "roster-declarations-"+strconv.Itoa(i), "alpha", "beta", "gamma")
			tt.declare(t, db)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var opts []TenantRosterOption
			if tt.declared {
				opts = append(opts, WithTenantDeclarations(stationColumns))
			}
			r := NewTenantRoster(client, stationsTable, stationsKey, opts...)
			if err := r.Start(ctx); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if tt.redeclare != nil {
				tt.redeclare(t, db)
				if err := r.Reload(ctx); err != nil {
					t.Fatalf("Reload() error = %v", err)
				}
			}
			for domain, want := range tt.want {
				got, err := r.Declarations(ctx, domain)
				if err != nil {
					t.Fatalf("Declarations(%q) error = %v", domain, err)
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("Declarations(%q) mismatch (-want +got):\n%s", domain, diff)
				}
			}
		})
	}
}

// TestNewTenantDeclarationReader pins the sign-in policy's reader over the emulator: it
// reads one station's declaration through the read-write transaction it is handed, the
// sign-in's own, answering the flag and the directories as the roster's copy does, an
// empty declaration for a station declaring nothing, and nil for a station the table
// does not hold or an empty tenant.
func TestNewTenantDeclarationReader(t *testing.T) {
	t.Parallel()

	workspace := signin.Directory{Method: sessioninfo.MethodGoogle, Connection: "frontier.example"}
	entra := signin.Directory{Method: sessioninfo.MethodAzure, Connection: "6f2c1b7e-0000-4000-8000-000000000001"}
	db, _ := tenantDatabase(t, "declaration-reader", "alpha", "beta")
	declareStation(t, db, "alpha", true, workspace, entra)
	reader := NewTenantDeclarationReader(stationsTable, stationsKey, stationColumns)

	tests := []struct {
		name   string
		tenant accesstypes.Domain
		want   *signin.TenantDeclaration
	}{
		{name: "a declared station: its flag and its directories", tenant: "alpha", want: &signin.TenantDeclaration{DirectoryOnly: true, Directories: []signin.Directory{entra, workspace}}},
		{name: "a station declaring nothing: an empty declaration", tenant: "beta", want: &signin.TenantDeclaration{}},
		{name: "a station the table does not hold: nil", tenant: "nowhere", want: nil},
		{name: "an empty tenant: nil, with no read", tenant: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got *signin.TenantDeclaration
			if _, err := db.ReadWriteTransaction(t.Context(), func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
				declaration, err := reader(ctx, txn, tt.tenant)
				if err != nil {
					return err
				}
				got = declaration

				return nil
			}); err != nil {
				t.Fatalf("reader() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("reader(%q) mismatch (-want +got):\n%s", tt.tenant, diff)
			}
		})
	}
}

// spannerRoleManager is a signin.UserRoleManager whose read goes through the Spanner
// client's single-use read, as the access library's store does.
type spannerRoleManager struct {
	signin.UserRoleManager
	db *spanner.Client
}

func (m *spannerRoleManager) UserRoles(ctx context.Context, _ accesstypes.User, _ ...accesstypes.PolicyScope) (accesstypes.RoleCollection, error) {
	iter := m.db.Single().Query(ctx, spanner.Statement{SQL: "SELECT 1"})
	defer iter.Stop()
	if _, err := iter.Next(); err != nil {
		return nil, errors.Wrap(err, "spanner.RowIterator.Next()")
	}

	return accesstypes.RoleCollection{accesstypes.DomainPolicyScope("north"): {"Reader"}}, nil
}

// TestSignInRoles_spanner shows the condition SignInRoles exists for, on the emulator:
// the store's read with the sign-in transaction's context is refused as a nested
// transaction, and the adapter's read is not.
func TestSignInRoles_spanner(t *testing.T) {
	t.Parallel()

	db, _ := tenantDatabase(t, "signinroles")
	store := &spannerRoleManager{db: db.Client}

	tests := []struct {
		name    string
		manager signin.UserRoleManager
		wantErr bool
	}{
		{name: "the store read in the sign-in's transaction is a nested transaction", manager: store, wantErr: true},
		{name: "SignInRoles reads outside the transaction", manager: SignInRoles(store)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var held accesstypes.RoleCollection
			_, err := db.ReadWriteTransaction(t.Context(), func(ctx context.Context, _ *spanner.ReadWriteTransaction) error {
				var err error
				held, err = tt.manager.UserRoles(ctx, "someone")
				if err != nil {
					return errors.Wrap(err, "signin.UserRoleManager.UserRoles()")
				}

				return nil
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("UserRoles() in the transaction error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(held) != 1 {
				t.Errorf("UserRoles() = %v, want the one membership", held)
			}
		})
	}
}
