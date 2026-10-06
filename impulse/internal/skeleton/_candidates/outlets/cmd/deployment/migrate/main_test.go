package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	initiator "github.com/cccteam/db-initiator"
)

// The emulator's own project and instance: db-initiator creates the instance when the
// container starts, and each case creates a database on it. versionsTable is where the
// runner records the schema versions, whose row a case edits to stand for a migration
// file that stopped part way with its progress recorded.
const (
	containerProjectID  = "unit-testing"
	containerInstanceID = "test-instance"
	versionsTable       = "SchemaMigrations"
	migrationsDir       = "schema/migrations"
	seedDir             = "schema/devseed"
)

// serviceName is the service name the migration run's core level reads
// (APP_SERVICE_NAME); any name serves the emulators.
const serviceName = "migrate-test"

// The package's emulators: the Spanner container the cases create their databases on,
// and the Firestore emulator the migration run's live service opens. The data level
// requires the live service, and a migration run finds its Firestore database as the
// deployment's job does (APP_FIRESTORE_DATABASE) or as the development stack does
// (FIRESTORE_EMULATOR_HOST), which is how the cases hand it the emulator.
var (
	container         *initiator.SpannerContainer
	firestoreEmulator *initiator.FirestoreContainer
)

// TestMain starts one Spanner emulator and one Firestore emulator for the package; the
// cases publish them in the environment, which is how the deployment's job finds its
// databases. The Firestore emulator runs from the Cloud SDK emulators image the Procfile
// starts, by the SDK version, and without rules, since the live service writes with the
// emulator's owner credential, which rules never apply to.
func TestMain(m *testing.M) {
	ctx := context.Background()

	c, err := initiator.NewSpannerContainer(ctx, "1.5.56")
	if err != nil {
		log.Fatal(err)
	}
	container = c

	f, err := initiator.NewFirestoreContainer(ctx, "562.0.0")
	if err != nil {
		stopSpanner(ctx, c)
		log.Fatal(err)
	}
	firestoreEmulator = f

	exitCode := m.Run()

	stopFirestore(ctx, f)
	stopSpanner(ctx, c)

	os.Exit(exitCode)
}

// stopSpanner terminates the Spanner emulator's container and closes the handle on it.
func stopSpanner(ctx context.Context, c *initiator.SpannerContainer) {
	if err := c.Terminate(ctx); err != nil {
		fmt.Println(err)
	}
	if err := c.Close(); err != nil {
		fmt.Println(err)
	}
}

// stopFirestore terminates the Firestore emulator's container and closes the handle on
// it.
func stopFirestore(ctx context.Context, f *initiator.FirestoreContainer) {
	if err := f.Terminate(ctx); err != nil {
		fmt.Println(err)
	}
	if err := f.Close(); err != nil {
		fmt.Println(err)
	}
}

// TestExecute runs the command's flags against the emulators, each case on a database of
// its own: the migrations with the seed, the report on a fresh, a migrated and a dirty
// database, the forces with the row before and after, the refusals before the database is
// touched, and the exit codes. The command reads its target from the environment, so the
// cases run one at a time, from the module root, where the migrations are read from as
// they are in the image.
func TestExecute(t *testing.T) {
	ctx := context.Background()
	t.Chdir(filepath.Join("..", "..", ".."))
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "9010/tcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPANNER_EMULATOR_HOST", host+":"+port.Port())
	t.Setenv("GOOGLE_CLOUD_SPANNER_PROJECT", containerProjectID)
	t.Setenv("GOOGLE_CLOUD_SPANNER_INSTANCE_ID", containerInstanceID)
	// The migration run opens the data level, which reads the core level's service name
	// and opens the live service, required, over the Firestore emulator.
	t.Setenv("APP_SERVICE_NAME", serviceName)
	t.Setenv("FIRESTORE_EMULATOR_HOST", firestoreEmulator.Host())
	last := lastMigration(t)

	tests := []struct {
		name string
		// prepare readies the case's database; nil leaves it fresh.
		prepare func(t *testing.T, db *initiator.SpannerDB)
		// missing points the command at a database that does not exist: a refusal must
		// come before the database is touched, and a run against it fails.
		missing  bool
		args     []string
		wantCode int
		// wantOut is the whole of stdout; wantErr a fragment of stderr.
		wantOut string
		wantErr string
		// wantAfter is what -version reports once the case has run; empty skips the check.
		wantAfter string
	}{
		{
			name:      "the migrations with the seed apply every schema and data migration",
			args:      []string{"-seed"},
			wantAfter: fmt.Sprintf("schema: version %d\ndata: %s\n", last, seededData(t)),
		},
		{
			name:    "the report on a fresh database",
			args:    []string{"-version"},
			wantOut: "schema: no version\ndata: no version\n",
		},
		{
			name:    "the report on a migrated database",
			prepare: migrated,
			args:    []string{"-version"},
			wantOut: fmt.Sprintf("schema: version %d\ndata: no version\n", last),
		},
		{
			name:    "the report on a database dirty with progress",
			prepare: dirty,
			args:    []string{"-version"},
			wantOut: fmt.Sprintf("schema: version %d, dirty: 2 statements applied\ndata: no version\n", last),
		},
		{
			name:      "a force sets the schema version and prints the row before and after",
			prepare:   dirty,
			args:      []string{"-force", strconv.Itoa(last - 1)},
			wantOut:   fmt.Sprintf("schema: version %d, dirty: 2 statements applied\nforced schema to version %d\nschema: version %d\n", last, last-1, last-1),
			wantAfter: fmt.Sprintf("schema: version %d\ndata: no version\n", last-1),
		},
		{
			name:      "a force to -1 leaves no version",
			prepare:   migrated,
			args:      []string{"-force", "-1"},
			wantOut:   fmt.Sprintf("schema: version %d\nforced schema to no version\nschema: no version\n", last),
			wantAfter: "schema: no version\ndata: no version\n",
		},
		{
			name:      "a data force sets the data version",
			args:      []string{"-force-data", "1"},
			wantOut:   "data: no version\nforced data to version 1\ndata: version 1\n",
			wantAfter: "schema: no version\ndata: version 1\n",
		},
		{
			name:      "both forces together, the schema first",
			prepare:   migrated,
			args:      []string{"-force-data", "2", "-force", strconv.Itoa(last - 1)},
			wantOut:   fmt.Sprintf("schema: version %d\nforced schema to version %d\nschema: version %d\ndata: no version\nforced data to version 2\ndata: version 2\n", last, last-1, last-1),
			wantAfter: fmt.Sprintf("schema: version %d\ndata: version 2\n", last-1),
		},
		{
			name:     "a force with the seed is refused before the database is touched",
			missing:  true,
			args:     []string{"-force", "40", "-seed"},
			wantCode: exitRefused,
			wantErr:  "-force and -force-data apply no migration: -seed cannot go with them",
		},
		{
			name:     "a force with the report is refused",
			missing:  true,
			args:     []string{"-force-data", "1", "-version"},
			wantCode: exitRefused,
			wantErr:  "-version reports and -force changes: give one or the other",
		},
		{
			name:     "the report with the seed is refused",
			missing:  true,
			args:     []string{"-version", "-seed"},
			wantCode: exitRefused,
			wantErr:  "-version applies no migration: -seed cannot go with it",
		},
		{
			name:     "a force without a value is refused",
			missing:  true,
			args:     []string{"-force"},
			wantCode: exitRefused,
			wantErr:  "flag needs an argument: -force",
		},
		{
			name:     "a force with a non-integer is refused",
			missing:  true,
			args:     []string{"-force", "forty"},
			wantCode: exitRefused,
			wantErr:  `"forty" is not a version: an integer 0 or above, or -1 for no version`,
		},
		{
			name:     "a force below -1 is refused",
			missing:  true,
			args:     []string{"-force", "-2"},
			wantCode: exitRefused,
			wantErr:  `"-2" is not a version`,
		},
		{
			name:     "a stray argument is refused",
			missing:  true,
			args:     []string{"-version", "extra"},
			wantCode: exitRefused,
			wantErr:  `the command takes no argument: "extra"`,
		},
		{
			name:     "a database that does not exist fails the run",
			missing:  true,
			args:     []string{"-version"},
			wantCode: exitFailed,
			wantErr:  "deploy.Versions()",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := "nothing-here"
			if !tt.missing {
				db := database(t)
				if tt.prepare != nil {
					tt.prepare(t, db)
				}
				name = path.Base(db.DatabaseName())
			}
			t.Setenv("GOOGLE_CLOUD_SPANNER_DATABASE_NAME", name)

			var stdout, stderr bytes.Buffer
			code := execute(t.Context(), tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("execute(%v) = %d, want %d; stdout:\n%s\nstderr:\n%s", tt.args, code, tt.wantCode, stdout.String(), stderr.String())
			}
			if stdout.String() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantOut)
			}
			if tt.wantErr != "" && !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stderr lacks %q:\n%s", tt.wantErr, stderr.String())
			}
			if tt.wantErr == "" && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want none", stderr.String())
			}
			if tt.wantAfter == "" {
				return
			}
			var after bytes.Buffer
			if code := execute(t.Context(), []string{"-version"}, &after, &stderr); code != 0 {
				t.Fatalf("execute(-version) after the run = %d; stderr:\n%s", code, stderr.String())
			}
			if after.String() != tt.wantAfter {
				t.Errorf("the report after the run = %q, want %q", after.String(), tt.wantAfter)
			}
		})
	}
}

// database creates a fresh database for the case and drops it when the case ends.
func database(t *testing.T) *initiator.SpannerDB {
	t.Helper()

	db, err := container.CreateDatabase(t.Context(), t.Name())
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

	return db
}

// migrated applies every schema migration the tree carries.
func migrated(t *testing.T, db *initiator.SpannerDB) {
	t.Helper()

	if err := db.MigrateUp("file://" + migrationsDir); err != nil {
		t.Fatalf("initiator.SpannerDB.MigrateUp() error = %v", err)
	}
}

// dirty migrates the database and then edits the version row into what the runner leaves
// when the last file stops at its third statement: dirty, two statements applied.
func dirty(t *testing.T, db *initiator.SpannerDB) {
	t.Helper()

	migrated(t, db)
	mutation := spanner.Update(versionsTable, []string{"Version", "Dirty", "Applied", "Checkpoint"}, []any{int64(lastMigration(t)), true, int64(2), "cafe"})
	if _, err := db.Apply(t.Context(), []*spanner.Mutation{mutation}); err != nil {
		t.Fatalf("spanner.Client.Apply() error = %v", err)
	}
}

// lastMigration is the highest version among the tree's schema migrations.
func lastMigration(t *testing.T) int {
	t.Helper()

	last, ok := lastIndex(t, migrationsDir)
	if !ok {
		t.Fatalf("no schema migrations under %s", migrationsDir)
	}

	return last
}

// seededData is what -version reports for the data table once the seed has run: the
// highest version among the seed's files, or no version while the application has no
// seed.
func seededData(t *testing.T) string {
	t.Helper()

	last, ok := lastIndex(t, seedDir)
	if !ok {
		return "no version"
	}

	return "version " + strconv.Itoa(last)
}

// lastIndex is the highest index among a migrations directory's up files; ok is false
// when the directory holds none or does not exist.
func lastIndex(t *testing.T, dir string) (last int, ok bool) {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		t.Fatalf("filepath.Glob() error = %v", err)
	}
	for _, file := range files {
		index, _, _ := strings.Cut(filepath.Base(file), "_")
		n, err := strconv.Atoi(index)
		if err != nil {
			t.Fatalf("%s does not start with a version: %v", file, err)
		}
		last = max(last, n)
	}

	return last, len(files) > 0
}
