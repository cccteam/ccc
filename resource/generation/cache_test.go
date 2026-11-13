package generation

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
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

	missingMigrations := func(t *testing.T) string {
		return "file://" + filepath.Join(t.TempDir(), "missing")
	}
	tests := []struct {
		name       string
		migrations func(t *testing.T) string
		options    []ResourceOption
		// emulatorVersion is the version the generator names; none is the refusal's case.
		emulatorVersion string
		// emulator marks a case that reads the schema before it fails.
		emulator bool
		wantErr  string
	}{
		{
			name:       "no emulator version is refused before anything is read",
			migrations: copyFixtureMigrations,
			wantErr:    "no Spanner emulator version is set",
		},
		{
			name:            "a migration source that does not exist fails in the client",
			migrations:      missingMigrations,
			emulatorVersion: cacheTestEmulatorVersion,
			wantErr:         "no such file or directory",
		},
		{
			name:            "the latest tag is a version a generator may name, so the failure is the migration source's",
			migrations:      missingMigrations,
			emulatorVersion: latestEmulatorVersion,
			wantErr:         "no such file or directory",
		},
		{
			name:            "an option the generator refuses fails after the client",
			migrations:      copyFixtureMigrations,
			options:         []ResourceOption{WithRouterOutlet(defaultOutletName, "machine")},
			emulatorVersion: cacheTestEmulatorVersion,
			emulator:        true,
			wantErr:         "redeclares the reserved default outlet",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.emulator && testing.Short() {
				t.Skip("reading the schema requires the Spanner emulator")
			}

			options := slices.Clone(tt.options)
			if tt.emulatorVersion != "" {
				options = append(options, WithSpannerEmulatorVersion(tt.emulatorVersion))
			}
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

// Test_client_spannerCachePath pins that a cached schema is reused only by the emulator
// and generator versions that read it: a newer emulator can report an unchanged schema
// differently, and a cache from the older one would hide that change.
func Test_client_spannerCachePath(t *testing.T) {
	// readBuildInfo is package state: no t.Parallel here.
	migrations := []string{"file://schema/migrations", "file://schema/draft_migrations"}
	pathFor := func(t *testing.T, emulator, generator string, migrations []string) (path, schemaPath string) {
		t.Helper()

		setGeneratorVersion(t, generator)
		c := &client{migrationSourceURLs: migrations, spannerEmulatorVersion: emulator}
		path, err := c.spannerCachePath()
		if err != nil {
			t.Fatalf("spannerCachePath() error = %v", err)
		}
		schemaPath, err = c.spannerSchemaCachePath()
		if err != nil {
			t.Fatalf("spannerSchemaCachePath() error = %v", err)
		}

		return path, schemaPath
	}
	basePath, baseSchemaPath := pathFor(t, "1.5.59", "v0.16.11", migrations)

	tests := []struct {
		name       string
		emulator   string
		generator  string
		migrations []string
		// wantSamePath means the cached schema is reused; wantSameSchemaPath means a
		// rebuild deletes the base entry as one cached for the same migrations.
		wantSamePath       bool
		wantSameSchemaPath bool
	}{
		{name: "same migrations and versions", emulator: "1.5.59", generator: "v0.16.11", migrations: migrations, wantSamePath: true, wantSameSchemaPath: true},
		{name: "another emulator version", emulator: "1.5.44", generator: "v0.16.11", migrations: migrations, wantSameSchemaPath: true},
		{name: "another generator version", emulator: "1.5.59", generator: "v0.16.12", migrations: migrations, wantSameSchemaPath: true},
		{name: "other migrations", emulator: "1.5.59", generator: "v0.16.11", migrations: []string{"file://other/migrations"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, schemaPath := pathFor(t, tt.emulator, tt.generator, tt.migrations)

			if got := path == basePath; got != tt.wantSamePath {
				t.Errorf("spannerCachePath() equal to the base = %v, want %v", got, tt.wantSamePath)
			}
			if got := schemaPath == baseSchemaPath; got != tt.wantSameSchemaPath {
				t.Errorf("spannerSchemaCachePath() equal to the base = %v, want %v", got, tt.wantSameSchemaPath)
			}
			if !strings.HasPrefix(path, schemaPath+string(filepath.Separator)) {
				t.Errorf("spannerCachePath() = %q, want it under spannerSchemaCachePath() %q", path, schemaPath)
			}
		})
	}
}

// Test_newClient_versionChangeRebuildsCache pins that a generator of another version reads
// the schema again over an unchanged schema, and that the rebuild leaves only its own
// entry for those migrations. Requires the Spanner emulator (podman/docker).
func Test_newClient_versionChangeRebuildsCache(t *testing.T) {
	if testing.Short() {
		t.Skip("reading the schema requires the Spanner emulator")
	}
	// The cache lives at the root of the module found from the working directory, and
	// readBuildInfo is package state: no t.Parallel here.
	dir := newCacheTestModule(t)
	t.Chdir(dir)
	migrations := []string{copyFixtureMigrations(t)}

	entriesAfter := func(t *testing.T, generator string) (path string, entries []string) {
		t.Helper()

		setGeneratorVersion(t, generator)
		if _, err := generateOnce(t.Context(), migrations); err != nil {
			t.Fatalf("generateOnce() with generator %s error = %v", generator, err)
		}
		c := &client{migrationSourceURLs: migrations, spannerEmulatorVersion: cacheTestEmulatorVersion}
		path, err := c.spannerCachePath()
		if err != nil {
			t.Fatalf("spannerCachePath() error = %v", err)
		}
		schemaPath, err := c.spannerSchemaCachePath()
		if err != nil {
			t.Fatalf("spannerSchemaCachePath() error = %v", err)
		}
		dirEntries, err := os.ReadDir(filepath.Join(dir, ".ccc-cache", schemaPath))
		if err != nil {
			t.Fatalf("os.ReadDir() error = %v", err)
		}
		for _, e := range dirEntries {
			entries = append(entries, filepath.Join(schemaPath, e.Name()))
		}

		return path, entries
	}

	oldPath, entries := entriesAfter(t, "v0.16.11")
	if diff := cmp.Diff([]string{oldPath}, entries); diff != "" {
		t.Fatalf("cache entries after the first generator (-want +got):\n%s", diff)
	}

	newPath, entries := entriesAfter(t, "v0.16.12")
	if newPath == oldPath {
		t.Fatalf("spannerCachePath() = %q for both generator versions", newPath)
	}
	if diff := cmp.Diff([]string{newPath}, entries); diff != "" {
		t.Errorf("cache entries after the newer generator (-want +got):\n%s", diff)
	}
}

func Test_generatorVersion(t *testing.T) {
	// readBuildInfo is package state: no t.Parallel here.
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{
			name: "a required version",
			info: &debug.BuildInfo{Deps: []*debug.Module{{Path: generatorModulePath, Version: "v0.16.11", Sum: "h1:abc="}}},
			ok:   true,
			want: "v0.16.11h1:abc=",
		},
		{
			name: "a module replace",
			info: &debug.BuildInfo{Deps: []*debug.Module{{
				Path: generatorModulePath, Version: "v0.16.11",
				Replace: &debug.Module{Path: "github.com/fork/ccc/resource", Version: "v0.16.12", Sum: "h1:def="},
			}}},
			ok:   true,
			want: "v0.16.12h1:def=",
		},
		{
			name: "a directory replace",
			info: &debug.BuildInfo{Deps: []*debug.Module{{
				Path: generatorModulePath, Version: "v0.16.11",
				Replace: &debug.Module{Path: "../ccc/resource"},
			}}},
			ok:   true,
			want: develVersion,
		},
		{
			name: "the module is the main module",
			info: &debug.BuildInfo{Main: debug.Module{Path: generatorModulePath, Version: "(devel)"}},
			ok:   true,
			want: develVersion,
		},
		{
			name: "no build information",
			want: develVersion,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := readBuildInfo
			t.Cleanup(func() { readBuildInfo = original })
			readBuildInfo = func() (*debug.BuildInfo, bool) { return tt.info, tt.ok }

			if got := generatorVersion(); got != tt.want {
				t.Errorf("generatorVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

// setGeneratorVersion makes generatorVersion() answer version until the test ends.
func setGeneratorVersion(t *testing.T, version string) {
	t.Helper()

	original := readBuildInfo
	t.Cleanup(func() { readBuildInfo = original })
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Deps: []*debug.Module{{Path: generatorModulePath, Version: version}}}, true
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

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	fixture := filepath.Join(filepath.Dir(thisFile), "testdata", "migrations")
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(fixture)); err != nil {
		t.Fatalf("os.CopyFS() error = %v", err)
	}

	return "file://" + dir
}
