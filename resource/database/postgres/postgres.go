// Package postgres is the PostgreSQL database driver: from the variables it declares, it
// opens a connection pool on the application's database and the resource client over it.
// An application embeds Settings in its configuration and calls Open, and names no
// database vendor in its own code; moving to another database swaps this import for that
// database's driver (resource/database/spanner). Bound under the neutral alias database
// (database "github.com/cccteam/ccc/resource/database/postgres"), with the settings
// embedded as database.Settings and the driver opened by database.Open, the swap is the
// import line alone.
//
// The driver opens the pool and wraps resource.NewPostgresClient; what the resource
// client itself can do on PostgreSQL is the resource package's to complete.
package postgres

import (
	"context"
	"strings"

	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Settings are the variables the driver reads, where the database is and how the
// application connects, declared on the application's configuration by embedding, so
// the stack and the development environment render them from the declaration. The
// declaration the tools read is declaration.Settings, in the package beside this one,
// which holds nothing of the driver.
type Settings struct {
	// Host is the server's host name or address, or the directory holding its Unix
	// socket (/cloudsql/<connection name> on Cloud Run).
	Host string `env:"APP_POSTGRES_HOST,required"`
	// Port is the server's port.
	Port string `env:"APP_POSTGRES_PORT,default=5432"`
	// Database is the database on the server.
	Database string `env:"APP_POSTGRES_DATABASE,required"`
	// User is the role the application connects as.
	User string `env:"APP_POSTGRES_USER,required"`
	// Password is the role's password, a secret that belongs in a secret store and never
	// in a committed file; empty where the server authenticates the connection another
	// way (the peer of a Unix socket, IAM on Cloud SQL).
	Password string `env:"APP_POSTGRES_PASSWORD"`
	// SSLMode is how the connection is protected, as PostgreSQL spells it (disable,
	// allow, prefer, require, verify-ca, verify-full): require unless set, and disable
	// against a local container.
	SSLMode string `env:"APP_POSTGRES_SSL_MODE,default=require"`
}

// connectionString renders the settings as the keyword/value connection string pgx
// parses, every value quoted so a space or a quote in it is read as written.
func (s Settings) connectionString() string {
	pairs := []struct{ key, value string }{
		{"host", s.Host},
		{"port", s.Port},
		{"dbname", s.Database},
		{"user", s.User},
		{"password", s.Password},
		{"sslmode", s.SSLMode},
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p.value == "" {
			continue
		}
		parts = append(parts, p.key+"="+quote(p.value))
	}

	return strings.Join(parts, " ")
}

// quote single-quotes a connection string value, escaping the backslashes and the
// quotes it holds.
func quote(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)

	return "'" + escaped + "'"
}

// Driver is what Open built: the pool over the database and the resource client over
// it, closed together.
type Driver struct {
	// ResourceClient is the resource client over the database: what the generated
	// handlers read and write through.
	ResourceClient *resource.PostgresClient
	// Pool is the connection pool the resource client is built over, for what else the
	// application runs on the database.
	Pool *pgxpool.Pool
}

// Open opens a connection pool on the database the settings name and checks it answers,
// so a wrong host, role or password fails the start and not the first request, then
// builds the resource client over the pool, with the file stores the options wire on it
// (resource.WithFileStore, resource.WithNamedFileStore), the options the Spanner driver's
// Open takes.
func Open(ctx context.Context, s Settings, opts ...resource.ClientOption) (*Driver, error) {
	config, err := pgxpool.ParseConfig(s.connectionString())
	if err != nil {
		return nil, errors.Wrap(err, "pgxpool.ParseConfig()")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.Wrap(err, "pgxpool.NewWithConfig()")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, errors.Wrap(err, "pgxpool.Pool.Ping()")
	}

	return &Driver{ResourceClient: resource.NewPostgresClient(pool, opts...), Pool: pool}, nil
}

// Close closes the pool, and with it every connection the resource client held.
func (d *Driver) Close() {
	d.Pool.Close()
}
