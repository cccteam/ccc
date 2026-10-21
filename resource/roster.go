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
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/signin"
	"github.com/cccteam/spxscan"
	"github.com/cccteam/spxscan/spxapi"
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

// errTenantRosterUnsupportedDatabase is the answer on a database the roster's read is
// not implemented for: the statement renders for Postgres, the read does not yet.
var errTenantRosterUnsupportedDatabase = errors.New("resource: the tenant roster's read is implemented for Spanner only")

// tenantSet is the roster's copy of the tenants: each key with what the tenant declares
// about who may enter it, nil for a tenant that declares nothing (or when the roster
// reads no declaration). The value behind the atomic pointer, replaced whole and never
// written in place.
type tenantSet map[accesstypes.Domain]*signin.TenantDeclaration

// TenantDeclarationColumns names where the tenant record keeps its declaration of who
// may enter it (signin.TenantDeclaration), so the roster reads the declaration beside
// the keys and the sign-in policy reads it in its own transaction
// (NewTenantDeclarationReader): the record's directory-only column, and the directories
// record's table with its tenant, method and connection columns. The generated
// New<Record>Roster and New<Record>DeclarationReader constructors supply them from the
// @directoryOnly and @tenantDirectory declarations; an application names none of them.
type TenantDeclarationColumns struct {
	// DirectoryOnly is the tenant record's BOOL column saying the tenant's people enter
	// through its directories only, so a password does not reach it; empty when the
	// record declares none, and every tenant admits a password.
	DirectoryOnly string
	// DirectoriesTable is the directories record's table, one row per directory a tenant
	// admits its people through: TenantColumn holds the tenant's key, MethodColumn the
	// sign-in method as sessioninfo.AuthMethod spells it (google, azure, workos) and
	// ConnectionColumn the connection the method's identities carry (a Google Workspace
	// by its domain, a Microsoft Entra tenant by its tenant id).
	DirectoriesTable string
	TenantColumn     string
	MethodColumn     string
	ConnectionColumn string
}

// declared reports whether the roster reads a declaration: the columns are given and
// name the directories record.
func (c *TenantDeclarationColumns) declared() bool {
	return c != nil && c.DirectoriesTable != ""
}

// TenantRoster is one instance's copy of the tenant record's keys: the set of tenants
// the application serves, which the generated DomainGuard and the consolidated
// dispatcher ask before a tenant-scoped request runs. Start reads the set once and then
// keeps it current until the context ends: on every tenants signal the channel delivers
// and at the backstop interval regardless. The generated write paths of the tenant
// record call Add and Remove after their commit, so the instance that wrote a tenant
// serves it at once, and signal the tenants kind so every other instance reloads.
//
// With WithTenantDeclarations the copy carries what each tenant declares about who may
// enter it beside its key, read in the same snapshot, and Declarations answers the
// generated DomainGuard's and the session's tenant list's question from it
// (TenantDeclarations) with no read and no wait; the generated write paths of the tenant
// record and of the directories record call Reload after their commit, so a declaration
// changed on this instance decides the next request here, and signal the tenants kind
// so every other instance reloads.
//
// The set sits behind an atomic pointer, so Has never waits on a reload or a writer and
// performs no read: a domain the set does not hold is unknown, whatever the table says
// at that moment. A nil roster holds no tenant, so an application that wires none fails
// closed on every tenant-scoped route. It is safe for concurrent use.
type TenantRoster struct {
	client       Client
	table        string
	keyColumn    string
	declarations *TenantDeclarationColumns
	backstop     time.Duration
	signals      TenantSignals

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

// WithTenantDeclarations has the roster read what each tenant declares about who may
// enter it beside its key: the columns the generated New<Record>Roster constructor
// supplies from the @directoryOnly and @tenantDirectory declarations. Without it every
// tenant's declaration is nil, which declares nothing.
func WithTenantDeclarations(columns *TenantDeclarationColumns) TenantRosterOption {
	return func(r *TenantRoster) {
		r.declarations = columns
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
	if err := r.Reload(ctx); err != nil {
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
		if err := r.Reload(ctx); err != nil && ctx.Err() == nil {
			logger.FromCtx(ctx).Errorf("tenants: rereading the %s table failed; the roster stays as it was until the next signal or the backstop: %v", r.table, err)
		}
	}
}

// Reload rereads the table, the declarations with it, and replaces the set whole: what
// the tenant record's and the directories record's generated write paths call after
// their commit, so the instance that wrote serves the change at its next request, and
// what Start calls on every tenants signal and backstop tick. A failed read leaves the
// set as it was. A nil roster reads nothing.
func (r *TenantRoster) Reload(ctx context.Context) error {
	if r == nil {
		return errors.New("resource: no tenant roster is wired")
	}
	set, err := r.read(ctx)
	if err != nil {
		return err
	}

	r.writes.Lock()
	defer r.writes.Unlock()
	r.set.Store(&set)

	return nil
}

// read runs the roster's statements outside any request transaction, in one read-only
// snapshot, and returns the tenants as a set.
func (r *TenantRoster) read(ctx context.Context) (tenantSet, error) {
	dbType := r.client.DBType()
	statements, err := tenantRosterStatements(dbType, r.table, r.keyColumn, r.declarations, false)
	if err != nil {
		return nil, err
	}
	if dbType != SpannerDBType {
		return nil, errTenantRosterUnsupportedDatabase
	}

	txn := r.client.ReadOnlyTransaction()
	defer txn.Close()

	return readTenantSet(ctx, txn.SpannerReadOnlyTransaction(), statements, nil)
}

// rosterStatements are the roster's statements: the keys, each with its directory-only
// flag (FALSE where the record declares none), and the directories, empty where the
// roster reads no declaration. Filtered statements take the tenant as @domain.
type rosterStatements struct {
	Keys        string
	Directories string
}

// tenantRosterStatements renders the roster's statements for the database type: the
// key column as text under the name the scan reads, with the directory-only column
// beside it, and the directories record's rows, each identifier quoted the way the
// package's other statements quote them (backticks on Spanner, double quotes on
// Postgres). With filtered the statements read one tenant, the @domain parameter.
func tenantRosterStatements(dbType DBType, table, keyColumn string, columns *TenantDeclarationColumns, filtered bool) (rosterStatements, error) {
	var g *sqlGenerator
	var text, domainParam string
	switch dbType {
	case SpannerDBType:
		g, text, domainParam = newSQLGenerator(Spanner), columnTypeString, "@"+domainParamName
	case PostgresDBType:
		g, text, domainParam = newSQLGenerator(PostgreSQL), "TEXT", "$1"
	default:
		return rosterStatements{}, errors.Newf("resource: tenant roster: unsupported database type %q", dbType)
	}
	quote := func(column string) string {
		if dbType == PostgresDBType {
			return g.quoteIdentifier(column)
		}

		return column
	}
	directoryOnly := "FALSE"
	if columns.declared() && columns.DirectoryOnly != "" {
		directoryOnly = g.quoteIdentifier(columns.DirectoryOnly)
	}
	out := rosterStatements{
		Keys: fmt.Sprintf("SELECT CAST(%s AS %s) AS %s, %s AS %s FROM %s", g.quoteIdentifier(keyColumn), text, quote("Domain"), directoryOnly, quote("DirectoryOnly"), g.quoteIdentifier(table)),
	}
	if filtered {
		out.Keys += fmt.Sprintf(" WHERE CAST(%s AS %s) = %s", g.quoteIdentifier(keyColumn), text, domainParam)
	}
	if !columns.declared() {
		return out, nil
	}
	out.Directories = fmt.Sprintf("SELECT CAST(%s AS %s) AS %s, %s AS %s, %s AS %s FROM %s",
		g.quoteIdentifier(columns.TenantColumn), text, quote("Domain"), g.quoteIdentifier(columns.MethodColumn), quote("Method"), g.quoteIdentifier(columns.ConnectionColumn), quote("Connection"), g.quoteIdentifier(columns.DirectoriesTable))
	if filtered {
		out.Directories += fmt.Sprintf(" WHERE CAST(%s AS %s) = %s", g.quoteIdentifier(columns.TenantColumn), text, domainParam)
	}
	out.Directories += fmt.Sprintf(" ORDER BY %s, %s", g.quoteIdentifier(columns.MethodColumn), g.quoteIdentifier(columns.ConnectionColumn))

	return out, nil
}

// readTenantSet runs the roster's statements through a Spanner transaction and
// returns the tenants as a set: each key with its declaration when the directories
// statement is rendered, nil otherwise. params carry the filtered statements' tenant.
func readTenantSet(ctx context.Context, txn spxapi.Querier, statements rosterStatements, params map[string]any) (tenantSet, error) {
	var rows []struct {
		Domain        string `spanner:"Domain"`
		DirectoryOnly bool   `spanner:"DirectoryOnly"`
	}
	if err := spxscan.Select(ctx, txn, &rows, spanner.Statement{SQL: statements.Keys, Params: params}); err != nil {
		return nil, errors.Wrap(err, "spxscan.Select()")
	}
	declared := statements.Directories != ""
	set := make(tenantSet, len(rows))
	for _, row := range rows {
		var declaration *signin.TenantDeclaration
		if declared {
			declaration = &signin.TenantDeclaration{DirectoryOnly: row.DirectoryOnly}
		}
		set[accesstypes.Domain(row.Domain)] = declaration
	}
	if !declared {
		return set, nil
	}

	var directoryRows []struct {
		Domain     string `spanner:"Domain"`
		Method     string `spanner:"Method"`
		Connection string `spanner:"Connection"`
	}
	if err := spxscan.Select(ctx, txn, &directoryRows, spanner.Statement{SQL: statements.Directories, Params: params}); err != nil {
		return nil, errors.Wrap(err, "spxscan.Select()")
	}
	for _, row := range directoryRows {
		declaration := set[accesstypes.Domain(row.Domain)]
		if declaration == nil {
			// A directory of a tenant the record does not hold declares nothing.
			continue
		}
		declaration.Directories = append(declaration.Directories, signin.Directory{Method: sessioninfo.AuthMethod(row.Method), Connection: row.Connection})
	}

	return set, nil
}

// NewTenantDeclarationReader returns the reader of a tenant's declaration for the
// sign-in policy (signin.TenantPolicy): it reads the tenant record's row and the
// directories record's rows for the tenant in the sign-in's own transaction, on
// Spanner, where the session's transaction is *spanner.ReadWriteTransaction. The
// generated New<Record>DeclarationReader constructor supplies the table and the
// columns; an application calls that one. A tenant the table does not hold declares
// nothing (nil). The reader has no side effects, as a retried transaction needs.
func NewTenantDeclarationReader(table, keyColumn string, columns *TenantDeclarationColumns) signin.DeclarationReader[*spanner.ReadWriteTransaction] {
	return func(ctx context.Context, txn *spanner.ReadWriteTransaction, tenant accesstypes.Domain) (*signin.TenantDeclaration, error) {
		if tenant == "" {
			return nil, nil
		}
		statements, err := tenantRosterStatements(SpannerDBType, table, keyColumn, columns, true)
		if err != nil {
			return nil, err
		}
		set, err := readTenantSet(ctx, txn, statements, map[string]any{domainParamName: string(tenant)})
		if err != nil {
			return nil, err
		}

		return set[tenant], nil
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

// Declarations answers what the domain declares about who may enter it, from the copy:
// the TenantDeclarations an application hands SessionPermissions, so the session's
// tenant list and the generated DomainGuard decide with no read and no wait. A tenant
// the set does not hold, a tenant that declares nothing, and every tenant of a roster
// built without WithTenantDeclarations answer nil, which declares nothing. A nil roster
// answers nil too. It never fails.
func (r *TenantRoster) Declarations(_ context.Context, domain accesstypes.Domain) (*signin.TenantDeclaration, error) {
	if r == nil {
		return nil, nil
	}
	set := r.set.Load()
	if set == nil {
		return nil, nil
	}

	return (*set)[domain], nil
}

// Add puts the domain in the set at once, declaring nothing: what a test harness calls
// to hold a tenant the schema has no row for. The generated write paths call Reload
// instead, so the tenant's declaration comes with it. A nil roster takes nothing.
func (r *TenantRoster) Add(domain accesstypes.Domain) {
	if r == nil {
		return
	}
	r.replace(func(set tenantSet) {
		set[domain] = nil
	})
}

// Remove drops the domain from the set at once. A nil roster drops nothing.
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
	for domain, declaration := range current {
		next[domain] = declaration
	}
	change(next)
	r.set.Store(&next)
}
