package transition

import (
	"go/format"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/cloud"
	"github.com/cccteam/ccc/impulse/app"
	cloudrundeclaration "github.com/cccteam/ccc/resource/jobs/cloudrun/declaration"
)

// TestJobsTemplateVariable holds the variable the development template documents to the
// job driver's declaration, by its Template field's name and not its position, and to
// the name the fixtures spell: a field renamed or moved on the driver, or a retag of it,
// fails here rather than in a template that documents a variable the driver never reads.
func TestJobsTemplateVariable(t *testing.T) {
	t.Parallel()

	if want := templateVariableOf(cloudrundeclaration.Settings()); jobsTemplateVariable != want {
		t.Errorf("jobsTemplateVariable = %q, want the declaration's %q", jobsTemplateVariable, want)
	}
	tests := []struct {
		name string
		d    cloud.Declaration
		want string
	}{
		{name: "the job driver's declaration", d: cloudrundeclaration.Settings(), want: "APP_JOBS_TEMPLATE"},
		{name: "a tag with options keeps the variable alone", d: cloud.Declaration{Fields: []cloud.Field{{Name: jobsTemplateField, Tag: "APP_OTHER_TEMPLATE, required"}}}, want: "APP_OTHER_TEMPLATE"},
		{name: "the field found by name, not by position", d: cloud.Declaration{Fields: []cloud.Field{{Name: "Region", Tag: "APP_REGION"}, {Name: jobsTemplateField, Tag: "APP_JOBS_TEMPLATE"}}}, want: "APP_JOBS_TEMPLATE"},
		{name: "a declaration without the field", d: cloud.Declaration{Fields: []cloud.Field{{Name: "Region", Tag: "APP_REGION"}}}},
		{name: "a declaration with no field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := templateVariableOf(tt.d); got != tt.want {
				t.Errorf("templateVariableOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

// skeletonModule is the module path the base skeleton's files carry; the fixtures
// rewrite it to beacon's.
const skeletonModule = "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo"

// skeletonFiles stages the base skeleton's files the files transition edits, under
// beacon's module path: the data and site levels, the app, the bootstrap, the two test
// configurers, and the environment files.
func skeletonFiles(t *testing.T) map[string]string {
	t.Helper()

	files := map[string]string{}
	for _, rel := range []string{
		"pkg/config/data.go",
		"pkg/config/site.go",
		"app/app.go",
		"cmd/bootstrap/main.go",
		"test/authz/harness_test.go",
		"test/integration/harness_test.go",
		".envrc.template",
		".gitignore",
	} {
		files[rel] = strings.ReplaceAll(skeletonFile(t, rel), skeletonModule, "example.com/acme/beacon")
	}

	return files
}

// wiredConfig is a data level that already reads the store's variable.
const wiredConfig = `package config

type dataConfig struct {
	FileStore string ` + "`env:\"APP_FILE_STORE\"`" + `
}
`

// sitesProgram is a generator program of the sites layout.
const sitesProgram = `package main

import (
	"context"
	"log"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"apps/console/pkg/resources",
		[]string{"file://schema/migrations"},
		generation.GenerateHandlers("apps/console/app"),
		generation.GenerateRoutes("apps/console/pkg/router", "api"),
	)
	if err != nil {
		return err
	}
	defer generator.Close()

	return generator.Generate()
}
`

// rpcProgram is beacon's program with an rpc package declared.
var rpcProgram = strings.Replace(beaconProgram, "\t\tgeneration.GenerateHandlers(\"app\"),\n", "\t\tgeneration.GenerateHandlers(\"app\"),\n\t\tgeneration.WithRPC(\"pkg/rpc\"),\n", 1)

// existingRPCClient is an rpc package's Client without the job process's starter.
const existingRPCClient = `package rpc

// Client carries application dependencies into RPC method implementations.
type Client struct{}

// NewClient constructs a Client.
func NewClient() *Client {
	return &Client{}
}
`

func TestFilesValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		extra   map[string]string
		wantErr string
	}{
		{name: "a flat application without the store"},
		{name: "the store already wired", extra: map[string]string{"pkg/config/data.go": wiredConfig}, wantErr: "pkg/config/data.go:4: the file store is already wired; APP_FILE_STORE is declared there"},
		{name: "the sites layout", extra: map[string]string{"cmd/generate/resourcegenerator/main.go": sitesProgram}, wantErr: "sites layout is not supported yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := beacon(t, tt.extra)
			err := Files{}.Validate(a)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestFilesApply(t *testing.T) {
	t.Parallel()

	a := beacon(t, skeletonFiles(t))
	exec := &fakeExec{}
	change, err := Files{}.Apply(t.Context(), a, exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		"pkg/config/files.go: FileStoreSettings (APP_FILE_STORE), LoadFileStoreSettings for the bootstrap, openFileStore and fileStoreOptions; pkg/config/data.go gained the files field, FileStores on dataConfig, the opening, the resource client built over the store, and the release in Close",
		".envrc.template: APP_FILE_STORE=file://uploads, the development store, and APP_JOBS_TEMPLATE documented in the site block, unset",
		".gitignore: uploads/, the development store's directory",
		"cmd/bootstrap/files.go: emptyFileStore, which empties a file:// store; cmd/bootstrap/main.go calls it before the development seed",
		"pkg/jobs/cleanup.go: CleanupCommand (cleanup-files) and CleanupFiles, the orphaned-file cleanup over the store through the generated FileHolders(); cmd/jobs/main.go: the job process running it, with -window and -dry-run",
		`pkg/rpc: the rpc package, its Client carrying the job process's starter (Jobs()), and CleanUpFiles (@rpc, @schedule("0 9 * * *")), which starts cmd/jobs cleanup-files through it`,
		"pkg/config/site.go: SiteConfiguration gained the fields, built from the environment; pkg/config/scheduled.go: Scheduler() and Jobs(), which the app's Configurer asks for, and Close(), which releases the driver with the level",
		"app/app.go: App gained Scheduler() and Jobs() on Configurer, the scheduler field, its construction, and the RPC client built over the starter; app/scheduled.go: SchedulerAuth, the middleware the generated router mounts the scheduled routes behind, and RPCClient(), the dependencies of the RPC methods",
		"the test configurers gained a nil guard, a fake starter and a memory file store: test/authz/harness_test.go (testConfigurer: Scheduler(), Jobs(), and the files field); test/integration/harness_test.go (servedConfigurer: Scheduler(), Jobs(), and the files field)",
		`cmd/generate/resourcegenerator/main.go: WithRPC("pkg/rpc"), so the generator reads the rpc package`,
		"ran go generate ./..., which emitted the CleanUpFiles handler, its scheduled route under /_scheduled, and the router's SchedulerAuth requirement",
	}
	if diff := cmp.Diff(wantDid, change.Did); diff != "" {
		t.Errorf("Did mismatch (-want +got):\n%s", diff)
	}
	if len(change.Skipped) != 0 {
		t.Errorf("Skipped = %q, want none: the base skeleton takes every edit", change.Skipped)
	}
	if diff := cmp.Diff([]string{"go generate ./..."}, exec.calls); diff != "" {
		t.Errorf("exec calls mismatch (-want +got):\n%s", diff)
	}

	// The edited and written files carry the wiring, and every Go file is formatted.
	wantContains := map[string][]string{
		"pkg/config/data.go": {
			`"github.com/cccteam/ccc/resource/filestore"`,
			"files filestore.Store",
			"FileStores FileStoreSettings",
			"files, err := openFileStore(ctx, env.FileStores)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\n\t// The database driver",
			"database.Open(ctx, env.Database.Settings, fileStoreOptions(files)...)",
			"files:             files,",
			"if c.files != nil {\n\t\tif err := c.files.Close(); err != nil {\n\t\t\tlog.Print(errors.Wrap(err, \"filestore.Store.Close()\"))",
		},
		"pkg/config/files.go":     {"package config", "Default string `env:\"APP_FILE_STORE\"`", "func LoadFileStoreSettings(", "func openFileStore(", "func fileStoreOptions("},
		"pkg/config/site.go":      {"scheduler *scheduled.Guard", "jobs *jobstarter.Driver", "scheduler, err := scheduled.FromEnvironment(ctx)", "starter, err := jobstarter.Open(ctx, env.Settings, data.AppVersion())", "scheduler:         scheduler,", "jobs:              starter,", "\tjobstarter.Settings\n}", `jobstarter "github.com/cccteam/ccc/resource/jobs/cloudrun"`},
		"pkg/config/scheduled.go": {"func (c *SiteConfiguration) Scheduler() *scheduled.Guard", "func (c *SiteConfiguration) Jobs() jobs.Starter", "func (c *SiteConfiguration) Close() {\n\tc.jobs.Close()\n\tc.DataConfiguration.Close()\n}"},
		"app/app.go":              {"Scheduler() *scheduled.Guard", "Jobs() jobs.Starter", "scheduler   *scheduled.Guard", "rpcClient   *rpc.Client", "scheduler:      cfg.Scheduler(),", "rpcClient:      rpc.NewClient(cfg.Jobs()),", `"example.com/acme/beacon/pkg/rpc"`},
		"app/scheduled.go":        {"func (a *App) SchedulerAuth(next http.Handler) http.Handler", "return a.scheduler.Middleware(next)", "func (a *App) RPCClient() *rpc.Client", "(pkg/rpc)"},
		"cmd/bootstrap/main.go":   {"\t// The file store is emptied before the seed (files.go): no row holds a file yet.\n\tif err := emptyFileStore(ctx); err != nil {\n\t\treturn err\n\t}\n\n\t// The development seed:"},
		"cmd/bootstrap/files.go":  {"config.LoadFileStoreSettings(ctx)", "filestore.SchemeDir", "store.Delete(ctx, keys)"},
		"pkg/jobs/cleanup.go":     {"package jobs", `const CleanupCommand = "cleanup-files"`, `"example.com/acme/beacon/pkg/resources"`, "Holders: resources.FileHolders(),", "Store:   resource.DefaultStore,"},
		"cmd/jobs/main.go":        {"package main", `"example.com/acme/beacon/pkg/config"`, `"example.com/acme/beacon/pkg/jobs"`, "config.NewDataConfiguration(ctx)", "jobs.CleanupFiles(ctx, data.ResourceClient(), jobs.CleanupOptions{Window: *window, DryRun: *dryRun})"},
		"pkg/rpc/rpc.go":          {"package rpc", "func NewClient(starter jobs.Starter) *Client", "func (c *Client) Jobs() jobs.Starter"},
		"pkg/rpc/clean_up_files.go": {
			"// @rpc\n\t// @schedule(\"0 9 * * *\")\n\tCleanUpFiles struct{}",
			"func (m *CleanUpFiles) Execute(ctx context.Context, _ resource.ReadWriteTransaction, c *Client) (*CleanupStarted, error)",
			"c.Jobs().Start(ctx, appjobs.CleanupCommand)",
			`appjobs "example.com/acme/beacon/pkg/jobs"`,
		},
		"test/authz/harness_test.go":             {"func (c *testConfigurer) Scheduler() *scheduled.Guard {\n\treturn nil\n}", "func (c *testConfigurer) Jobs() jobs.Starter {\n\treturn jobs.NewFake()\n}", `"github.com/cccteam/ccc/resource/jobs"`, `"github.com/cccteam/ccc/resource/scheduled"`},
		"test/integration/harness_test.go":       {"func (c *servedConfigurer) Scheduler() *scheduled.Guard", "func (c *servedConfigurer) Jobs() jobs.Starter", "files *filestore.Mem", "files: filestore.NewMem()", "resource.NewSpannerClient(c.db.Client, resource.WithFileStore(c.files))", `"github.com/cccteam/ccc/resource/filestore"`},
		"cmd/generate/resourcegenerator/main.go": {"\t\tgeneration.GenerateHandlers(\"app\"),\n\t\tgeneration.WithRPC(\"pkg/rpc\"),\n"},
		".envrc.template":                        {"# cmd/jobs cleanup-files removes what no row holds.\nexport APP_FILE_STORE=file://uploads\n\n# --- site: the served site ---\n# APP_JOBS_TEMPLATE is the job process's template job", "# export APP_JOBS_TEMPLATE=\nexport PORT="},
		".gitignore":                             {"go.work.sum\nuploads/\n"},
	}
	for rel, wants := range wantContains {
		text := read(t, a, rel)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q:\n%s", rel, want, text)
			}
		}
		if strings.HasSuffix(rel, ".go") {
			formatted, err := format.Source([]byte(text))
			if err != nil {
				t.Errorf("%s does not parse: %v", rel, err)
			} else if string(formatted) != text {
				t.Errorf("%s is not formatted (-formatted +got):\n%s", rel, cmp.Diff(string(formatted), text))
			}
		}
	}
	// The variable lands in the data block, before the site block, once.
	if env := read(t, a, ".envrc.template"); strings.Count(env, "export APP_FILE_STORE=") != 1 {
		t.Errorf(".envrc.template assigns APP_FILE_STORE %d times, want once", strings.Count(env, "export APP_FILE_STORE="))
	}
	// The profile now reports the store, so a second run is refused.
	if err := (Files{}).Validate(rediscovered(t, a)); err == nil || !strings.Contains(err.Error(), "already wired") {
		t.Errorf("Validate() after Apply error = %v, want the store already wired", err)
	}
}

// rediscovered reads the application again after a transition changed its tree.
func rediscovered(t *testing.T, a *app.App) *app.App {
	t.Helper()

	again, err := app.Discover(a.Root)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return again
}

func TestFilesApplyExistingRPC(t *testing.T) {
	t.Parallel()

	files := skeletonFiles(t)
	files["cmd/generate/resourcegenerator/main.go"] = rpcProgram
	files["pkg/rpc/rpc.go"] = existingRPCClient
	files["cmd/jobs/main.go"] = "package main\n\nfunc main() {}\n"
	a := beacon(t, files)
	change, err := Files{}.Apply(t.Context(), a, &fakeExec{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		`pkg/rpc/clean_up_files.go: CleanUpFiles (@rpc, @schedule("0 9 * * *")), which starts cmd/jobs cleanup-files through the Client's Jobs()`,
		"app/app.go: App gained Scheduler() and Jobs() on Configurer, the scheduler field, and its construction; app/scheduled.go: SchedulerAuth, the middleware the generated router mounts the scheduled routes behind",
	}
	for _, want := range wantDid {
		if !slicesContains(change.Did, want) {
			t.Errorf("Did lacks %q:\n%s", want, strings.Join(change.Did, "\n"))
		}
	}
	wantSkipped := []string{
		"cmd/jobs exists, so the cleanup-files command was not written; add it there: the data level, then filestore.Cleanup over resource.DefaultStore with the generated FileHolders(), -window and -dry-run",
		"pkg/rpc: give Client a Jobs() jobs.Starter accessor fed from the configuration's Jobs() (the job driver, resource/jobs/cloudrun), which CleanUpFiles starts the job through",
		"app/app.go: the app's RPCClient() keeps the client it builds; hand it the configuration's Jobs() so CleanUpFiles can start the job",
	}
	if diff := cmp.Diff(wantSkipped, change.Skipped); diff != "" {
		t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
	}
	for _, unwanted := range []string{"WithRPC", "pkg/rpc: the rpc package"} {
		for _, d := range change.Did {
			if strings.Contains(d, unwanted) {
				t.Errorf("Did %q names %q; the program and the Client were the application's already", d, unwanted)
			}
		}
	}
	if text := read(t, a, "pkg/rpc/rpc.go"); text != existingRPCClient {
		t.Errorf("pkg/rpc/rpc.go changed:\n%s", text)
	}
	if text := read(t, a, "app/scheduled.go"); strings.Contains(text, "RPCClient") {
		t.Errorf("app/scheduled.go declares RPCClient over a Client the application already builds:\n%s", text)
	}
	if text := read(t, a, "cmd/generate/resourcegenerator/main.go"); text != rpcProgram {
		t.Errorf("the program changed:\n%s", text)
	}
}

// scheduledFiles stages the base skeleton with the scheduled routes' guard wired by hand,
// as an application with a scheduled method of its own has it: the site level's field,
// its construction and its accessor, the Configurer's accessor, the App's field, its
// construction and SchedulerAuth, and the test configurers' accessors.
func scheduledFiles(t *testing.T) map[string]string {
	t.Helper()

	files := skeletonFiles(t)
	site := files["pkg/config/site.go"]
	site = replaceOnce(t, site, "\tvalidator *validator.Validate\n}", "\tvalidator *validator.Validate\n\tscheduler *scheduled.Guard\n}")
	site = replaceOnce(t, site, "\treturn &SiteConfiguration{\n", "\tscheduler, err := scheduled.FromEnvironment(ctx)\n\tif err != nil {\n\t\treturn nil, errors.Wrap(err, \"scheduled.FromEnvironment()\")\n\t}\n\n\treturn &SiteConfiguration{\n")
	site = replaceOnce(t, site, "\t\tvalidator:         validator.New(),\n", "\t\tvalidator:         validator.New(),\n\t\tscheduler:         scheduler,\n")
	site += "\n// Scheduler is the guard the scheduled routes sit behind.\nfunc (c *SiteConfiguration) Scheduler() *scheduled.Guard {\n\treturn c.scheduler\n}\n"
	files["pkg/config/site.go"] = withImport(t, "pkg/config/site.go", site, scheduledImportPath)

	application := files["app/app.go"]
	application = replaceOnce(t, application, "\tConsoleDist() string\n}", "\tConsoleDist() string\n\tScheduler() *scheduled.Guard\n}")
	application = replaceOnce(t, application, "\tcsp         string\n}", "\tcsp         string\n\tscheduler   *scheduled.Guard\n}")
	application = replaceOnce(t, application, "\t\tcsp:            cspPolicy(cfg.LiveOrigins()),\n", "\t\tcsp:            cspPolicy(cfg.LiveOrigins()),\n\t\tscheduler:      cfg.Scheduler(),\n")
	application += "\n// SchedulerAuth admits Cloud Scheduler's calls to the scheduled routes.\nfunc (a *App) SchedulerAuth(next http.Handler) http.Handler {\n\treturn a.scheduler.Middleware(next)\n}\n"
	files["app/app.go"] = withImport(t, "app/app.go", application, scheduledImportPath)

	for rel, receiver := range map[string]string{"test/authz/harness_test.go": "testConfigurer", "test/integration/harness_test.go": "servedConfigurer"} {
		text := files[rel] + "\nfunc (c *" + receiver + ") Scheduler() *scheduled.Guard { return nil }\n"
		files[rel] = withImport(t, rel, text, scheduledImportPath)
	}

	return files
}

// replaceOnce replaces the anchor the fixture builds on, failing when the skeleton no
// longer has it.
func replaceOnce(t *testing.T, text, anchor, replacement string) string {
	t.Helper()

	if strings.Count(text, anchor) != 1 {
		t.Fatalf("the skeleton has %d of %q, want one", strings.Count(text, anchor), anchor)
	}

	return strings.Replace(text, anchor, replacement, 1)
}

// withImport adds an import to a fixture's source, formatted.
func withImport(t *testing.T, rel, text, importPath string) string {
	t.Helper()

	out, err := app.AddImport(rel, []byte(text), importPath)
	if err != nil {
		t.Fatalf("app.AddImport(%s) error = %v", rel, err)
	}

	return string(out)
}

func TestFilesApplyScheduled(t *testing.T) {
	t.Parallel()

	a := beacon(t, scheduledFiles(t))
	change, err := Files{}.Apply(t.Context(), a, &fakeExec{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		"pkg/config/site.go: SiteConfiguration gained the jobs field, built from the environment (the guard was wired already); pkg/config/scheduled.go: Jobs(), which the app's Configurer asks for, and Close(), which releases the driver with the level",
		"app/app.go: App gained Jobs() on Configurer and the RPC client built over the starter; app/scheduled.go: RPCClient(), the dependencies of the RPC methods",
		"the test configurers gained a nil guard, a fake starter and a memory file store: test/authz/harness_test.go (testConfigurer: Jobs() and the files field); test/integration/harness_test.go (servedConfigurer: Jobs() and the files field)",
	}
	for _, want := range wantDid {
		if !slicesContains(change.Did, want) {
			t.Errorf("Did lacks %q:\n%s", want, strings.Join(change.Did, "\n"))
		}
	}
	if len(change.Skipped) != 0 {
		t.Errorf("Skipped = %q, want none: the wired guard is left as it is", change.Skipped)
	}

	// Nothing the application wired is declared twice, and what it lacked is there once.
	wantCounts := map[string]map[string]int{
		"pkg/config/site.go": {
			"scheduler *scheduled.Guard":                                 1,
			"scheduled.FromEnvironment(ctx)":                             1,
			"scheduler:         scheduler,":                              1,
			"jobstarter.Open(ctx, env.Settings, data.AppVersion())":      1,
			"jobs:              starter,":                                1,
			"func (c *SiteConfiguration) Scheduler()":                    1,
			"\tjobstarter.Settings\n":                                    1,
			`"github.com/cccteam/ccc/resource/scheduled"`:                1,
			`jobstarter "github.com/cccteam/ccc/resource/jobs/cloudrun"`: 1,
			`"github.com/cccteam/ccc/resource/jobs"`:                     0,
		},
		"pkg/config/scheduled.go": {
			"func (c *SiteConfiguration) Jobs() jobs.Starter": 1,
			"func (c *SiteConfiguration) Close() {":           1,
			"Scheduler()":                                     0,
			"scheduled":                                       0,
		},
		"app/app.go": {
			"Scheduler() *scheduled.Guard":              1,
			"Jobs() jobs.Starter":                       1,
			"scheduler   *scheduled.Guard":              1,
			"scheduler:      cfg.Scheduler()":           1,
			"rpcClient:      rpc.NewClient(cfg.Jobs())": 1,
			"func (a *App) SchedulerAuth(":              1,
		},
		"app/scheduled.go": {
			"func (a *App) RPCClient() *rpc.Client": 1,
			"SchedulerAuth":                         0,
			`"net/http"`:                            0,
		},
		"test/authz/harness_test.go":       {"Scheduler() *scheduled.Guard": 1, "func (c *testConfigurer) Jobs() jobs.Starter": 1, "files *filestore.Mem": 1, "files: filestore.NewMem()": 1, "resource.WithFileStore(c.files)": 1},
		"test/integration/harness_test.go": {"Scheduler() *scheduled.Guard": 1, "func (c *servedConfigurer) Jobs() jobs.Starter": 1, "files *filestore.Mem": 1, "files: filestore.NewMem()": 1, "resource.WithFileStore(c.files)": 1},
	}
	for rel, counts := range wantCounts {
		text := read(t, a, rel)
		for want, n := range counts {
			if got := strings.Count(text, want); got != n {
				t.Errorf("%s has %d of %q, want %d:\n%s", rel, got, want, n, text)
			}
		}
		formatted, err := format.Source([]byte(text))
		if err != nil {
			t.Errorf("%s does not parse: %v", rel, err)
		} else if string(formatted) != text {
			t.Errorf("%s is not formatted (-formatted +got):\n%s", rel, cmp.Diff(string(formatted), text))
		}
	}
}

func TestFilesApplyBare(t *testing.T) {
	t.Parallel()

	// An application with the generator program alone: every edit the tree lacks an
	// anchor for is the agent's, and what needs no anchor is written.
	a := beacon(t, nil)
	change, err := Files{}.Apply(t.Context(), a, &fakeExec{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		".gitignore: uploads/, the development store's directory",
		`pkg/rpc: the rpc package, its Client carrying the job process's starter (Jobs()), and CleanUpFiles (@rpc, @schedule("0 9 * * *")), which starts cmd/jobs cleanup-files through it`,
		`cmd/generate/resourcegenerator/main.go: WithRPC("pkg/rpc"), so the generator reads the rpc package`,
		"ran go generate ./..., which emitted the CleanUpFiles handler, its scheduled route under /_scheduled, and the router's SchedulerAuth requirement",
	}
	if diff := cmp.Diff(wantDid, change.Did); diff != "" {
		t.Errorf("Did mismatch (-want +got):\n%s", diff)
	}
	wantSkipped := []string{
		"no file declares a DataConfiguration struct, so the file store is not opened anywhere; open it with filestore.Open from APP_FILE_STORE where the database is opened, build the resource client over it with resource.WithFileStore, and close it with the level",
		"no environment template (.envrc.template, .env.template, .env.example) to add APP_FILE_STORE=file://uploads to; the env-template check names the variable",
		"no main package at cmd/bootstrap, so nothing empties the development store before a seed; where the development database is seeded, empty a file:// store first (filestore.Open, Objects, Delete), since no row holds a file then",
		"the data level is not constructible (no DataConfiguration), so cmd/jobs was not written; write the job process over the configuration the site opens",
		"no file declares a SiteConfiguration struct, so the scheduled routes' guard and the job driver are not built; build them where the served site's configuration is (scheduled.FromEnvironment; the job driver's Settings embedded in the site's environment struct and its Open, the driver bound under the alias jobstarter) and expose them as Scheduler() and Jobs()",
		"no file declares a Configurer interface, so the guard and the RPC client are not exposed on the app; the generated router needs SchedulerAuth(next http.Handler) http.Handler (scheduled.Guard's Middleware) and the generated handlers RPCClient() returning the rpc package's *Client, built over the configuration's Jobs()",
	}
	if diff := cmp.Diff(wantSkipped, change.Skipped); diff != "" {
		t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
	}
	if text := read(t, a, ".gitignore"); text != "uploads/\n" {
		t.Errorf(".gitignore = %q, want the directory alone", text)
	}
}

func TestFilesChangeText(t *testing.T) {
	t.Parallel()

	ch := &Change{Command: Files{}.Command(), Did: []string{"did"}, Skipped: []string{"skipped"}}
	got := ch.Text()
	for _, want := range []string{"`impulse add files` made these changes and staged them:", "- did", "It could not make these; they are yours:", "- skipped"} {
		if !strings.Contains(got, want) {
			t.Errorf("Text() lacks %q:\n%s", want, got)
		}
	}
	meaning := Files{}.Meaning()
	for _, want := range []string{"file://uploads", "cmd/jobs cleanup-files", `@schedule("0 9 * * *")`, "Left to wire, in this order:", "1. Record files.", "4. Tests."} {
		if !strings.Contains(meaning, want) {
			t.Errorf("Meaning() lacks %q:\n%s", want, meaning)
		}
	}
	if got := (Files{}).Command(); got != "impulse add files" {
		t.Errorf("Command() = %q", got)
	}
}

func TestIgnoresDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		gitignore string
		want      bool
	}{
		{name: "the directory with a slash", gitignore: ".envrc\nuploads/\n", want: true},
		{name: "the directory bare", gitignore: "uploads\n", want: true},
		{name: "anchored at the root", gitignore: "/uploads/\n", want: true},
		{name: "anchored and bare", gitignore: "/uploads\n", want: true},
		{name: "another directory", gitignore: "uploads-documents/\n", want: false},
		{name: "empty", gitignore: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ignoresDir(tt.gitignore, "uploads"); got != tt.want {
				t.Errorf("ignoresDir() = %v, want %v", got, tt.want)
			}
		})
	}
}

// slicesContains reports whether the strings hold the one.
func slicesContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}

	return false
}

// envTemplateRE pins the shape of the variable's assignment the transition writes.
var envTemplateRE = regexp.MustCompile(`(?m)^export APP_FILE_STORE=file://uploads$`)

func TestFilesEnvTemplateAppended(t *testing.T) {
	t.Parallel()

	// A template without a site block takes the variable at its end.
	files := skeletonFiles(t)
	files[".envrc.template"] = "export APP_SERVICE_NAME=beacon\n"
	a := beacon(t, files)
	if _, err := (Files{}).Apply(t.Context(), a, &fakeExec{}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	text := read(t, a, ".envrc.template")
	if !strings.HasPrefix(text, "export APP_SERVICE_NAME=beacon\n\n# APP_FILE_STORE is the file store") || !envTemplateRE.MatchString(text) || !strings.Contains(text, "export APP_FILE_STORE=file://uploads\n\n# APP_JOBS_TEMPLATE is the job process's template job") || !strings.HasSuffix(text, "# export APP_JOBS_TEMPLATE=\n") {
		t.Errorf(".envrc.template = %q", text)
	}
}
