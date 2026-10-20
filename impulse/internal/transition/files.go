package transition

import (
	"context"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// Files wires the framework's file store into an application (resource/filestore): the
// data level opens the store from APP_FILE_STORE and builds the resource client over it,
// development keeps files in a gitignored directory, the bootstrap empties that directory
// before it seeds, the job process gains the orphaned-file cleanup command, and a
// scheduled method starts the cleanup each day through the job process's starter. The
// generator program gains WithRPC where the application has no rpc package yet, and the
// App the scheduler guard and the RPC client the generated router and handlers ask for.
// Which resources record files (@file, @upload) is the application's.
type Files struct{}

// The wiring's names: the variable, the development store, and the framework packages.
const (
	fileStoreDevURL     = "file://uploads"
	fileStoreDevDir     = "uploads/"
	filestoreImportPath = "github.com/cccteam/ccc/resource/filestore"
	jobsImportPath      = "github.com/cccteam/ccc/resource/jobs"
	cloudrunImportPath  = "github.com/cccteam/ccc/resource/jobs/cloudrun"
	databaseImportPath  = "github.com/cccteam/ccc/resource/database/spanner"
	scheduledImportPath = "github.com/cccteam/ccc/resource/scheduled"
	loggerImportPath    = "github.com/cccteam/logger"
	// cleanupSchedule is when the cleanup runs: 09:00 UTC, outside every environment's
	// busy hours.
	cleanupSchedule = "0 9 * * *"
	// cleanupCommand is the job process's command for the cleanup, as pkg/jobs declares
	// it and the scheduled method starts it.
	cleanupCommand = "cleanup-files"
)

// The directories the wiring writes into: the rpc package a flat application gains, the
// job package and command, and the bootstrap it edits.
const (
	rpcDirDefault = "pkg/rpc"
	jobsPkgDir    = "pkg/jobs"
	jobsCmdDir    = "cmd/jobs"
	bootstrapDir  = "cmd/bootstrap"
)

// The files the wiring writes beside the ones it edits.
const (
	filesConfigFile     = "files.go"
	scheduledConfigFile = "scheduled.go"
	scheduledAppFile    = "scheduled.go"
	bootstrapFilesFile  = "files.go"
	rpcClientFile       = "rpc.go"
	rpcMethodFile       = "clean_up_files.go"
	jobsCleanupFile     = "cleanup.go"
	jobsMainFile        = "main.go"
	gitignoreFile       = ".gitignore"
)

// The names the wiring takes in the application's own code.
const (
	dataConfigType       = "DataConfiguration"
	dataConstructor      = "NewDataConfiguration"
	siteConfigType       = "SiteConfiguration"
	siteConstructor      = "NewSiteConfiguration"
	configurerType       = "Configurer"
	appType              = "App"
	appConstructor       = "New"
	closeMethod          = "Close"
	bootstrapRun         = "run"
	seedCall             = "deploy.SeedDevelopmentData"
	spannerClientCall    = "resource.NewSpannerClient"
	jobDriverAlias       = "jobstarter"
	rpcClientType        = "Client"
	rpcClientAccessor    = "RPCClient"
	resourceClientMethod = "ResourceClient"
	harnessStoreField    = "files"
	dockerfileName       = "Dockerfile"
	jobsProcess          = "jobs"
	siteGuardField       = "scheduler"
	siteStarterField     = "jobs"
	appGuardField        = "scheduler"
	schedulerAccessor    = "Scheduler"
	jobsAccessor         = "Jobs"
	schedulerAuth        = "SchedulerAuth"
	configurerMarker     = "LogExporter"
	envTemplateSiteSep   = "# --- site:"
)

// Command is the impulse command line for the transition.
func (Files) Command() string {
	return "impulse add files"
}

// Validate checks the transition against the application before anything is changed.
func (Files) Validate(a *app.App) error {
	p := a.Profile()
	switch {
	case len(p.Sites) == 0:
		return errors.New("no generator program emits handlers; the application has no site to wire the file store into")
	case p.Layout == app.LayoutSites:
		return errors.New("wiring the file store into an application in the sites layout is not supported yet")
	}
	if tag := p.FileStore; tag != nil {
		return errors.Newf("%s:%d: the file store is already wired; %s is declared there", tag.File, tag.Line, tag.Name)
	}
	if a.GoMod == nil || a.GoMod.Module == nil {
		return errors.New("go.mod has no module directive; the wiring imports the application's own packages by module path")
	}

	return nil
}

// Apply makes the deterministic half of the transition.
func (f Files) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := f.Validate(a); err != nil {
		return nil, err
	}
	ch := &Change{Command: f.Command()}
	site := &a.Profile().Sites[0]
	g := site.Generator
	modulePath := a.GoMod.Module.Mod.Path

	configDir, err := f.editDataConfig(a, ch)
	if err != nil {
		return nil, err
	}
	if err := f.writeEnvTemplate(a, ch); err != nil {
		return nil, err
	}
	if err := f.writeGitignore(a, ch); err != nil {
		return nil, err
	}
	if err := f.editBootstrap(a, modulePath, configDir, ch); err != nil {
		return nil, err
	}
	job := f.cleanup()
	s, err := job.scaffold(a)
	if err != nil {
		return nil, err
	}
	switch err := job.validateScaffold(a); {
	case configDir == "":
		ch.skipf("the data level is not constructible (no %s), so %s was not written; write the job process over the configuration the site opens", dataConfigType, jobsCmdDir)
	case err != nil:
		ch.skipf("the cleanup command was not written (%v); write it as a command of the job process: the data level, then filestore.Cleanup over resource.DefaultStore with the generated FileHolders(), --window and --dry-run", err)
	default:
		if err := s.write(a, ch); err != nil {
			return nil, err
		}
	}
	w := starterWiring{method: cleanupMethod, command: jobsCmdDir + " " + cleanupCommand, withStore: true}
	if err := w.wire(a, s, g, ch); err != nil {
		return nil, err
	}

	if err := generate(ctx, a, exec, ch,
		"go generate ./... failed, so the scheduled route and the method's handler are not generated yet; fix the cause and run it:",
		"ran go generate ./..., which emitted the CleanUpFiles handler, its scheduled route under /_scheduled, and the router's SchedulerAuth requirement"); err != nil {
		return nil, err
	}

	return ch, nil
}

// envVarRE finds the data level's environment struct: the variable envconfig fills.
var envStructRE = regexp.MustCompile(`(?m)^\s*env := &(\w+)\{\}`)

// editDataConfig wires the store into the data level: the file store settings and the
// store's opening in a new file beside the configuration, and in the file declaring
// DataConfiguration the field, the settings on the environment struct, the opening
// before the construction, the resource client built over the store, and the store's
// release in Close. It returns the configuration package's root-relative directory, or
// empty when the application declares no DataConfiguration.
func (f Files) editDataConfig(a *app.App, ch *Change) (string, error) {
	rel, src, mode, err := findDeclaringFile(a, dataConfigType)
	if err != nil {
		return "", err
	}
	if rel == "" {
		ch.skipf("no file declares a %s struct, so the file store is not opened anywhere; open it with filestore.Open from %s where the database is opened, build the resource client over it with resource.WithFileStore, and close it with the level", dataConfigType, app.FileStoreVariable)

		return "", nil
	}
	pkg, err := app.PackageName(rel, src)
	if err != nil {
		return "", err
	}
	edit := &sourceEdit{rel: rel, src: src}
	if err := f.declareStore(edit); err != nil {
		return "", err
	}
	if err := f.openStore(edit); err != nil {
		return "", err
	}
	if err := f.releaseStore(edit); err != nil {
		return "", err
	}
	if err := os.WriteFile(a.Abs(rel), edit.src, mode); err != nil {
		return "", errors.Wrap(err, "os.WriteFile()")
	}
	configDir := path.Dir(rel)
	filesFile := path.Join(configDir, filesConfigFile)
	if err := writeGo(a, filesFile, f.configSource(pkg)); err != nil {
		return "", err
	}
	ch.didf("%s: FileStoreSettings (%s), LoadFileStoreSettings for the bootstrap, openFileStore and fileStoreOptions; %s gained %s", filesFile, app.FileStoreVariable, rel, joinAnd(edit.gained))
	edit.report(ch)

	return configDir, nil
}

// sourceEdit is one Go file under edit: its source as edited so far, what the edits
// gained it, and what they could not do.
type sourceEdit struct {
	rel     string
	src     []byte
	gained  []string
	skipped []string
}

// apply replaces the source with an edit's result.
func (e *sourceEdit) apply(src []byte, err error) error {
	if err != nil {
		return err
	}
	e.src = src

	return nil
}

// report records the skipped edits on the change.
func (e *sourceEdit) report(ch *Change) {
	for _, s := range e.skipped {
		ch.skipf("%s", s)
	}
}

// declareStore gives the data level the store's field and the settings on its
// environment struct.
func (Files) declareStore(e *sourceEdit) error {
	if err := e.apply(app.AddImport(e.rel, e.src, filestoreImportPath)); err != nil {
		return err
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, dataConfigType, "// files is the file store the resource client is built over ("+filesConfigFile+"), nil when\n// "+app.FileStoreVariable+" is unset.\nfiles filestore.Store")); err != nil {
		return err
	}
	e.gained = append(e.gained, "the files field")
	m := envStructRE.FindSubmatch(e.src)
	if m == nil {
		e.skipped = append(e.skipped, fmt.Sprintf("%s: no environment struct (env := &T{}) to add FileStoreSettings to; read %s into one and pass it to openFileStore", e.rel, app.FileStoreVariable))

		return nil
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, string(m[1]), "\n// FileStores are the file stores' URLs ("+filesConfigFile+").\nFileStores FileStoreSettings")); err != nil {
		return err
	}
	e.gained = append(e.gained, "FileStores on "+string(m[1]))

	return nil
}

// openStore opens the store before the database driver opens, hands the driver the
// store's options, so the resource client it builds is built over the store, and sets
// the field.
func (f Files) openStore(e *sourceEdit) error {
	// The driver's Open is named by the alias the file binds the driver under (database
	// in the skeleton), or by its package name in an application that aliases nothing.
	driver, err := app.ImportName(e.rel, e.src, databaseImportPath)
	if err != nil {
		return err
	}
	if driver == "" {
		e.skipped = append(e.skipped, fmt.Sprintf("%s: the file imports no database driver (%s) whose Open the store would be opened before, so the store is not opened; call openFileStore(ctx, env.FileStores) before the database driver opens, pass it fileStoreOptions(files)..., and set the files field", e.rel, databaseImportPath))

		return nil
	}
	databaseOpenCall := driver + ".Open"
	opened, err := app.AddStatementsBeforeCall(e.rel, e.src, "", dataConstructor, databaseOpenCall, f.openingStatements())
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s makes no %s call to open the store before, so the store is not opened; call openFileStore(ctx, env.FileStores) before the database driver opens, pass it fileStoreOptions(files)..., and set the files field", e.rel, dataConstructor, databaseOpenCall))

		return nil
	case err != nil:
		return err
	}
	e.src = opened
	e.gained = append(e.gained, "the opening")
	if err := e.apply(app.ExtendCall(e.rel, e.src, dataConstructor, databaseOpenCall, "fileStoreOptions(files)...")); err != nil {
		return err
	}
	e.gained = append(e.gained, "the resource client built over the store")
	with, err := app.AddLiteralElement(e.rel, e.src, dataConstructor, dataConfigType, "files: files")
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s builds no &%s{...} literal, so the files field is declared but not set; set it where the level is built", e.rel, dataConstructor, dataConfigType))

		return nil
	case err != nil:
		return err
	}
	e.src = with

	return nil
}

// releaseStore releases the store in Close.
func (f Files) releaseStore(e *sourceEdit) error {
	closed, err := f.releaseInClose(e.rel, e.src)
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s releases no database driver the store's release could go before; close the files field in %s when it is not nil", e.rel, closeMethod, closeMethod))
	case err != nil:
		return err
	default:
		e.src = closed
		e.gained = append(e.gained, "the release in "+closeMethod)
	}

	return nil
}

// openingStatements open the store before the database driver opens.
func (Files) openingStatements() string {
	return `// The file store belongs beside the database: the database driver builds the resource
// client over it (resource.WithFileStore), so the generated handlers read and write files
// through the client, and a committed transaction's released objects are deleted from the
// store. It opens from ` + app.FileStoreVariable + ` (` + filesConfigFile + `): a directory in development, a bucket on
// Cloud Run. A process whose variable is unset runs without a store, and the server
// refuses to start when its routes need one.
files, err := openFileStore(ctx, env.FileStores)
if err != nil {
	return nil, err
}`
}

// releaseInClose releases the store in Close, before the Spanner client it was opened
// beside.
func (Files) releaseInClose(rel string, src []byte) ([]byte, error) {
	// The store's release goes before the database driver's, which closes the resource
	// client the store is wired on; an application still closing a Spanner client of its
	// own has the release go before that.
	field, err := app.StructFieldOfType(rel, src, dataConfigType, databaseImportPath, "Driver")
	if err != nil {
		return nil, err
	}
	if field == "" {
		field, err = app.StructFieldOfType(rel, src, dataConfigType, spannerImportPath, "Client")
		if err != nil {
			return nil, err
		}
	}
	if field == "" {
		return nil, errors.Wrapf(app.ErrNoAnchor, "%s: %s holds no database driver (*Driver of %s) and no *spanner.Client", rel, dataConfigType, databaseImportPath)
	}
	receiver, err := receiverName(rel, src, dataConfigType, closeMethod)
	if err != nil {
		return nil, err
	}
	logCall := "log.Print(err)"
	if ok, _ := app.HasImport(rel, src, errorsImportPath); ok {
		logCall = `log.Print(errors.Wrap(err, "filestore.Store.Close()"))`
	}
	statements := fmt.Sprintf("if %[1]s.files != nil {\n\tif err := %[1]s.files.Close(); err != nil {\n\t\t%[2]s\n\t}\n}", receiver, logCall)
	edited, err := app.AddStatementsBeforeCall(rel, src, dataConfigType, closeMethod, receiver+"."+field+"."+closeMethod, statements)
	if err != nil {
		return nil, err
	}

	return app.AddImport(rel, edited, "log")
}

// configSource is the configuration package's files file: the settings, their loading
// for the bootstrap, the store's opening, and the resource client options.
func (Files) configSource(pkg string) string {
	return fmt.Sprintf(`package %[1]s

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// FileStoreSettings names the application's file store: one URL, %[2]s. A directory
// in development (%[3]s, gitignored), a bucket on Cloud Run (gs://<bucket>, which the
// stack sets from this variable). Unset, no store is opened: the migrate and bootstrap
// commands build the data level without one, and the server refuses to start when its
// routes need one.
type FileStoreSettings struct {
	Default string `+"`env:\"%[2]s\"`"+`
}

// LoadFileStoreSettings reads the store's URL from the environment without opening
// anything, for the bootstrap, which empties a directory store before it seeds.
func LoadFileStoreSettings(ctx context.Context) (FileStoreSettings, error) {
	var settings FileStoreSettings
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: &settings, Lookuper: envconfig.OsLookuper()}); err != nil {
		return FileStoreSettings{}, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	return settings, nil
}

// openFileStore opens the store the settings name through filestore.Open, which checks
// it and refuses a bad URL or a missing bucket; nil when no store is named.
func openFileStore(ctx context.Context, settings FileStoreSettings) (filestore.Store, error) {
	var store filestore.Store
	if settings.Default != "" {
		opened, err := filestore.Open(ctx, settings.Default)
		if err != nil {
			return nil, errors.Wrap(err, "filestore.Open(%[2]s)")
		}
		store = opened
	}

	return store, nil
}

// fileStoreOptions wires the opened store on the resource client as the default store
// (resource.WithFileStore); none when no store is open.
func fileStoreOptions(files filestore.Store) []resource.ClientOption {
	if files == nil {
		return nil
	}

	return []resource.ClientOption{resource.WithFileStore(files)}
}
`, pkg, app.FileStoreVariable, fileStoreDevURL)
}

// writeEnvTemplate puts the development store's URL into the environment template, in the
// data level's block when the template has one, at the end otherwise.
func (Files) writeEnvTemplate(a *app.App, ch *Change) error {
	if a.EnvTemplate == "" {
		ch.skipf("no environment template (.envrc.template, .env.template, .env.example) to add %s=%s to; the env-template check names the variable", app.FileStoreVariable, fileStoreDevURL)

		return nil
	}
	data, mode, err := readFile(a, a.EnvTemplate)
	if err != nil {
		return err
	}
	block := "# " + app.FileStoreVariable + " is the file store (" + path.Join("pkg/config", filesConfigFile) + "): where the uploaded files live. A\n" +
		"# directory in development (" + fileStoreDevURL + ", gitignored); on Cloud Run the stack sets it to\n" +
		"# the bucket, gs://<bucket>. " + bootstrapDir + " empties a directory store before it seeds, and\n" +
		"# " + jobsCmdDir + " " + cleanupCommand + " removes what no row holds.\n" +
		"export " + app.FileStoreVariable + "=" + fileStoreDevURL + "\n"
	text := string(data)
	var out string
	if at := strings.Index(text, envTemplateSiteSep); at >= 0 && (at == 0 || text[at-1] == '\n') {
		out = strings.TrimRight(text[:at], "\n") + "\n" + block + "\n" + text[at:]
	} else {
		out = strings.TrimRight(text, "\n") + "\n\n" + block
	}
	out, documented := documentJobsTemplate(out)
	if err := os.WriteFile(a.Abs(a.EnvTemplate), []byte(out), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	did := fmt.Sprintf("%s: %s=%s, the development store", a.EnvTemplate, app.FileStoreVariable, fileStoreDevURL)
	if documented {
		did += ", and " + jobsTemplateVariable + " documented in the site block, unset"
	}
	ch.didf("%s", did)

	return nil
}

// writeGitignore keeps the development store's directory out of the repository.
func (Files) writeGitignore(a *app.App, ch *Change) error {
	data, mode, err := readFile(a, gitignoreFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data, mode = nil, 0o644
	case err != nil:
		return err
	}
	if ignoresDir(string(data), strings.TrimSuffix(fileStoreDevDir, "/")) {
		ch.didf("%s: %s is already ignored", gitignoreFile, fileStoreDevDir)

		return nil
	}
	text := string(data)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := os.WriteFile(a.Abs(gitignoreFile), []byte(text+fileStoreDevDir+"\n"), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: %s, the development store's directory", gitignoreFile, fileStoreDevDir)

	return nil
}

// ignoresDir reports whether a .gitignore's lines ignore the directory, written any of
// the ways git reads as the directory at the root or anywhere.
func ignoresDir(gitignore, dir string) bool {
	for line := range strings.Lines(gitignore) {
		switch strings.TrimSpace(line) {
		case dir, dir + "/", "/" + dir, "/" + dir + "/":
			return true
		}
	}

	return false
}

// editBootstrap empties the development store before the seed: a new file beside the
// bootstrap holds the emptying, and run calls it before the development seed is applied,
// since no row holds a file at that point.
func (f Files) editBootstrap(a *app.App, modulePath, configDir string, ch *Change) error {
	if !slices.Contains(a.MainPackages, bootstrapDir) {
		ch.skipf("no main package at %s, so nothing empties the development store before a seed; where the development database is seeded, empty a file:// store first (filestore.Open, Objects, Delete), since no row holds a file then", bootstrapDir)

		return nil
	}
	if configDir == "" {
		ch.skipf("%s: the store's URL is not loadable (no %s), so the bootstrap does not empty the development store; read %s and empty a file:// store before the seed", bootstrapDir, dataConfigType, app.FileStoreVariable)

		return nil
	}
	rel := path.Join(bootstrapDir, "main.go")
	src, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	configPkg := packageNameOf(a, configDir)
	filesFile := path.Join(bootstrapDir, bootstrapFilesFile)
	if err := writeGo(a, filesFile, f.bootstrapSource(modulePath, configDir, configPkg)); err != nil {
		return err
	}
	statements := "// The file store is emptied before the seed (" + bootstrapFilesFile + "): no row holds a file yet.\nif err := emptyFileStore(ctx); err != nil {\n\treturn err\n}"
	edited, err := app.AddStatementsBeforeCall(rel, src, "", bootstrapRun, seedCall, statements)
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		ch.didf("%s: emptyFileStore, which empties a file:// store", filesFile)
		ch.skipf("%s: %s makes no %s call to empty the store before; call emptyFileStore(ctx) before the development data is seeded", rel, bootstrapRun, seedCall)

		return nil
	case err != nil:
		return err
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: emptyFileStore, which empties a file:// store; %s calls it before the development seed", filesFile, rel)

	return nil
}

// bootstrapSource is the bootstrap's files file.
func (Files) bootstrapSource(modulePath, configDir, configPkg string) string {
	alias := ""
	if configPkg != path.Base(configDir) {
		alias = configPkg + " "
	}

	return fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"strings"

	%[1]s"%[2]s"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/go-playground/errors/v5"
)

// emptyFileStore removes every object from the file store when it is a directory
// (file://), so the seeded database starts with no file that no row holds: the bootstrap
// runs before any row exists, and an object left behind would otherwise sit in the
// development directory until the orphaned-file cleanup's window passed. A bucket store
// is left alone: the bootstrap targets development, and a bucket's objects are the
// cleanup's to judge. The store opens through filestore.Open, as the data level opens it.
func emptyFileStore(ctx context.Context) error {
	settings, err := %[3]s.LoadFileStoreSettings(ctx)
	if err != nil {
		return errors.Wrap(err, "%[3]s.LoadFileStoreSettings()")
	}
	if !strings.HasPrefix(settings.Default, filestore.SchemeDir+"://") {
		return nil
	}
	store, err := filestore.Open(ctx, settings.Default)
	if err != nil {
		return errors.Wrap(err, "filestore.Open()")
	}
	defer store.Close()

	var keys []string
	for obj, err := range store.Objects(ctx) {
		if err != nil {
			return errors.Wrap(err, "filestore.Store.Objects()")
		}
		keys = append(keys, obj.Key)
	}
	if err := store.Delete(ctx, keys); err != nil {
		return errors.Wrap(err, "filestore.Store.Delete()")
	}
	fmt.Printf("Emptied the file store %%s (%%d objects)\n", settings.Default, len(keys))

	return nil
}
`, alias, modulePath+"/"+configDir, configPkg)
}

// The cleanup's names: the method the framework's documentation names, its outcome,
// and the constant the command is declared under.
const (
	cleanupMethod   = "CleanUpFiles"
	cleanupOutcome  = "CleanupStarted"
	cleanupConstant = "CleanupCommand"
)

// cleanupJob is the file store's job: the orphaned-file cleanup, with work of its own, a
// command taking --window and --dry-run, and the method marked @schedule that starts it
// each day.
type cleanupJob struct{}

// cleanup is the cleanup as a job of the scaffold.
func (Files) cleanup() Job {
	return Job{Name: cleanupCommand, Schedule: cleanupSchedule, kind: cleanupJob{}}
}

func (cleanupJob) names(n *jobNames) {
	n.workFile = path.Join(jobsPkgDir, jobsCleanupFile)
	n.commandConst = cleanupConstant
	n.method = cleanupMethod
	n.outcome = cleanupOutcome
	n.methodFile = rpcMethodFile
}

func (cleanupJob) whatWork(*jobNames) string {
	return "the orphaned-file cleanup over the store through the generated FileHolders()"
}

func (cleanupJob) whatLeaf(*jobNames) string {
	return "with --window and --dry-run"
}

// work is the cleanup package: the command, and the cleanup over the store.
func (cleanupJob) work(s *scaffold, first bool) string {
	n := &s.names
	doc := ""
	if first {
		doc = jobsPackageDoc
	}

	return fmt.Sprintf(`%[1]spackage %[2]s

import (
	"context"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/filestore"
	%[3]s
	"github.com/go-playground/errors/v5"
)

// %[4]s is the job process's command for the orphaned-file cleanup: the argument
// the scheduled %[5]s method (%[6]s) starts the job with.
const %[4]s = %[7]q

// CleanupOptions says how the cleanup runs.
type CleanupOptions struct {
	// Window is the age an object must reach before it may be deleted; zero is the
	// cleanup's default of two days, and under a day is refused.
	Window time.Duration
	// DryRun lists what would be deleted and deletes nothing.
	DryRun bool
}

// %[8]s runs the orphaned-file cleanup over the file store through the holders the
// generator wrote (%[9]s.FileHolders): every resource recording stored files' keys. An
// object older than the window that no row holds is deleted. The resource client carries
// the store, so the cleanup refuses when none is wired, and it refuses to run when no row
// holds any key, since the holders would then be the wrong ones.
func %[8]s(ctx context.Context, client resource.Client, opts CleanupOptions) (*filestore.Report, error) {
	cleanup := filestore.Cleanup{
		Client:  client,
		Store:   resource.DefaultStore,
		Holders: %[9]s.FileHolders(),
		Window:  opts.Window,
		DryRun:  opts.DryRun,
	}
	report, err := cleanup.Run(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "filestore.Cleanup.Run()")
	}

	return report, nil
}
`, doc, s.jobsPkg, importLine(s.resourcesPkg, s.modulePath+"/"+s.resourcesDir), n.commandConst, n.method, rpcDirDefault, n.name, n.workFunc, s.resourcesPkg)
}

// leaf is the cleanup's command, with its two flags.
func (cleanupJob) leaf(s *scaffold) string {
	n := &s.names

	return fmt.Sprintf(`// Package %[1]s is the %[2]s command of the job process: the orphaned-file
// cleanup over the file store (%[3]s, %[4]s), run as
//
//	go run ./%[5]s %[2]s [--window 48h] [--dry-run]
//
// It reads every key the rows hold through the generated holders and deletes the
// UUID-named objects older than the window that no row holds; --window raises the age an
// object must reach, and --dry-run lists what would go and deletes nothing. On Cloud Run
// the scheduled %[6]s method (%[7]s) starts it each day.
package %[1]s

import (
	"context"
	"fmt"
	"time"

	%[8]s
	%[9]s
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
)

// Command is the %[2]s command over the data level the root opened.
func Command(%[10]s *%[11]s.%[12]s) *cobra.Command {
	c := &command{%[10]s: %[10]s}
	cmd := &cobra.Command{
		Use:   %[13]s.%[14]s,
		Short: "Delete the stored files no row holds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.Run(cmd.Context())
		},
	}
	cmd.Flags().DurationVar(&c.window, "window", 0, "the age an object must reach before it may be deleted; the cleanup's default of 48h when unset, and under 24h is refused")
	cmd.Flags().BoolVar(&c.dryRun, "dry-run", false, "list what would be deleted and delete nothing")

	return cmd
}

// command is the %[2]s command: the data level it runs over, and its flags.
type command struct {
	%[10]s *%[11]s.%[12]s
	window time.Duration
	dryRun bool
}

// Run runs the cleanup and prints its report: the counts, then the orphans deleted or,
// on a dry run, listed.
func (c *command) Run(ctx context.Context) error {
	report, err := %[13]s.%[4]s(ctx, c.%[10]s.ResourceClient(), %[13]s.CleanupOptions{Window: c.window, DryRun: c.dryRun})
	if err != nil {
		return errors.Wrap(err, "%[13]s.%[4]s()")
	}
	fmt.Println(report)
	for _, key := range report.Orphans {
		fmt.Printf("  %%s\n", key)
	}

	return nil
}
`, n.pkg, n.name, jobsPkgDir, n.workFunc, jobsCmdDir, n.method, rpcDirDefault, s.configImport(), s.jobsImport(), jobsDataVar, s.configPkg, dataConfigType, s.jobsPkg, n.commandConst)
}

// method is the scheduled method that starts the cleanup.
func (cleanupJob) method(s *scaffold, pkg string) string {
	n := &s.names

	return fmt.Sprintf(`package %[1]s

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"

	appjobs "%[2]s"
)

type (
	// %[6]s is the scheduled method that starts the orphaned-file cleanup. Every
	// day at 09:00 UTC Cloud Scheduler calls it, and it starts one execution of the
	// application's job process with the cleanup command (%[3]s %[4]s), through the
	// job driver the configuration built from the template job the stack sets
	// (APP_JOBS_TEMPLATE, resource/jobs/cloudrun) and the version. The service starts its job,
	// the job deployed with this revision, and the cleanup never runs
	// inside a request; where no job is configured (development, a pull-request stack)
	// the start is refused and the call says so.
	//
	// It takes no input and runs as the application; it answers the execution it
	// started, which the call's log carries, and the execution's own outcome is Cloud
	// Run's to show.
	//
	// @rpc
	// @schedule(%[5]q)
	%[6]s struct{}

	// %[7]s is the method's outcome: the execution started.
	%[7]s struct {
		Execution string
	}
)

// Execute starts the job process on the cleanup command.
func (m *%[6]s) Execute(ctx context.Context, _ resource.ReadWriteTransaction, c *Client) (*%[7]s, error) {
	execution, err := c.Jobs().Start(ctx, appjobs.%[8]s)
	if err != nil {
		return nil, errors.Wrap(err, "jobs.Starter.Start()")
	}
	logger.FromCtx(ctx).Infof("files: the orphaned-file cleanup started as %%s", execution)

	return &%[7]s{Execution: execution}, nil
}
`, pkg, s.modulePath+"/"+jobsPkgDir, jobsCmdDir, n.name, s.schedule, n.method, n.outcome, n.commandConst)
}

// wireHarnessStore builds the test configurer's resource client over a memory store:
// the configurer gains a files field, the literal that builds it sets a fresh store, and
// its ResourceClient method passes the store as resource.WithFileStore. A configurer
// that declares no ResourceClient in its file, builds the client elsewhere than
// resource.NewSpannerClient, or passes a store already is left as it is.
func wireHarnessStore(e *sourceEdit, h harness) (bool, error) {
	if !slices.Contains(h.methods, resourceClientMethod) || strings.Contains(string(e.src), "resource.WithFileStore(") {
		return false, nil
	}
	calls, err := app.HasCall(e.rel, e.src, h.typeName, resourceClientMethod, spannerClientCall)
	if err != nil || !calls {
		return false, err
	}
	receiver, err := receiverName(e.rel, e.src, h.typeName, resourceClientMethod)
	if err != nil {
		return false, err
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, h.typeName, "// "+harnessStoreField+" is the memory store the resource client is built over, standing in for\n// the bucket: a file route the application declares is served from it.\n"+harnessStoreField+" *filestore.Mem")); err != nil {
		return false, err
	}
	with, err := app.AddLiteralElementOfType(e.rel, e.src, h.typeName, harnessStoreField+": filestore.NewMem()")
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: no %s literal builds the configurer, so its memory store is declared but not set; set %s to filestore.NewMem() where it is built", e.rel, h.typeName, harnessStoreField))
	case err != nil:
		return false, err
	default:
		e.src = with
	}
	if err := e.apply(app.ExtendMethodCall(e.rel, e.src, h.typeName, resourceClientMethod, spannerClientCall, "resource.WithFileStore("+receiver+"."+harnessStoreField+")")); err != nil {
		return false, err
	}
	if err := e.apply(app.AddImport(e.rel, e.src, filestoreImportPath)); err != nil {
		return false, err
	}

	return true, nil
}

// writeGo writes a new Go file, formatted, so the imports sort whatever the module path
// is.
func writeGo(a *app.App, rel, src string) error {
	out, err := format.Source([]byte(src))
	if err != nil {
		return errors.Wrapf(err, "format.Source(): %s", rel)
	}

	return writeNew(a, rel, string(out))
}

// Meaning explains the file store in this framework and names what is left to do.
func (Files) Meaning() string {
	var b strings.Builder
	fmt.Fprintf(&b, "The framework keeps an application's uploaded files in a file store (`resource/filestore`), opened from one URL: `%s` is a directory in development, `gs://<bucket>` the bucket the stack makes for the application on Cloud Run, and the same code runs against either. The data level builds the resource client over the store (`resource.WithFileStore`), so the generated handlers stream uploads into it (`@upload`), serve a row's file from it (`@file`), and delete an object the moment the transaction that released it commits. Objects are named by UUID and held by rows; an object no row holds is an orphan, and the orphaned-file cleanup (`filestore.Cleanup`, run by `%s %s`) deletes the ones older than two days, refusing to run when no row holds any key. The cleanup is a command of the application's job process, a cobra command tree (`impulse add job` adds another), started once a day by the scheduled method `%s` (`@schedule(%q)`), which Cloud Scheduler calls under the scheduler's token and which starts the job through the job driver (`resource/jobs/cloudrun`, its settings embedded in the site level's environment); the service starts its job, and the cleanup never runs inside a request.\n\n", fileStoreDevURL, jobsCmdDir, cleanupCommand, cleanupMethod, cleanupSchedule)
	b.WriteString("Left to wire, in this order:\n\n")
	items := []string{
		"Record files. A resource that keeps a file declares a key column (`resource.Key[resource.Store]` for the default store) and `@file` on it, or an `@upload` method whose Execute takes `resource.Files`; the generated holders (`FileHolders()`) then list the resource, and the cleanup reads its keys. Until a resource records a file the store is wired and idle.",
		fmt.Sprintf("Development. `%s` in `%s` keeps files under `%s`, gitignored; `%s` empties the directory before it seeds, since no row holds a file then.", app.FileStoreVariable, ".envrc.template", fileStoreDevDir, bootstrapDir),
		"Deployment. The stack reads the variable from the configuration and makes the bucket, sets `APP_FILE_STORE` to it, deploys the job process with the service (`APP_JOBS_TEMPLATE`, the job driver's setting, names the template job to the service, and the site starts the copy of its own build), and schedules the method with Cloud Scheduler under the invoker identity it names in `APP_SCHEDULER_INVOKER`; nothing here is configured by hand.",
		"Tests. The test configurers answer a nil guard and a fake starter (`jobs.NewFake`), so no suite starts the job; a suite that drives the scheduled route builds a guard over `scheduled.NewFake` and reads the starts off the fake.",
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}

	return b.String()
}
