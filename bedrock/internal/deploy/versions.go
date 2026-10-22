// versions.go reads a database's schema migration version through the Spanner data API
// (a session, one SELECT, the session deleted), as the build's own identity: the migrate
// step reads the database in front of it before a consolidation runs, and the migration
// guard reads each environment's database on a consolidation pull request, through the
// migrations_reader database role the application's first migration creates (select on
// the SchemaMigrations table alone), which 2-env and 2-spn grant the first environment's
// deploy identity on the application's databases.

package deploy

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
)

// The schema migrations table the runner keeps, which the reader role holds select on, and
// the database role.
const (
	schemaMigrationsTable = "SchemaMigrations"
	migrationsReaderRole  = "migrations_reader"
)

// MigrationVersion is what the schema migrations table says about a database: the
// version applied (Known false when the table holds no row, or does not exist yet: a
// database nothing has run on, which the migrate command's first file gives the table),
// and whether a file is in progress.
type MigrationVersion struct {
	Number int
	Known  bool
	Dirty  bool
}

// noVersion is the version of a database whose migrations table holds no row.
const noVersion = "no version"

// String spells the version as the migrate command's report does.
func (v MigrationVersion) String() string {
	switch {
	case !v.Known:
		return noVersion
	case v.Dirty:
		return fmt.Sprintf("dirty at %d", v.Number)
	default:
		return strconv.Itoa(v.Number)
	}
}

// Versions reads the schema migration version of a Spanner database.
type Versions interface {
	// Version reads the schema migrations table's row of the database
	// (projects/<p>/instances/<i>/databases/<d>): through the database role when one is
	// named (the session is created as it), else as the identity itself. A database that
	// does not exist is a not-found error (isNotFound); a read the identity may not make
	// is a permission error (notYet).
	Version(ctx context.Context, database, role string) (MigrationVersion, error)
}

// VersionsFunc opens Versions.
type VersionsFunc func(ctx context.Context) (Versions, error)

// NewVersions opens the Spanner v1 REST API with the process's default credentials, the
// build's identity.
func NewVersions(ctx context.Context) (Versions, error) {
	client, err := google.DefaultClient(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &versions{spanner: &cloudRun{http: client, base: spannerAPI, service: serviceSpanner}}, nil
}

// versions reads through the shared REST caller.
type versions struct {
	spanner *cloudRun
}

func (v *versions) Version(ctx context.Context, database, role string) (MigrationVersion, error) {
	body := map[string]any{}
	if role != "" {
		body["session"] = map[string]any{"creatorRole": role}
	}
	session, err := v.spanner.call(ctx, http.MethodPost, "/v1/"+database+"/sessions", body)
	if err != nil {
		return MigrationVersion{}, err
	}
	name := text(session, keyName)
	defer func() {
		_, _ = v.spanner.call(context.WithoutCancel(ctx), http.MethodDelete, "/v1/"+name, nil)
	}()
	answer, err := v.spanner.call(ctx, http.MethodPost, "/v1/"+name+":executeSql", map[string]any{"sql": "SELECT Version, Dirty FROM " + schemaMigrationsTable + " LIMIT 1"})
	switch {
	case tableNotFound(err):
		return MigrationVersion{}, nil
	case err != nil:
		return MigrationVersion{}, err
	}

	return versionRow(answer)
}

// tableNotFound reports Spanner answering that the schema migrations table does not
// exist: a database nothing has run on (a pull request's, fresh from its stack), which
// reads as no version, as a table with no row does. The migrate command's first file
// creates the table.
func tableNotFound(err error) bool {
	var e *apiError

	return errors.As(err, &e) && e.status == http.StatusBadRequest && strings.Contains(e.message, "Table not found: "+schemaMigrationsTable)
}

// versionRow reads the version from an executeSql answer: no rows is no version; a row
// is the version as a string (INT64 travels as a JSON string) and the dirty flag.
func versionRow(answer map[string]any) (MigrationVersion, error) {
	rows, _ := answer["rows"].([]any)
	if len(rows) == 0 {
		return MigrationVersion{}, nil
	}
	row, _ := rows[0].([]any)
	if len(row) < 2 {
		return MigrationVersion{}, errors.Newf("the %s row has %d column(s), want Version and Dirty", schemaMigrationsTable, len(row))
	}
	number, err := strconv.Atoi(fmt.Sprint(row[0]))
	if err != nil {
		return MigrationVersion{}, errors.Wrapf(err, "the %s row's Version %v is not an integer", schemaMigrationsTable, row[0])
	}
	dirty, _ := row[1].(bool)

	return MigrationVersion{Number: number, Known: true, Dirty: dirty}, nil
}
