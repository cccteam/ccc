package recipe

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// fixtureDir holds the old forms the provider-drivers recipe moves: each candidate's
// config package before the drivers, and the base skeleton's with the file store wired
// by the files transition (files), beside that form after the move (files-moved), which
// is what the files transition writes on the moved skeleton.
const fixtureDir = "testdata/providerdrivers"

// fixtureFiles reads one fixture's files, relative to the application root.
func fixtureFiles(t *testing.T, name string) map[string]string {
	t.Helper()

	root := os.DirFS(filepath.Join(fixtureDir, name))
	files := map[string]string{}
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(root, p)
		if err != nil {
			return errors.Wrap(err, "fs.ReadFile()")
		}
		files[p] = string(data)

		return nil
	})
	if err != nil {
		t.Fatalf("reading the fixture %s: %v", name, err)
	}

	return files
}

// candidateFile reads a file of an embedded candidate.
func candidateFile(t *testing.T, candidate, rel string) string {
	t.Helper()

	sub, err := skeleton.FS(candidate)
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(sub, rel)
	if err != nil {
		t.Fatalf("candidate %s: %v", candidate, err)
	}

	return string(data)
}

// TestProviderDrivers moves every candidate's config package from the hand-wired form
// to the drivers, byte for byte the candidate's committed form, deletes the test that
// moved with the live driver, and is idle on the result; moves the base skeleton with
// the file store wired by the files transition to what the transition writes on the
// moved skeleton, the site level's job driver included; renames the wrappers in the
// commands that refer to them; binds the cloud driver the cloud-driver recipe left under
// its package name under the alias, byte for byte the candidate's core level; reports a
// form it does not know instead of touching it; and leaves an application already on
// the drivers alone.
func TestProviderDrivers(t *testing.T) {
	t.Parallel()

	const (
		dataDid     = dataFile + ": the data level embeds the database and live drivers' settings (DatabaseSettings, LiveSettings) and opens the drivers under the aliases database and liveservice, which hold the Spanner client, the resource client and the live service"
		dataTestDid = dataTestFile + ": deleted; the live service's project resolution it tested is the live driver's, tested there"
		siteDid     = siteFile + ": the site level embeds the job driver's settings and opens the driver, which names the job of this build and starts it"
		closeDid    = scheduledFile + ": Close releases the job driver, then the levels below"
		configDid   = configFile + ": the cloud driver is bound under the alias cloud"
		migrateFile = "cmd/deployment/migrate/main.go"
		renameDid   = migrateFile + ": SpannerSettings, LoadSpannerSettings, Spanner() and FirestoreSettings are DatabaseSettings, LoadDatabaseSettings, Database() and LiveSettings, named for the drivers' kinds"
	)
	// migrateOld is a command referring to the wrappers under the vendors' names, and
	// migrateNew the same under the kinds'.
	const (
		migrateOld = "package main\n\nimport (\n\t\"context\"\n\n\t\"example.com/acme/beacon/pkg/config\"\n)\n\nfunc report(ctx context.Context, settings config.SpannerSettings) error {\n\treturn nil\n}\n\nfunc run(ctx context.Context) error {\n\tsettings, err := config.LoadSpannerSettings(ctx)\n\tif err != nil {\n\t\treturn err\n\t}\n\tdata, err := config.NewDataConfiguration(ctx)\n\tif err != nil {\n\t\treturn err\n\t}\n\t_ = data.Spanner()\n\n\treturn report(ctx, settings)\n}\n"
		migrateNew = "package main\n\nimport (\n\t\"context\"\n\n\t\"example.com/acme/beacon/pkg/config\"\n)\n\nfunc report(ctx context.Context, settings config.DatabaseSettings) error {\n\treturn nil\n}\n\nfunc run(ctx context.Context) error {\n\tsettings, err := config.LoadDatabaseSettings(ctx)\n\tif err != nil {\n\t\treturn err\n\t}\n\tdata, err := config.NewDataConfiguration(ctx)\n\tif err != nil {\n\t\treturn err\n\t}\n\t_ = data.Database()\n\n\treturn report(ctx, settings)\n}\n"
	)
	type fixture struct {
		name  string
		files map[string]string
		// want are the files after the move, by path; a file wanted empty must be gone.
		want map[string]string
		// wantDetect are the markers Detect reports, each the marker's file and line as
		// Detect prints it; wantDid and wantSkip what Apply reports.
		wantDetect []string
		wantDid    []string
		wantSkip   []string
		// diverged marks a fixture left in the old form, so running the recipe again is
		// not idle.
		diverged bool
	}
	var fixtures []fixture
	for _, candidate := range []string{"solo", "tenanted", "outlets", "sites"} {
		files := fixtureFiles(t, candidate)
		data, test := files[dataFile], files[dataTestFile]
		fixtures = append(fixtures, fixture{
			name:  "the " + candidate + " candidate",
			files: files,
			want:  map[string]string{dataFile: candidateFile(t, candidate, dataFile), dataTestFile: ""},
			wantDetect: []string{
				at(dataFile, data, dataMarkers[0].text) + dataMarkers[0].what,
				at(dataFile, data, dataMarkers[1].text) + dataMarkers[1].what,
				at(dataFile, data, dataMarkers[2].text) + dataMarkers[2].what,
				at(dataFile, data, dataMarkers[3].text) + dataMarkers[3].what,
				at(dataTestFile, test, dataTestMarker.text) + dataTestMarker.what,
			},
			wantDid: []string{dataDid, dataTestDid},
		})
	}
	wired := fixtureFiles(t, "files")
	moved := fixtureFiles(t, "files-moved")
	fixtures = append(fixtures, fixture{
		name:  "the base skeleton with the file store wired",
		files: wired,
		want:  map[string]string{dataFile: moved[dataFile], siteFile: moved[siteFile], scheduledFile: moved[scheduledFile]},
		wantDetect: []string{
			at(dataFile, wired[dataFile], dataMarkers[0].text) + dataMarkers[0].what,
			at(dataFile, wired[dataFile], dataMarkers[1].text) + dataMarkers[1].what,
			at(dataFile, wired[dataFile], dataMarkers[2].text) + dataMarkers[2].what,
			at(dataFile, wired[dataFile], dataMarkers[3].text) + dataMarkers[3].what,
			at(siteFile, wired[siteFile], siteMarker.text) + siteMarker.what,
		},
		wantDid: []string{dataDid, siteDid, closeDid},
	})
	withCommand := fixtureFiles(t, "solo")
	withCommand[migrateFile] = migrateOld
	fixtures = append(fixtures, fixture{
		name:  "a command referring to the wrappers follows their renaming",
		files: withCommand,
		want:  map[string]string{dataFile: candidateFile(t, "solo", dataFile), dataTestFile: "", migrateFile: migrateNew},
		wantDetect: []string{
			at(dataFile, withCommand[dataFile], dataMarkers[0].text) + dataMarkers[0].what,
			at(dataFile, withCommand[dataFile], dataMarkers[1].text) + dataMarkers[1].what,
			at(dataFile, withCommand[dataFile], dataMarkers[2].text) + dataMarkers[2].what,
			at(dataFile, withCommand[dataFile], dataMarkers[3].text) + dataMarkers[3].what,
			at(dataTestFile, withCommand[dataTestFile], dataTestMarker.text) + dataTestMarker.what,
		},
		wantDid: []string{dataDid, renameDid, dataTestDid},
	})
	// The core level as the cloud-driver recipe writes it: the candidate's, with the
	// alias taken back off.
	coreNew := candidateFile(t, "solo", configFile)
	coreOld := strings.NewReplacer(cloudImportNew, cloudImportOld, cloudSettingsNew, cloudSettingsField, cloudFieldNew, cloudFieldOld, cloudOpenNew, cloudOpenOld).Replace(coreNew)
	if coreOld == coreNew {
		t.Fatal("the solo candidate's core level is not under the alias")
	}
	fixtures = append(fixtures, fixture{
		name:    "the cloud driver the cloud-driver recipe left under its package name is bound under the alias",
		files:   map[string]string{configFile: coreOld},
		want:    map[string]string{configFile: coreNew},
		wantDid: []string{configDid},
	})
	unknown := fixtureFiles(t, "solo")
	unknown[dataFile] = strings.Replace(unknown[dataFile], "\tspannerClient, err := cloudspanner.NewClient(ctx, env.Spanner.DatabasePath())\n", "\tspannerClient, err := cloudspanner.NewClient(ctx, env.Spanner.DatabasePath()) // kept by hand\n", 1)
	unknown[dataTestFile] += "\nfunc TestMore(t *testing.T) {\n\tt.Parallel()\n}\n"
	fixtures = append(fixtures, fixture{
		name:     "a data level and a test the recipe does not know are reported, not touched",
		files:    unknown,
		want:     map[string]string{dataFile: unknown[dataFile], dataTestFile: unknown[dataTestFile]},
		diverged: true,
		wantDetect: []string{
			at(dataFile, unknown[dataFile], dataMarkers[0].text) + dataMarkers[0].what,
			at(dataFile, unknown[dataFile], dataMarkers[1].text) + dataMarkers[1].what,
			at(dataFile, unknown[dataFile], dataMarkers[2].text) + dataMarkers[2].what,
			at(dataFile, unknown[dataFile], dataMarkers[3].text) + dataMarkers[3].what,
			at(dataTestFile, unknown[dataTestFile], dataTestMarker.text) + dataTestMarker.what,
		},
		wantSkip: []string{
			dataFile + ": the Spanner client's opening is not in the skeleton's form, so the data level was not rewritten; bind the drivers under the aliases database and liveservice, embed database.Settings in DatabaseSettings (SpannerSettings renamed) and liveservice.Settings in LiveSettings (FirestoreSettings renamed), open them with database.Open (the Spanner client and the resource client are its) and liveservice.Open (which resolves the live service's project), close the database driver in Close, drop the Spanner client, the resource client and openLive, and rename LoadSpannerSettings and the Spanner accessor LoadDatabaseSettings and Database, the rest of the application following",
			dataTestFile + ": the file tests more than the live service's project resolution, so it was not deleted; delete TestFirestoreProject, which the live driver's tests cover (liveservice.Open)",
		},
	}, fixture{
		name:  "an application already on the drivers is left alone",
		files: map[string]string{dataFile: candidateFile(t, "solo", dataFile)},
		want:  map[string]string{dataFile: candidateFile(t, "solo", dataFile)},
	})

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := writeApp(t, tt.files)
			r := ProviderDrivers{}
			found, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDetect, found, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Detect() mismatch (-want +got):\n%s", diff)
			}
			ch, err := r.Apply(t.Context(), a, nil)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, ch.Did, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Did mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSkip, ch.Skipped, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
			}
			for rel, want := range tt.want {
				data, err := os.ReadFile(a.Abs(rel))
				switch {
				case want == "" && os.IsNotExist(err):
					continue
				case want == "":
					t.Errorf("%s still exists after Apply, want it deleted", rel)

					continue
				case err != nil:
					t.Fatalf("%s after Apply: %v", rel, err)
				}
				if diff := cmp.Diff(want, string(data)); diff != "" {
					t.Errorf("%s after Apply mismatch (-want +got):\n%s", rel, diff)
				}
			}
			if tt.diverged {
				return
			}
			// Running again finds nothing: the recipe is safe to run twice.
			again, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() after Apply error = %v", err)
			}
			if len(again) > 0 {
				t.Errorf("Detect() after Apply = %v, want none", again)
			}
		})
	}
}
