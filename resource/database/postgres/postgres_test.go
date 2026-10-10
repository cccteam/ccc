package postgres_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/database/postgres"
	"github.com/cccteam/ccc/resource/database/postgres/declaration"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/internal/declarationtest"
	initiator "github.com/cccteam/db-initiator"
	"github.com/google/go-cmp/cmp"
	"github.com/sethvargo/go-envconfig"
)

// postgresVersion pins the PostgreSQL image the driver's tests open against, the major
// version db-initiator's own tests run.
const postgresVersion = "16"

// sharedContainer is the one PostgreSQL container the package's tests share, started on
// first demand and stopped when the test binary exits.
var sharedContainer struct {
	once      sync.Once
	container *initiator.PostgresContainer
	err       error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if c := sharedContainer.container; c != nil {
		if err := c.Terminate(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		c.Close()
	}
	os.Exit(code)
}

// postgresContainer returns the shared container, starting it on first demand. Under
// -short the calling test skips instead, so the unit run never needs a container
// runtime.
func postgresContainer(t *testing.T) *initiator.PostgresContainer {
	t.Helper()

	if testing.Short() {
		t.Skip("requires a PostgreSQL container")
	}
	sharedContainer.once.Do(func() {
		sharedContainer.container, sharedContainer.err = initiator.NewPostgresContainer(context.Background(), postgresVersion)
	})
	if sharedContainer.err != nil {
		t.Fatalf("initiator.NewPostgresContainer() error = %v", sharedContainer.err)
	}

	return sharedContainer.container
}

// TestSettings reads the settings from an environment: the host, database and user are
// required, the port and the SSL mode have defaults, and the password is optional.
func TestSettings(t *testing.T) {
	t.Parallel()

	full := map[string]string{"APP_POSTGRES_HOST": "db.internal", "APP_POSTGRES_PORT": "6432", "APP_POSTGRES_DATABASE": "harbor", "APP_POSTGRES_USER": "harbor", "APP_POSTGRES_PASSWORD": "s3cret", "APP_POSTGRES_SSL_MODE": "verify-full"}
	without := func(name string) map[string]string {
		env := map[string]string{}
		for k, v := range full {
			if k != name {
				env[k] = v
			}
		}

		return env
	}
	tests := []struct {
		name    string
		env     map[string]string
		want    postgres.Settings
		wantErr string
	}{
		{
			name: "every variable set",
			env:  full,
			want: postgres.Settings{Host: "db.internal", Port: "6432", Database: "harbor", User: "harbor", Password: "s3cret", SSLMode: "verify-full"},
		},
		{
			name: "the port and the SSL mode default, and the password may stay empty",
			env:  map[string]string{"APP_POSTGRES_HOST": "/cloudsql/p:r:i", "APP_POSTGRES_DATABASE": "harbor", "APP_POSTGRES_USER": "harbor"},
			want: postgres.Settings{Host: "/cloudsql/p:r:i", Port: "5432", Database: "harbor", User: "harbor", SSLMode: "require"},
		},
		{name: "the host is required", env: without("APP_POSTGRES_HOST"), wantErr: "APP_POSTGRES_HOST"},
		{name: "the database is required", env: without("APP_POSTGRES_DATABASE"), wantErr: "APP_POSTGRES_DATABASE"},
		{name: "the user is required", env: without("APP_POSTGRES_USER"), wantErr: "APP_POSTGRES_USER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got postgres.Settings
			err := envconfig.ProcessWith(t.Context(), &envconfig.Config{Target: &got, Lookuper: envconfig.MapLookuper(tt.env)})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("envconfig.ProcessWith() error = %v, want one naming %s", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("envconfig.ProcessWith() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("settings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestOpen opens the driver against a container: the pool answers and the resource
// client is the PostgreSQL client, holding the file store the options wire on it; a wrong
// password, a wrong database and a wrong port are refused at Open, naming the check; and
// Close closes the pool.
func TestOpen(t *testing.T) {
	t.Parallel()

	db, err := postgresContainer(t).CreateDatabase(t.Context(), "driver")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	t.Cleanup(db.Close)
	conn := db.Config().ConnConfig
	settings := postgres.Settings{
		Host:     conn.Host,
		Port:     strconv.Itoa(int(conn.Port)),
		Database: conn.Database,
		User:     conn.User,
		Password: conn.Password,
		SSLMode:  "disable",
	}

	tests := []struct {
		name string
		edit func(s *postgres.Settings)
		// store wires a memory store on the client, which FileStore must answer.
		store   bool
		wantErr string
	}{
		{name: "the container's database opens"},
		{name: "the file store the options wire is the client's", store: true},
		{name: "a wrong password is refused", edit: func(s *postgres.Settings) { s.Password = "wrong" }, wantErr: "pgxpool.Pool.Ping()"},
		{name: "a database that does not exist is refused", edit: func(s *postgres.Settings) { s.Database = "absent" }, wantErr: "pgxpool.Pool.Ping()"},
		{name: "a port nothing listens on is refused", edit: func(s *postgres.Settings) { s.Port = "1" }, wantErr: "pgxpool.Pool.Ping()"},
		{name: "a port that is not a number is refused when the settings are parsed", edit: func(s *postgres.Settings) { s.Port = "port" }, wantErr: "pgxpool.ParseConfig()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := settings
			if tt.edit != nil {
				tt.edit(&s)
			}
			var opts []resource.ClientOption
			var store *filestore.Mem
			if tt.store {
				store = filestore.NewMem()
				opts = append(opts, resource.WithFileStore(store))
			}
			d, err := postgres.Open(t.Context(), s, opts...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Open() error = %v, want one from %s", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if d.ResourceClient.DBType() != resource.PostgresDBType {
				t.Errorf("ResourceClient.DBType() = %v, want PostgreSQL", d.ResourceClient.DBType())
			}
			if got := d.ResourceClient.FileStore(resource.DefaultStore); (store == nil && got != nil) || (store != nil && got != resource.FileStore(store)) {
				t.Errorf("ResourceClient.FileStore(DefaultStore) = %v, want the store the options wired (%v)", got, store)
			}
			var one int
			if err := d.Pool.QueryRow(t.Context(), "SELECT 1").Scan(&one); err != nil || one != 1 {
				t.Errorf("SELECT 1 = %d, %v", one, err)
			}
			d.Close()
			if err := d.Pool.Ping(t.Context()); err == nil {
				t.Error("Ping() after Close() succeeded, want the pool closed")
			}
		})
	}
}

// TestSettingsDeclaration holds the declaration the declaration package publishes to the
// struct, as the cloud driver's test does.
func TestSettingsDeclaration(t *testing.T) {
	t.Parallel()

	declarationtest.Hold(t, reflect.TypeFor[postgres.Settings](), declaration.Settings())
}
