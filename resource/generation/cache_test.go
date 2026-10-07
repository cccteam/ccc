package generation

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cccteam/ccc/cache"
	"github.com/google/go-cmp/cmp"
)

// cacheTestEmulatorVersion is the Spanner emulator the cache tests read the schema with.
const cacheTestEmulatorVersion = "1.5.56"

// Test_newClient_concurrent pins that generators running at once over one package share
// its generation cache without failing: go test runs an application's packages in
// parallel, and two of them regenerating at once had one delete the migration cache entry
// the other was still writing. Each generator here opens its client, stores the schema as
// Generate does, and closes the client; all succeed, read the same tables, and leave a
// cache the next run finds clean. Requires the Spanner emulator (podman/docker).
func Test_newClient_concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("reading the schema requires the Spanner emulator")
	}
	// The generator keeps its cache at the root of the module it runs in, found from the
	// working directory, which belongs to the whole process: no t.Parallel here.
	t.Chdir(newCacheTestModule(t))

	tests := []struct {
		name       string
		generators int
		// filled runs one generator before the concurrent ones, so they start on the
		// cache's fast path rather than reading the schema through the emulator.
		filled bool
	}{
		{name: "two generators over an empty cache", generators: 2},
		{name: "three generators over an empty cache", generators: 3},
		{name: "two generators over a filled cache", generators: 2, filled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A migration source of its own gives the case its own cache entries, so it
			// starts empty whatever the cases before it stored.
			migrations := []string{copyFixtureMigrations(t)}
			if tt.filled {
				if _, err := generateOnce(t.Context(), migrations); err != nil {
					t.Fatalf("generateOnce() filling the cache error = %v", err)
				}
			}

			tables := make([][]string, tt.generators)
			errs := make([]error, tt.generators)
			var wg sync.WaitGroup
			for i := range tt.generators {
				wg.Go(func() {
					tables[i], errs[i] = generateOnce(t.Context(), migrations)
				})
			}
			wg.Wait()

			for i, err := range errs {
				if err != nil {
					t.Errorf("generator %d error = %v", i, err)
				}
			}
			if t.Failed() {
				return
			}
			if len(tables[0]) == 0 {
				t.Fatal("the first generator read no tables")
			}
			for i := 1; i < len(tables); i++ {
				if diff := cmp.Diff(tables[0], tables[i]); diff != "" {
					t.Errorf("generator %d read other tables than generator 0 (-0 +%d):\n%s", i, i, diff)
				}
			}

			c, err := newClient(t.Context(), "pkg/resources", migrations, []option{WithSpannerEmulatorVersion(cacheTestEmulatorVersion)})
			if err != nil {
				t.Fatalf("newClient() after the generators error = %v", err)
			}
			defer c.Close()
			if clean, err := c.isSchemaClean(); err != nil || !clean {
				t.Errorf("isSchemaClean() after the generators = %v, %v; want true, nil", clean, err)
			}
		})
	}
}

// Test_NewResourceGenerator_failureReleasesCache pins that a generator whose construction
// fails closes its cache: the cache holds a lock on the package's cache until it is
// closed, and a caller never closes a generator it was not given, so the next generator
// in the same process would otherwise wait for it.
func Test_NewResourceGenerator_failureReleasesCache(t *testing.T) {
	// The generator keeps its cache at the root of the module it runs in, found from the
	// working directory, which belongs to the whole process: no t.Parallel here.
	dir := newCacheTestModule(t)
	t.Chdir(dir)

	tests := []struct {
		name       string
		migrations func(t *testing.T) string
		options    []ResourceOption
		// emulator marks a case that reads the schema before it fails.
		emulator bool
		wantErr  string
	}{
		{
			name: "a migration source that does not exist fails in the client",
			migrations: func(t *testing.T) string {
				return "file://" + filepath.Join(t.TempDir(), "missing")
			},
			wantErr: "no such file or directory",
		},
		{
			name:       "an option the generator refuses fails after the client",
			migrations: copyFixtureMigrations,
			options:    []ResourceOption{WithRouterOutlet(defaultOutletName, "machine")},
			emulator:   true,
			wantErr:    "redeclares the reserved default outlet",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.emulator && testing.Short() {
				t.Skip("reading the schema requires the Spanner emulator")
			}

			options := append(slices.Clone(tt.options), WithSpannerEmulatorVersion(cacheTestEmulatorVersion))
			g, err := NewResourceGenerator(t.Context(), "pkg/resources", []string{tt.migrations(t)}, options...)
			if err == nil {
				_ = g.Close()
				t.Fatalf("NewResourceGenerator() error = nil, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewResourceGenerator() error = %v, want one containing %q", err, tt.wantErr)
			}

			opened := make(chan error, 1)
			go func() {
				c, err := cache.New(dir)
				if err == nil {
					err = c.Close()
				}
				opened <- err
			}()
			select {
			case err := <-opened:
				if err != nil {
					t.Errorf("cache.New() after the failed construction error = %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("cache.New() still waits: the failed construction left the cache locked")
			}
		})
	}
}

// generateOnce is one generator's use of the cache: it opens the client, which reads the
// schema through the emulator or loads it from the cache, stores the schema as Generate
// does, and closes the client. It answers the tables the client read.
func generateOnce(ctx context.Context, migrations []string) ([]string, error) {
	c, err := newClient(ctx, "pkg/resources", migrations, []option{WithSpannerEmulatorVersion(cacheTestEmulatorVersion)})
	if err != nil {
		return nil, err
	}
	tables := slices.Sorted(maps.Keys(c.tableMap))
	if err := c.populateCache(); err != nil {
		_ = c.Close()

		return nil, err
	}
	if err := c.Close(); err != nil {
		return nil, err
	}

	return tables, nil
}

// newCacheTestModule makes an empty module for a cache test to run the generator in, so
// the test's cache is its own and not the resource module's.
func newCacheTestModule(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/cachetest\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	return dir
}

// copyFixtureMigrations copies the generation fixture's migrations into a directory of
// the test's own and answers it as a migration source URL. The cache keys its entries by
// the source URL, so a fresh copy starts on an empty cache entry.
func copyFixtureMigrations(t *testing.T) string {
	t.Helper()

	return copyMigrations(t, "migrations")
}

// copyMigrations copies one of the fixture migration directories under testdata.
func copyMigrations(t *testing.T, name string) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	fixture := filepath.Join(filepath.Dir(thisFile), "testdata", name)
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(fixture)); err != nil {
		t.Fatalf("os.CopyFS() error = %v", err)
	}

	return "file://" + dir
}

// Test_newClient_postgres runs the client over PostgreSQL as the generator does: WithPostgres
// reads the schema through a container, and the table map is cached apart from Spanner's, so
// the second client over the same migrations loads it without one. The client keeps its cache
// at the root of the module it runs in, found from the working directory, which belongs to the
// whole process: no t.Parallel here.
func Test_newClient_postgres(t *testing.T) {
	t.Chdir(newCacheTestModule(t))
	if testing.Short() {
		t.Skip("reading the schema requires a PostgreSQL container")
	}

	migrations := []string{copyMigrations(t, "postgresmigrations")}

	c, err := newClient(t.Context(), "pkg/resources", migrations, []option{WithPostgres("17")})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if c.postgresVersion != "17" {
		t.Errorf("postgresVersion = %q, want 17", c.postgresVersion)
	}
	if _, ok := c.tableMap["Orders"]; !ok {
		t.Fatalf("the table map has no Orders: %v", len(c.tableMap))
	}
	if err := c.populateCache(); err != nil {
		t.Fatalf("populateCache() error = %v", err)
	}
	cachePath, err := c.schemaCachePath()
	if err != nil {
		t.Fatalf("schemaCachePath() error = %v", err)
	}
	if !strings.HasPrefix(cachePath, "postgres"+string(filepath.Separator)) {
		t.Errorf("schemaCachePath() = %q, want it under postgres", cachePath)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// The same migrations over Spanner would not find these entries.
	spannerClient := &client{migrationSourceURLs: migrations}
	spannerPath, err := spannerClient.schemaCachePath()
	if err != nil {
		t.Fatalf("schemaCachePath() error = %v", err)
	}
	if spannerPath == cachePath {
		t.Errorf("Spanner and PostgreSQL share the cache path %q", cachePath)
	}

	// With the schema unchanged and cached, the second client reads no database: an image
	// version no registry has would fail a container start, and it does not.
	cached, err := newClient(t.Context(), "pkg/resources", migrations, []option{WithPostgres("no-such-image-version")})
	if err != nil {
		t.Fatalf("newClient() over the cached schema error = %v", err)
	}
	defer cached.Close()
	if diff := cmp.Diff(slices.Sorted(maps.Keys(c.tableMap)), slices.Sorted(maps.Keys(cached.tableMap))); diff != "" {
		t.Errorf("the cached table map differs from the one read (-read +cached):\n%s", diff)
	}
}

// Test_NewResourceGenerator_postgresRefusesHandlerTests pins that the handler test suite,
// which runs over the Spanner emulator, is refused with PostgreSQL.
func Test_NewResourceGenerator_postgresRefusesHandlerTests(t *testing.T) {
	t.Chdir(newCacheTestModule(t))
	if testing.Short() {
		t.Skip("reading the schema requires a PostgreSQL container")
	}

	g, err := NewResourceGenerator(t.Context(), "pkg/resources", []string{copyMigrations(t, "postgresmigrations")},
		WithPostgres("17"), GenerateHandlers("handlers"), GenerateRoutes("routes", "/api"), GenerateHandlerTests("handlertests"))
	if err == nil {
		_ = g.Close()
		t.Fatal("NewResourceGenerator() error = nil, want the handler tests refused")
	}
	if !strings.Contains(err.Error(), "not available with WithPostgres") {
		t.Errorf("NewResourceGenerator() error = %v, want it to say the handler tests are not available with WithPostgres", err)
	}
}
