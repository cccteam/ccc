package resource

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/logger"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

// TenantSignals is the signal channel a TenantRoster follows: what the live service
// offers (resource.Signaler and resource.SignalSubscriber together). Signal tells every
// instance that a kind changed; Subscribe runs onSignal on every signal of the kind after
// the subscription began, on the channel's own goroutine, and stop ends the subscription.
// The roster never opens a listener of its own: it subscribes to the application's one
// watch, for KindTenants.
type TenantSignals interface {
	Signaler
	SignalSubscriber
}

// TenantBackstop is how often a TenantRoster rereads its table whether or not a signal
// arrived: the correctness guard behind the push, so an instance that missed a signal is
// at most this far behind.
const TenantBackstop = 5 * time.Minute

// tenantSet is the roster's copy of the tenant keys: the value behind the atomic
// pointer, replaced whole and never written in place.
type tenantSet map[accesstypes.Domain]struct{}

// TenantRoster is one instance's copy of the tenant record's keys: the set of tenants
// the application serves, which the generated DomainGuard and the consolidated
// dispatcher ask before a tenant-scoped request runs. Start reads the set once and then
// keeps it current until the context ends: on every tenants signal the channel delivers
// and at the backstop interval regardless. The generated write paths of the tenant
// record call Add and Remove after their commit, so the instance that wrote a tenant
// serves it at once, and signal the tenants kind so every other instance reloads.
//
// The set sits behind an atomic pointer, so Has never waits on a reload or a writer and
// performs no read: a domain the set does not hold is unknown, whatever the table says
// at that moment. A nil roster holds no tenant, so an application that wires none fails
// closed on every tenant-scoped route. It is safe for concurrent use.
type TenantRoster struct {
	client    Client
	table     string
	keyColumn string
	backstop  time.Duration
	signals   TenantSignals

	// writes serializes the writers (Add, Remove, reload), each of which replaces the
	// set whole; readers load the pointer and never take it.
	writes sync.Mutex
	set    atomic.Pointer[tenantSet]
}

// TenantRosterOption configures a TenantRoster at construction.
type TenantRosterOption func(*TenantRoster)

// WithTenantSignals wires the application's signal channel: Start subscribes the roster
// to the tenants kind on it, so a tenant another instance wrote reaches this one within
// a moment instead of at the backstop. Without it the backstop alone keeps the copy
// current.
func WithTenantSignals(s TenantSignals) TenantRosterOption {
	return func(r *TenantRoster) {
		r.signals = s
	}
}

// WithTenantBackstop sets how often Start rereads the table without a signal;
// TenantBackstop by default. Tests shorten it.
func WithTenantBackstop(d time.Duration) TenantRosterOption {
	return func(r *TenantRoster) {
		r.backstop = d
	}
}

// NewTenantRoster builds the roster over the tenant record's table, reading the key
// column into the set. The generated New<Record>Roster constructor supplies the table
// and the column from the @tenant declaration; an application calls that one. Nothing
// is read until Start.
func NewTenantRoster(client Client, table, keyColumn string, opts ...TenantRosterOption) *TenantRoster {
	r := &TenantRoster{client: client, table: table, keyColumn: keyColumn, backstop: TenantBackstop}
	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Start loads the set once, failing when the read fails, so an application never
// starts serving on an empty roster it could not fill, and then keeps it current until
// ctx ends: a goroutine rereads the table on every tenants signal (when a channel is
// wired) and at the backstop interval regardless. A failed reread is logged and the
// copy stays as it was until the next.
func (r *TenantRoster) Start(ctx context.Context) error {
	if r == nil {
		return errors.New("resource: no tenant roster is wired")
	}
	if err := r.reload(ctx); err != nil {
		return err
	}

	signals := make(chan struct{}, 1)
	stop := func() {}
	if r.signals != nil {
		var err error
		stop, err = r.signals.Subscribe(KindTenants, func() {
			select {
			case signals <- struct{}{}:
			default:
			}
		})
		if err != nil {
			return errors.Wrap(err, "resource.TenantSignals.Subscribe()")
		}
	}
	go r.refresh(ctx, signals, stop)

	return nil
}

// refresh is Start's loop: a reread per signal and per backstop tick, until ctx ends.
func (r *TenantRoster) refresh(ctx context.Context, signals <-chan struct{}, stop func()) {
	defer stop()
	ticker := time.NewTicker(r.backstop)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-signals:
		}
		if err := r.reload(ctx); err != nil && ctx.Err() == nil {
			logger.FromCtx(ctx).Errorf("tenants: rereading the %s table failed; the roster stays as it was until the next signal or the backstop: %v", r.table, err)
		}
	}
}

// reload rereads the table and replaces the set; a failed read leaves the set as it was.
func (r *TenantRoster) reload(ctx context.Context) error {
	set, err := r.read(ctx)
	if err != nil {
		return err
	}

	r.writes.Lock()
	defer r.writes.Unlock()
	r.set.Store(&set)

	return nil
}

// read runs the roster's one statement outside any request transaction and returns the
// keys as a set.
func (r *TenantRoster) read(ctx context.Context) (tenantSet, error) {
	dbType := r.client.DBType()
	sql, err := tenantRosterStatement(dbType, r.table, r.keyColumn)
	if err != nil {
		return nil, err
	}

	txn := r.client.ReadOnlyTransaction()
	defer txn.Close()

	// The statement above refused any other database type.
	var keys []string
	if dbType == PostgresDBType {
		if keys, err = selectPostgresStrings(ctx, txn.PostgresReadOnlyTransaction(), &Statement{SQL: sql}); err != nil {
			return nil, err
		}
	} else {
		var rows []struct {
			Domain string `spanner:"Domain"`
		}
		if err := spxscan.Select(ctx, txn.SpannerReadOnlyTransaction(), &rows, spanner.Statement{SQL: sql}); err != nil {
			return nil, errors.Wrap(err, "spxscan.Select()")
		}
		for _, row := range rows {
			keys = append(keys, row.Domain)
		}
	}
	set := make(tenantSet, len(keys))
	for _, key := range keys {
		set[accesstypes.Domain(key)] = struct{}{}
	}

	return set, nil
}

// tenantRosterStatement renders the roster's one statement for the database type: the
// key column of the table, as text, under the name the scan reads. The identifiers are
// quoted the way the package's other statements quote them: backticks on Spanner,
// double quotes on Postgres.
func tenantRosterStatement(dbType DBType, table, keyColumn string) (string, error) {
	switch dbType {
	case SpannerDBType:
		g := newSQLGenerator(Spanner)

		return fmt.Sprintf("SELECT CAST(%s AS STRING) AS Domain FROM %s", g.quoteIdentifier(keyColumn), g.quoteIdentifier(table)), nil
	case PostgresDBType:
		g := newSQLGenerator(PostgreSQL)

		return fmt.Sprintf(`SELECT CAST(%s AS TEXT) AS "Domain" FROM %s`, g.quoteIdentifier(keyColumn), g.quoteIdentifier(table)), nil
	default:
		return "", errors.Newf("resource: tenant roster: unsupported database type %q", dbType)
	}
}

// Domains lists the tenants, sorted. It never fails and reads nothing; the signature
// is DomainRoster's, so the roster's Domains is what an application hands
// SessionPermissions. A nil roster lists none.
func (r *TenantRoster) Domains(context.Context) ([]accesstypes.Domain, error) {
	domains := []accesstypes.Domain{}
	if r == nil {
		return domains, nil
	}
	set := r.set.Load()
	if set == nil {
		return domains, nil
	}
	for domain := range *set {
		domains = append(domains, domain)
	}
	slices.Sort(domains)

	return domains, nil
}

// Has reports whether the domain is a tenant the roster holds. It loads the set and
// looks the domain up: no read, no wait. A domain the set does not hold is unknown,
// and every domain is unknown to a nil roster.
func (r *TenantRoster) Has(domain accesstypes.Domain) bool {
	if r == nil {
		return false
	}
	set := r.set.Load()
	if set == nil {
		return false
	}
	_, ok := (*set)[domain]

	return ok
}

// Add puts the domain in the set at once: what a tenant record's generated create path
// calls after its commit, so this instance serves the new tenant before any reload. A
// nil roster takes nothing.
func (r *TenantRoster) Add(domain accesstypes.Domain) {
	if r == nil {
		return
	}
	r.replace(func(set tenantSet) {
		set[domain] = struct{}{}
	})
}

// Remove drops the domain from the set at once: what a tenant record's generated delete
// path calls after its commit. A nil roster drops nothing.
func (r *TenantRoster) Remove(domain accesstypes.Domain) {
	if r == nil {
		return
	}
	r.replace(func(set tenantSet) {
		delete(set, domain)
	})
}

// replace copies the set, applies the change to the copy and swaps it in, under the
// writers' lock, so a reader never sees a set being written.
func (r *TenantRoster) replace(change func(tenantSet)) {
	r.writes.Lock()
	defer r.writes.Unlock()

	var current tenantSet
	if set := r.set.Load(); set != nil {
		current = *set
	}
	next := make(tenantSet, len(current)+1)
	for domain := range current {
		next[domain] = struct{}{}
	}
	change(next)
	r.set.Store(&next)
}
