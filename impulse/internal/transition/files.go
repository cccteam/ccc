package transition

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/cloud"
	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	cloudrundeclaration "github.com/cccteam/ccc/resource/jobs/cloudrun/declaration"
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
	if err := f.writeJobs(a, modulePath, configDir, g, ch); err != nil {
		return nil, err
	}
	rpcDir, hasClient, err := f.writeMethod(a, modulePath, g, ch)
	if err != nil {
		return nil, err
	}
	if err := f.editSiteConfig(a, ch); err != nil {
		return nil, err
	}
	if err := f.editApp(a, modulePath, rpcDir, hasClient, ch); err != nil {
		return nil, err
	}
	if err := f.editHarnesses(a, ch); err != nil {
		return nil, err
	}
	if err := f.editProgram(a, g, rpcDir, ch); err != nil {
		return nil, err
	}

	generate(ctx, a, exec, ch,
		"go generate ./... failed, so the scheduled route and the method's handler are not generated yet; fix the cause and run it:",
		"ran go generate ./..., which emitted the CleanUpFiles handler, its scheduled route under /_scheduled, and the router's SchedulerAuth requirement")

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

// jobsTemplateVariable is the job driver's variable (cloudrun.Settings), which the site
// level declares by embedding the settings, so the development template documents it.
// The name is read off the driver's declaration, by its template field, so the template
// documents the variable the driver reads however the driver spells it.
var jobsTemplateVariable = templateVariableOf(cloudrundeclaration.Settings())

// jobsTemplateField is the field of the job driver's settings struct that carries the
// template job, by the name the declaration gives it.
const jobsTemplateField = "Template"

// templateVariableOf reads the template variable off the job driver's declaration: the
// variable the tag of its template field names, without the tag's options. Empty when
// the declaration has no field of that name, which the tests hold it against.
func templateVariableOf(d cloud.Declaration) string {
	for _, f := range d.Fields {
		if f.Name == jobsTemplateField {
			variable, _, _ := strings.Cut(f.Tag, ",")

			return strings.TrimSpace(variable)
		}
	}

	return ""
}

// jobsTemplateBlock documents the job driver's variable in the development template,
// unset: no job is configured in development.
var jobsTemplateBlock = "# " + jobsTemplateVariable + " is the job process's template job as the Cloud Run API names it, read by\n" +
	"# the job driver (resource/jobs/cloudrun), whose settings the site level embeds; on Cloud Run\n" +
	"# the stack sets it, and the site starts the copy of its own build. Unset, as in development,\n" +
	"# no job is configured, and a scheduled method that starts one says so.\n" +
	"# export " + jobsTemplateVariable + "=\n"

// documentJobsTemplate adds the job driver's variable to the template's site block,
// after the block's heading, or at the end when the template has no site block; a
// template that names the variable already is left as it is. It reports whether the
// block was added.
func documentJobsTemplate(text string) (string, bool) {
	if strings.Contains(text, jobsTemplateVariable) {
		return text, false
	}
	at := strings.Index(text, envTemplateSiteSep)
	if at < 0 || (at > 0 && text[at-1] != '\n') {
		return strings.TrimRight(text, "\n") + "\n\n" + jobsTemplateBlock, true
	}
	end := strings.Index(text[at:], "\n")
	if end < 0 {
		return text + "\n" + jobsTemplateBlock, true
	}
	end += at + 1

	return text[:end] + jobsTemplateBlock + text[end:], true
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

// writeJobs gives the application its job process: the cleanup in a package the scheduled
// method names the command from, and the command around it.
func (f Files) writeJobs(a *app.App, modulePath, configDir string, g *app.Generator, ch *Change) error {
	if slices.Contains(a.MainPackages, jobsCmdDir) {
		ch.skipf("%s exists, so the %s command was not written; add it there: the data level, then filestore.Cleanup over resource.DefaultStore with the generated FileHolders(), -window and -dry-run", jobsCmdDir, cleanupCommand)

		return nil
	}
	if exists(a, jobsPkgDir) {
		ch.skipf("%s exists, so the cleanup package was not written; declare CleanupCommand = %q and CleanupFiles there, which the scheduled method and %s name", jobsPkgDir, cleanupCommand, jobsCmdDir)

		return nil
	}
	if configDir == "" {
		ch.skipf("the data level is not constructible (no %s), so %s was not written; write the job process over the configuration the site opens", dataConfigType, jobsCmdDir)

		return nil
	}
	resourcesPkg := packageNameOf(a, g.ResourcePackageDir)
	cleanupFile := path.Join(jobsPkgDir, jobsCleanupFile)
	if err := writeGo(a, cleanupFile, f.jobsSource(modulePath, g.ResourcePackageDir, resourcesPkg)); err != nil {
		return err
	}
	mainFile := path.Join(jobsCmdDir, jobsMainFile)
	configPkg := packageNameOf(a, configDir)
	if err := writeGo(a, mainFile, f.jobsMainSource(modulePath, configDir, configPkg)); err != nil {
		return err
	}
	ch.didf("%s: CleanupCommand (%s) and CleanupFiles, the orphaned-file cleanup over the store through the generated FileHolders(); %s: the job process running it, with -window and -dry-run", cleanupFile, cleanupCommand, mainFile)
	if dockerfile, err := os.ReadFile(a.Abs(dockerfileName)); err == nil && !strings.Contains(string(dockerfile), "/build/"+jobsProcess) {
		ch.skipf("%s: build %s as /%s beside the other binaries (go build -o /build/%s ./%s), which bedrock check asks for once the job process exists", dockerfileName, jobsCmdDir, jobsProcess, jobsProcess, jobsCmdDir)
	}

	return nil
}

// jobsSource is the cleanup package.
func (Files) jobsSource(modulePath, resourcesDir, resourcesPkg string) string {
	alias := ""
	if resourcesPkg != path.Base(resourcesDir) {
		alias = resourcesPkg + " "
	}

	return fmt.Sprintf(`// Package jobs holds the work the application's job process runs apart from the served
// site: the orphaned-file cleanup over the file store. %[4]s is the command around it.
package jobs

import (
	"context"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/filestore"
	%[1]s"%[2]s"
	"github.com/go-playground/errors/v5"
)

// CleanupCommand is the job process's command for the orphaned-file cleanup: the
// argument the scheduled CleanUpFiles method (%[5]s) starts the job with.
const CleanupCommand = %[6]q

// CleanupOptions says how the cleanup runs.
type CleanupOptions struct {
	// Window is the age an object must reach before it may be deleted; zero is the
	// cleanup's default of two days, and under a day is refused.
	Window time.Duration
	// DryRun lists what would be deleted and deletes nothing.
	DryRun bool
}

// CleanupFiles runs the orphaned-file cleanup over the file store through the holders the
// generator wrote (%[3]s.FileHolders): every resource recording stored files' keys. An
// object older than the window that no row holds is deleted. The resource client carries
// the store, so the cleanup refuses when none is wired, and it refuses to run when no row
// holds any key, since the holders would then be the wrong ones.
func CleanupFiles(ctx context.Context, client resource.Client, opts CleanupOptions) (*filestore.Report, error) {
	cleanup := filestore.Cleanup{
		Client:  client,
		Store:   resource.DefaultStore,
		Holders: %[3]s.FileHolders(),
		Window:  opts.Window,
		DryRun:  opts.DryRun,
	}
	report, err := cleanup.Run(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "filestore.Cleanup.Run()")
	}

	return report, nil
}
`, alias, modulePath+"/"+resourcesDir, resourcesPkg, jobsCmdDir, rpcDirDefault, cleanupCommand)
}

// jobsMainSource is the job process.
func (Files) jobsMainSource(modulePath, configDir, configPkg string) string {
	alias := ""
	if configPkg != path.Base(configDir) {
		alias = configPkg + " "
	}

	return fmt.Sprintf(`// Command jobs is the application's job process: the work that runs apart from the
// served site, over the same data level the site opens. Its one command is
// %[4]s, the orphaned-file cleanup over the file store:
//
//	go run ./%[5]s %[4]s [-window 48h] [-dry-run]
//
// It opens the data level, reads every key the rows hold through the generated holders,
// and deletes the UUID-named objects older than the window that no row holds; -window
// raises the age an object must reach, and -dry-run lists what would go and deletes
// nothing. On Cloud Run the same binary runs as the job the scheduled CleanUpFiles method
// (%[6]s) starts each day: Cloud Scheduler calls the service, and the service starts the
// job deployed with it.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	%[1]s"%[2]s"
	"%[7]s"
	"github.com/go-playground/errors/v5"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	if len(args) == 0 || args[0] != jobs.CleanupCommand {
		return errors.Newf("usage: jobs %%s [-window <duration>] [-dry-run]", jobs.CleanupCommand)
	}
	flags := flag.NewFlagSet(jobs.CleanupCommand, flag.ContinueOnError)
	window := flags.Duration("window", 0, "the age an object must reach before it may be deleted; the cleanup's default of 48h when unset, and under 24h is refused")
	dryRun := flags.Bool("dry-run", false, "list what would be deleted and delete nothing")
	if err := flags.Parse(args[1:]); err != nil {
		return errors.Wrap(err, "flag.FlagSet.Parse()")
	}

	data, err := %[3]s.%[8]s(ctx)
	if err != nil {
		return errors.Wrap(err, "%[3]s.%[8]s()")
	}
	defer data.Close()

	report, err := jobs.CleanupFiles(ctx, data.ResourceClient(), jobs.CleanupOptions{Window: *window, DryRun: *dryRun})
	if err != nil {
		return errors.Wrap(err, "jobs.CleanupFiles()")
	}
	fmt.Println(report)
	for _, key := range report.Orphans {
		fmt.Printf("  %%s\n", key)
	}

	return nil
}
`, alias, modulePath+"/"+configDir, configPkg, cleanupCommand, jobsCmdDir, rpcDirDefault, modulePath+"/"+jobsPkgDir, dataConstructor)
}

// writeMethod writes the scheduled method into the application's rpc package, with the
// package and its Client where the application has none: an application with an rpc
// package already keeps its Client, and giving it the job process's starter is the
// agent's. It returns the rpc package's directory and whether the Client was written
// here, so the App's wiring knows what it can construct.
func (f Files) writeMethod(a *app.App, modulePath string, g *app.Generator, ch *Change) (rpcDir string, hasClient bool, err error) {
	rpcDir = g.RPCDir()
	fresh := rpcDir == ""
	if fresh {
		rpcDir = rpcDirDefault
		if exists(a, rpcDir) {
			ch.skipf("%s exists but the generator program declares no WithRPC over it, so the scheduled method was not written; declare WithRPC(%q) and write CleanUpFiles (@rpc, @schedule(%q)) there, starting %s %s through the job process's starter", rpcDir, rpcDir, cleanupSchedule, jobsCmdDir, cleanupCommand)

			return "", false, nil
		}
	}
	pkg := path.Base(rpcDir)
	if !fresh {
		pkg = packageNameOf(a, rpcDir)
	}
	methodFile := path.Join(rpcDir, rpcMethodFile)
	if exists(a, methodFile) {
		ch.skipf("%s exists, so the scheduled method was not written; make it start %s %s through the job process's starter on the schedule %q", methodFile, jobsCmdDir, cleanupCommand, cleanupSchedule)

		return rpcDir, false, nil
	}
	if fresh {
		if err := writeGo(a, path.Join(rpcDir, rpcClientFile), f.rpcClientSource(pkg)); err != nil {
			return "", false, err
		}
	}
	if err := writeGo(a, methodFile, f.methodSource(pkg, modulePath)); err != nil {
		return "", false, err
	}
	switch {
	case fresh:
		ch.didf("%s: the rpc package, its Client carrying the job process's starter (Jobs()), and CleanUpFiles (@rpc, @schedule(%q)), which starts %s %s through it", rpcDir, cleanupSchedule, jobsCmdDir, cleanupCommand)
	default:
		ch.didf("%s: CleanUpFiles (@rpc, @schedule(%q)), which starts %s %s through the Client's Jobs()", methodFile, cleanupSchedule, jobsCmdDir, cleanupCommand)
		ch.skipf("%s: give %s a Jobs() jobs.Starter accessor fed from the configuration's Jobs() (the job driver, resource/jobs/cloudrun), which CleanUpFiles starts the job through", rpcDir, rpcClientType)
	}

	return rpcDir, fresh, nil
}

// rpcClientSource is a fresh rpc package's Client.
func (Files) rpcClientSource(pkg string) string {
	return fmt.Sprintf(`// Package %[1]s defines the application's RPC methods: an @rpc struct per method whose Execute
// signature says how it runs (resource/README.md §7), and the Client that carries the
// application's dependencies into them. The generator reads the package (WithRPC in the
// generator program) and emits each method's handler and route.
package %[1]s

import "%[2]s"

// Client carries application dependencies into RPC method implementations: the starter
// of the job process, which CleanUpFiles starts the cleanup through.
type Client struct {
	jobs jobs.Starter
}

// NewClient constructs a Client over the job process's starter.
func NewClient(starter jobs.Starter) *Client {
	return &Client{jobs: starter}
}

// Jobs is the job process's starter (resource/jobs): the job driver the configuration
// built.
func (c *Client) Jobs() jobs.Starter {
	return c.jobs
}
`, pkg, jobsImportPath)
}

// methodSource is the scheduled method.
func (Files) methodSource(pkg, modulePath string) string {
	return fmt.Sprintf(`package %[1]s

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"

	appjobs "%[2]s"
)

type (
	// CleanUpFiles is the scheduled method that starts the orphaned-file cleanup. Every
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
	CleanUpFiles struct{}

	// CleanupStarted is the method's outcome: the execution started.
	CleanupStarted struct {
		Execution string
	}
)

// Execute starts the job process on the cleanup command.
func (m *CleanUpFiles) Execute(ctx context.Context, _ resource.ReadWriteTransaction, c *Client) (*CleanupStarted, error) {
	execution, err := c.Jobs().Start(ctx, appjobs.CleanupCommand)
	if err != nil {
		return nil, errors.Wrap(err, "jobs.Starter.Start()")
	}
	logger.FromCtx(ctx).Infof("files: the orphaned-file cleanup started as %%s", execution)

	return &CleanupStarted{Execution: execution}, nil
}
`, pkg, modulePath+"/"+jobsPkgDir, jobsCmdDir, cleanupCommand, cleanupSchedule)
}

// editSiteConfig gives the site level the scheduled routes' guard and the job driver,
// the guard read from the environment and the driver opened from the settings the site's
// environment struct embeds, where the level is built, with their accessors in a new file
// beside it. What the level holds already is left as it is: an application whose own
// scheduled method wired the guard gains the driver alone, one that wired a starter of
// its own keeps it, and an accessor the package declares is not written twice.
func (f Files) editSiteConfig(a *app.App, ch *Change) error {
	rel, src, mode, err := findDeclaringFile(a, siteConfigType)
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a %s struct, so the scheduled routes' guard and the job driver are not built; build them where the served site's configuration is (scheduled.FromEnvironment; the job driver's Settings embedded in the site's environment struct and its Open, the driver bound under the alias %s) and expose them as %s() and %s()", siteConfigType, jobDriverAlias, schedulerAccessor, jobsAccessor)

		return nil
	}
	pkg, err := app.PackageName(rel, src)
	if err != nil {
		return err
	}
	guardField, err := app.StructFieldOfType(rel, src, siteConfigType, scheduledImportPath, "Guard")
	if err != nil {
		return err
	}
	starterField, driver, err := f.starterField(rel, src)
	if err != nil {
		return err
	}
	wrapErr := plainErr
	if ok, _ := app.HasImport(rel, src, errorsImportPath); ok {
		wrapErr = `errors.Wrap(err, %q)`
	}
	edit := &sourceEdit{rel: rel, src: src}
	var added, wired []string
	built := true
	if guardField == "" {
		guardField = siteGuardField
		ok, err := f.buildInSite(edit, &siteWiring{
			importPath: scheduledImportPath,
			field:      "// " + siteGuardField + " is the guard the scheduled routes sit behind (" + scheduledConfigFile + ").\n" + siteGuardField + " *scheduled.Guard",
			statements: "// The scheduled routes' guard (" + scheduledConfigFile + "): it reads the invoker identity the stack\n// names in APP_SCHEDULER_INVOKER, and without one logs that the scheduled routes are off.\n" +
				siteGuardField + ", err := scheduled.FromEnvironment(ctx)\nif err != nil {\n\treturn nil, " + wrapFor(wrapErr, "scheduled.FromEnvironment()") + "\n}",
			callee:  "scheduled.FromEnvironment",
			element: siteGuardField + ": " + siteGuardField,
			what:    "the guard",
		})
		if err != nil {
			return err
		}
		built = built && ok
		added = append(added, "the "+siteGuardField+" field")
	} else {
		wired = append(wired, "the guard")
	}
	if starterField == "" {
		starterField, driver = siteStarterField, true
		ok, err := f.buildInSite(edit, &siteWiring{
			importPath: cloudrunImportPath,
			importName: jobDriverAlias,
			field:      "// " + siteStarterField + " is the job driver: the starter of this build's job, or of none (" + scheduledConfigFile + ").\n" + siteStarterField + " *" + jobDriverAlias + ".Driver",
			statements: "// The job driver (" + scheduledConfigFile + "): it names the job of this build from the template job the\n// stack sets in APP_JOBS_TEMPLATE, the setting the site's environment embeds, and the\n// version the image bakes in, and without a template refuses every start.\n" +
				"starter, err := " + jobDriverAlias + ".Open(ctx, env.Settings, data.AppVersion())\nif err != nil {\n\treturn nil, " + wrapFor(wrapErr, jobDriverAlias+".Open()") + "\n}",
			callee:  jobDriverAlias + ".Open",
			element: siteStarterField + ": starter",
			what:    "the driver",
		})
		if err != nil {
			return err
		}
		built = built && ok
		added = append(added, "the "+siteStarterField+" field")
		f.embedDriverSettings(edit)
	} else {
		wired = append(wired, "the starter")
	}
	if len(added) > 0 {
		if err := os.WriteFile(a.Abs(rel), edit.src, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
	}
	accessors, accessorsFile, err := f.writeSiteAccessors(a, rel, pkg, guardField, starterField, driver)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s: %s gained %s", rel, siteConfigType, gainedInSite(added, built, wired))
	if closed := slices.Index(accessors, closeMethod+"()"); closed >= 0 {
		accessors = slices.Delete(accessors, closed, closed+1)
		if len(accessors) > 0 {
			line += fmt.Sprintf("; %s: %s, which the app's %s asks for, and %s(), which releases the driver with the level", accessorsFile, joinAnd(accessors), configurerType, closeMethod)
		} else {
			line += fmt.Sprintf("; %s: %s(), which releases the driver with the level", accessorsFile, closeMethod)
		}
	} else if len(accessors) > 0 {
		line += fmt.Sprintf("; %s: %s, which the app's %s asks for", accessorsFile, joinAnd(accessors), configurerType)
	}
	ch.didf("%s", line)
	edit.report(ch)

	return nil
}

// starterField finds the field the site level holds its job starter in: the job driver
// (*cloudrun.Driver), reported as such, or a starter of the application's own
// (jobs.Starter); empty when the level holds neither.
func (Files) starterField(rel string, src []byte) (field string, driver bool, err error) {
	field, err = app.StructFieldOfType(rel, src, siteConfigType, cloudrunImportPath, "Driver")
	if err != nil || field != "" {
		return field, field != "", err
	}
	field, err = app.StructFieldOfValueType(rel, src, siteConfigType, jobsImportPath, "Starter")

	return field, false, err
}

// embedDriverSettings embeds the job driver's settings in the site's environment struct,
// so the application declares the template job's variable through the driver's
// declaration; a site level without an environment struct leaves the embedding to the
// agent.
func (Files) embedDriverSettings(e *sourceEdit) {
	m := envStructRE.FindSubmatch(e.src)
	if m == nil {
		e.skipped = append(e.skipped, fmt.Sprintf("%s: no environment struct (env := &T{}) to embed %s.Settings in; embed it in the site's environment struct, which declares APP_JOBS_TEMPLATE, and pass it to %s.Open", e.rel, jobDriverAlias, jobDriverAlias))

		return
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, string(m[1]), "\n// The job driver's variables ("+jobDriverAlias+".Settings, whichever driver the import names:\n// the Cloud Run driver's template job this build's job is named from).\n"+jobDriverAlias+".Settings")); err != nil {
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s.Settings was not embedded in %s (%v); embed it there, since it declares APP_JOBS_TEMPLATE", e.rel, jobDriverAlias, m[1], err))

		return
	}
	e.gained = append(e.gained, jobDriverAlias+".Settings on "+string(m[1]))
}

// writeSiteAccessors writes the site level's scheduled file with the accessors the
// package lacks, Scheduler() over the guard field and Jobs() over the starter field, and
// Close when the starter is the job driver and the type declares no Close, so the driver
// is released with the level; it returns the methods written and the file; none written,
// no file is.
func (Files) writeSiteAccessors(a *app.App, rel, pkg, guardField, starterField string, driver bool) (accessors []string, file string, err error) {
	methods, err := methodsOfType(a, path.Dir(rel), siteConfigType)
	if err != nil {
		return nil, "", err
	}
	var imports []string
	var body strings.Builder
	if !slices.Contains(methods, schedulerAccessor) {
		accessors, imports = append(accessors, schedulerAccessor+"()"), append(imports, scheduledImportPath)
		fmt.Fprintf(&body, schedulerAccessorSource, schedulerAccessor, siteConfigType, guardField)
	}
	if !slices.Contains(methods, jobsAccessor) {
		accessors, imports = append(accessors, jobsAccessor+"()"), append(imports, jobsImportPath)
		fmt.Fprintf(&body, jobsAccessorSource, jobsAccessor, siteConfigType, starterField)
	}
	if driver && !slices.Contains(methods, closeMethod) {
		accessors = append(accessors, closeMethod+"()")
		fmt.Fprintf(&body, closeSiteSource, closeMethod, siteConfigType, starterField, dataConfigType)
	}
	file = path.Join(path.Dir(rel), scheduledConfigFile)
	if len(accessors) == 0 {
		return nil, file, nil
	}
	if err := writeGo(a, file, goFile(pkg, imports, body.String())); err != nil {
		return nil, "", err
	}

	return accessors, file, nil
}

// gainedInSite says what the site level took: both fields, one of them beside the one
// the application had wired, or nothing.
func gainedInSite(added []string, built bool, wired []string) string {
	from := ""
	if built {
		from = ", built from the environment"
	}
	switch {
	case len(added) == 0:
		return "nothing: " + joinAnd(wired) + " were wired already"
	case len(wired) == 0:
		return "the fields" + from
	default:
		return added[0] + from + " (" + wired[0] + " was wired already)"
	}
}

// siteWiring is one of the two values the site level gains: its field with the import
// the type needs, with the name the file binds it under (empty for the package's own),
// the statements that build it before the construction, the call those statements make
// (present already, they are not added), and the literal element that sets the field.
type siteWiring struct {
	importPath, importName, field, statements, callee, element, what string
}

// buildInSite adds one wiring to the site level: the field and its import, the
// statements unless the constructor makes the call already, and the element. It reports
// whether the value is built where the level is constructed; a constructor without the
// literal leaves the field declared and the building to the agent.
func (Files) buildInSite(e *sourceEdit, w *siteWiring) (bool, error) {
	if err := e.apply(app.AddStructField(e.rel, e.src, siteConfigType, w.field)); err != nil {
		return false, err
	}
	if err := e.apply(app.AddNamedImport(e.rel, e.src, w.importName, w.importPath)); err != nil {
		return false, err
	}
	calls, err := app.HasCall(e.rel, e.src, "", siteConstructor, w.callee)
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: no %s to build %s in; call %s(ctx) where the level is built and set the field", e.rel, siteConstructor, w.what, w.callee))

		return false, nil
	case err != nil:
		return false, err
	}
	if !calls {
		built, err := app.AddStatementsBeforeConstruction(e.rel, e.src, siteConstructor, siteConfigType, w.statements)
		switch {
		case errors.Is(err, app.ErrNoAnchor):
			e.skipped = append(e.skipped, fmt.Sprintf("%s: %s builds no &%s{...} literal, so %s is declared but not built; call %s(ctx) where the level is built and set the field", e.rel, siteConstructor, siteConfigType, w.what, w.callee))

			return false, nil
		case err != nil:
			return false, err
		}
		e.src = built
	}
	if err := e.apply(app.AddLiteralElement(e.rel, e.src, siteConstructor, siteConfigType, w.element)); err != nil {
		return false, err
	}

	return true, nil
}

// wrapFor renders an error return: the wrap template with its message, or err alone.
func wrapFor(wrapErr, message string) string {
	if wrapErr == plainErr {
		return plainErr
	}

	return fmt.Sprintf(wrapErr, message)
}

// The site level's accessors, one per value: the accessor's name, the type, and the
// field it reads.
const (
	schedulerAccessorSource = `
// %[1]s returns the guard the scheduled routes sit behind: Cloud Scheduler's tokens of
// the invoker identity APP_SCHEDULER_INVOKER names, and nothing else. With the variable
// unset, as in development, every scheduled call is refused.
func (c *%[2]s) %[1]s() *scheduled.Guard {
	return c.%[3]s
}
`
	jobsAccessorSource = `
// %[1]s starts the application's job process: the job driver (resource/jobs/cloudrun),
// which names the job of this build from the template job the stack sets in
// APP_JOBS_TEMPLATE and the version the image bakes in, or refuses every start where no
// template is configured.
func (c *%[2]s) %[1]s() jobs.Starter {
	return c.%[3]s
}
`
	closeSiteSource = `
// %[1]s releases the job driver, then the levels below.
func (c *%[2]s) %[1]s() {
	c.%[3]s.%[1]s()
	c.%[4]s.%[1]s()
}
`
)

// goFile is a Go file: the package clause, the imports, and the body.
func goFile(pkg string, imports []string, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", pkg)
	if len(imports) > 0 {
		b.WriteString("\nimport (\n")
		for _, path := range imports {
			fmt.Fprintf(&b, "\t%q\n", path)
		}
		b.WriteString(")\n")
	}
	b.WriteString(body)

	return b.String()
}

// editApp exposes the guard and the RPC client on the App: the Configurer asks the
// configuration for the guard and the starter, the App carries the guard and the client
// built over the starter, and a new file declares the middleware the generated router
// requires (SchedulerAuth) and the accessor the generated handlers require (RPCClient).
// What the app declares already, as an application with a scheduled method of its own
// does, is left as it is.
func (f Files) editApp(a *app.App, modulePath, rpcDir string, hasClient bool, ch *Change) error {
	rel, src, mode, err := findDeclaringFile(a, configurerType)
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a %s interface, so the guard and the RPC client are not exposed on the app; the generated router needs %s(next http.Handler) http.Handler (scheduled.Guard's Middleware) and the generated handlers %s() returning the rpc package's *%s, built over the configuration's %s()", configurerType, schedulerAuth, rpcClientAccessor, rpcClientType, jobsAccessor)

		return nil
	}
	pkg, err := app.PackageName(rel, src)
	if err != nil {
		return err
	}
	edit := &sourceEdit{rel: rel, src: src}
	guardField, err := f.exposeGuard(edit)
	if err != nil {
		return err
	}
	rpcPath, rpcPkg := modulePath+"/"+rpcDir, path.Base(rpcDir)
	if err := f.exposeRPCClient(edit, rpcPath, rpcPkg, rpcDir, hasClient); err != nil {
		return err
	}
	if len(edit.gained) > 0 {
		if err := os.WriteFile(a.Abs(rel), edit.src, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
	}
	methods, err := methodsOfType(a, path.Dir(rel), appType)
	if err != nil {
		return err
	}
	writeAuth := !slices.Contains(methods, schedulerAuth)
	writeClient := hasClient && !slices.Contains(methods, rpcClientAccessor)
	line := fmt.Sprintf("%s: %s gained %s", rel, appType, joinAnd(edit.gained))
	if len(edit.gained) == 0 {
		line = fmt.Sprintf("%s: %s exposed the guard and the starter already", rel, appType)
	}
	if writeAuth || writeClient {
		scheduledFile := path.Join(path.Dir(rel), scheduledAppFile)
		if err := writeGo(a, scheduledFile, f.appSource(pkg, rpcPath, rpcDir, rpcPkg, guardField, writeAuth, writeClient)); err != nil {
			return err
		}
		var declared string
		switch {
		case writeAuth && writeClient:
			declared = schedulerAuth + ", the middleware the generated router mounts the scheduled routes behind, and " + rpcClientAccessor + "(), the dependencies of the RPC methods"
		case writeAuth:
			declared = schedulerAuth + ", the middleware the generated router mounts the scheduled routes behind"
		default:
			declared = rpcClientAccessor + "(), the dependencies of the RPC methods"
		}
		line += fmt.Sprintf("; %s: %s", scheduledFile, declared)
	}
	ch.didf("%s", line)
	edit.report(ch)

	return nil
}

// exposeGuard asks the Configurer for the guard and the starter, and gives the App the
// guard. It returns the App's guard field: the one the application declared, or the one
// added here.
func (Files) exposeGuard(e *sourceEdit) (string, error) {
	var onConfigurer []string
	for _, m := range []struct{ name, line, importPath string }{
		{schedulerAccessor, "// " + schedulerAccessor + " is the guard the scheduled routes sit behind (resource/scheduled): the\n// invoker identity the stack names in APP_SCHEDULER_INVOKER, whose Google-signed tokens\n// alone are admitted. A guard with no invoker refuses every scheduled call.\n" + schedulerAccessor + "() *scheduled.Guard", scheduledImportPath},
		{jobsAccessor, "// " + jobsAccessor + " starts the application's job process (resource/jobs): an execution of the\n// Cloud Run job deployed with this revision, named from the template job the stack sets in\n// APP_JOBS_TEMPLATE and the version the image bakes in.\n// The scheduled CleanUpFiles method starts the orphaned-file cleanup through it.\n" + jobsAccessor + "() jobs.Starter", jobsImportPath},
	} {
		before := e.src
		if err := e.apply(app.AddInterfaceLine(e.rel, e.src, configurerType, m.line)); err != nil {
			return "", err
		}
		if bytes.Equal(before, e.src) {
			continue
		}
		if err := e.apply(app.AddImport(e.rel, e.src, m.importPath)); err != nil {
			return "", err
		}
		onConfigurer = append(onConfigurer, m.name+"()")
	}
	if len(onConfigurer) > 0 {
		e.gained = append(e.gained, joinAnd(onConfigurer)+" on "+configurerType)
	}
	field, err := app.StructFieldOfType(e.rel, e.src, appType, scheduledImportPath, "Guard")
	if err != nil {
		return "", err
	}
	if field != "" {
		return field, nil
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, appType, appGuardField+" *scheduled.Guard")); err != nil {
		return "", err
	}
	if err := e.apply(app.AddImport(e.rel, e.src, scheduledImportPath)); err != nil {
		return "", err
	}
	e.gained = append(e.gained, "the "+appGuardField+" field")
	with, err := app.AddLiteralElement(e.rel, e.src, appConstructor, appType, appGuardField+": cfg."+schedulerAccessor+"()")
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s builds no %s literal, so the guard is not set; set %s to cfg.%s()", e.rel, appConstructor, appType, appGuardField, schedulerAccessor))
	case err != nil:
		return "", err
	default:
		e.src = with
		e.gained = append(e.gained, "its construction")
	}

	return appGuardField, nil
}

// exposeRPCClient gives the App the RPC client built over the starter when the rpc
// package is this transition's; an application's own client is left as it is, and
// feeding it the starter is the agent's.
func (Files) exposeRPCClient(e *sourceEdit, rpcPath, rpcPkg, rpcDir string, hasClient bool) error {
	if !hasClient {
		if rpcDir != "" {
			e.skipped = append(e.skipped, fmt.Sprintf("%s: the app's %s() keeps the client it builds; hand it the configuration's %s() so CleanUpFiles can start the job", e.rel, rpcClientAccessor, jobsAccessor))
		}

		return nil
	}
	if err := e.apply(app.AddImport(e.rel, e.src, rpcPath)); err != nil {
		return err
	}
	if err := e.apply(app.AddStructField(e.rel, e.src, appType, "rpcClient *"+rpcPkg+"."+rpcClientType)); err != nil {
		return err
	}
	with, err := app.AddLiteralElement(e.rel, e.src, appConstructor, appType, "rpcClient: "+rpcPkg+".NewClient(cfg."+jobsAccessor+"())")
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		e.skipped = append(e.skipped, fmt.Sprintf("%s: %s builds no %s literal, so the RPC client is not built; set rpcClient to %s.NewClient(cfg.%s())", e.rel, appConstructor, appType, rpcPkg, jobsAccessor))
	case err != nil:
		return err
	default:
		e.src = with
		e.gained = append(e.gained, "the RPC client built over the starter")
	}

	return nil
}

// appSource is the app's scheduled file: SchedulerAuth over the App's guard field, and
// RPCClient when the rpc package is this transition's, each when the app lacks it.
func (Files) appSource(pkg, rpcPath, rpcDir, rpcPkg, guardField string, writeAuth, writeClient bool) string {
	var imports []string
	var body strings.Builder
	if writeAuth {
		imports = append(imports, "net/http")
		fmt.Fprintf(&body, `
// %[1]s admits Cloud Scheduler's calls to the scheduled routes (/_scheduled): a
// token Google signed for the route's URL whose verified email is the invoker identity
// named in APP_SCHEDULER_INVOKER. Any other call answers 401, its reason logged, and with
// no invoker configured every call is refused: the routes are off in development.
func (a *%[2]s) %[1]s(next http.Handler) http.Handler {
	return a.%[3]s.Middleware(next)
}
`, schedulerAuth, appType, guardField)
	}
	if writeClient {
		imports = append(imports, rpcPath)
		fmt.Fprintf(&body, `
// %[1]s returns the dependencies for RPC method implementations (%[2]s): the starter of
// the job process, which the scheduled CleanUpFiles method starts the cleanup through.
func (a *%[3]s) %[1]s() *%[4]s.%[5]s {
	return a.rpcClient
}
`, rpcClientAccessor, rpcDir, appType, rpcPkg, rpcClientType)
	}

	return goFile(pkg, imports, body.String())
}

// methodsOfType lists the methods the package at dir declares on the type in its
// non-test files, so an accessor or a middleware the application wrote itself is not
// written twice.
func methodsOfType(a *app.App, dir, typeName string) ([]string, error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	var methods []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		rel := path.Join(dir, name)
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		names, err := app.MethodNames(rel, src, typeName)
		if err != nil {
			return nil, err
		}
		methods = append(methods, names...)
	}

	return methods, nil
}

// editHarnesses gives every test configurer the two accessors the Configurer gained, a
// nil guard, since no suite mounts a scheduled route and a missing guard refuses every
// call, and a fake starter that records what a scheduled call starts, and builds its
// resource client over a memory store, as the data level builds production's over the
// bucket, so a file route the application declares later is served in the suites rather
// than refused by the router. A test configurer is a type in a test file declaring
// LogExporter, the Configurer method every harness implements.
func (f Files) editHarnesses(a *app.App, ch *Change) error {
	harnesses, err := f.harnessFiles(a)
	if err != nil {
		return err
	}
	var edited []string
	for _, h := range harnesses {
		src, mode, err := readFile(a, h.file)
		if err != nil {
			return err
		}
		edit := &sourceEdit{rel: h.file, src: src}
		var added []string
		for _, m := range []struct {
			name, method string
		}{
			{schedulerAccessor, fmt.Sprintf("\n// %[1]s is none: the suites mount no scheduled route, and a missing guard refuses\n// every scheduled call.\nfunc (%[2]s) %[1]s() *scheduled.Guard {\n\treturn nil\n}\n", schedulerAccessor, h.receiver)},
			{jobsAccessor, fmt.Sprintf("\n// %[1]s is a fake starter: no suite starts the job process, and a scheduled call that\n// does records the start.\nfunc (%[2]s) %[1]s() jobs.Starter {\n\treturn jobs.NewFake()\n}\n", jobsAccessor, h.receiver)},
		} {
			if slices.Contains(h.methods, m.name) {
				continue
			}
			edit.src = append(edit.src, m.method...)
			added = append(added, m.name+"()")
		}
		if len(added) > 0 {
			for _, importPath := range []string{scheduledImportPath, jobsImportPath} {
				if err := edit.apply(app.AddImport(edit.rel, edit.src, importPath)); err != nil {
					return err
				}
			}
		}
		stored, err := f.wireHarnessStore(edit, h)
		if err != nil {
			return err
		}
		if stored {
			added = append(added, "the "+harnessStoreField+" field")
		}
		edit.report(ch)
		if len(added) == 0 {
			continue
		}
		if err := os.WriteFile(a.Abs(h.file), edit.src, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		edited = append(edited, fmt.Sprintf("%s (%s: %s)", h.file, h.typeName, joinAnd(added)))
	}
	if len(edited) > 0 {
		ch.didf("the test configurers gained a nil guard, a fake starter and a memory file store: %s", strings.Join(edited, "; "))
	}

	return nil
}

// wireHarnessStore builds the test configurer's resource client over a memory store:
// the configurer gains a files field, the literal that builds it sets a fresh store, and
// its ResourceClient method passes the store as resource.WithFileStore. A configurer
// that declares no ResourceClient in its file, builds the client elsewhere than
// resource.NewSpannerClient, or passes a store already is left as it is.
func (Files) wireHarnessStore(e *sourceEdit, h harness) (bool, error) {
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

// harness is one test configurer: the file declaring it, its type, the receiver as its
// methods write it (c *testConfigurer), and the methods it declares in that file.
type harness struct {
	file     string
	typeName string
	receiver string
	methods  []string
}

// harnessFiles finds the test configurers: the receiver types of a LogExporter method in
// the application's test files, outside the browser workspaces.
func (Files) harnessFiles(a *app.App) ([]harness, error) {
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	webDirs := map[string]bool{}
	for _, w := range a.WebApps {
		webDirs[w.Dir] = true
	}
	var found []harness
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.Wrap(walkErr, "fs.WalkDir()")
		}
		if d.IsDir() {
			if p != "." && (webDirs[p] || skippedNames[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") || strings.HasPrefix(d.Name(), "zz_gen_") {
			return nil
		}
		data, readErr := root.ReadFile(filepath.FromSlash(p))
		if readErr != nil {
			return errors.Wrap(readErr, "os.Root.ReadFile()")
		}
		// A file the tool cannot parse declares no configurer it can edit.
		if h, ok := configurerIn(p, data); ok {
			found = append(found, h)
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "fs.WalkDir()")
	}

	return found, nil
}

// configurerIn reads a test file for a type declaring LogExporter, and the methods that
// type declares there.
func configurerIn(rel string, src []byte) (harness, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return harness{}, false
	}
	byType := map[string]*harness{}
	var order []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
			continue
		}
		recv := fd.Recv.List[0]
		typeName := receiverTypeName(recv.Type)
		if typeName == "" {
			continue
		}
		h := byType[typeName]
		if h == nil {
			h = &harness{file: rel, typeName: typeName}
			byType[typeName] = h
			order = append(order, typeName)
		}
		h.methods = append(h.methods, fd.Name.Name)
		if fd.Name.Name == configurerMarker {
			h.receiver = string(src[fset.Position(recv.Pos()).Offset:fset.Position(recv.End()).Offset])
		}
	}
	for _, typeName := range order {
		if h := byType[typeName]; h.receiver != "" {
			return *h, true
		}
	}

	return harness{}, false
}

// receiverTypeName is the type a method's receiver names, through a pointer.
func receiverTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}

	return ""
}

// receiverName is the receiver's name in the named method of the type.
func receiverName(rel string, src []byte, typeName, method string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return "", errors.Wrap(err, "parser.ParseFile()")
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Name.Name != method || len(fd.Recv.List) != 1 || receiverTypeName(fd.Recv.List[0].Type) != typeName {
			continue
		}
		if len(fd.Recv.List[0].Names) == 0 {
			return "", errors.Wrapf(app.ErrNoAnchor, "%s: %s of %s names no receiver", rel, method, typeName)
		}

		return fd.Recv.List[0].Names[0].Name, nil
	}

	return "", errors.Wrapf(app.ErrNoAnchor, "%s declares no method %s of %s", rel, method, typeName)
}

// editProgram declares the rpc package to the generator where the program has no WithRPC
// yet, so the regeneration emits the scheduled method's handler and route.
func (Files) editProgram(a *app.App, g *app.Generator, rpcDir string, ch *Change) error {
	if rpcDir == "" || g.RPCDir() != "" {
		return nil
	}
	src, mode, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	edited, err := app.InsertOptions(g.File, src, "GenerateHandlers", []string{fmt.Sprintf("generation.WithRPC(%q)", rpcDir)})
	if err != nil {
		ch.skipf("%s: WithRPC(%q) was not added (%v); declare it so the generator reads the rpc package", g.File, rpcDir, err)

		return nil
	}
	if err := os.WriteFile(a.Abs(g.File), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: WithRPC(%q), so the generator reads the rpc package", g.File, rpcDir)

	return nil
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
	fmt.Fprintf(&b, "The framework keeps an application's uploaded files in a file store (`resource/filestore`), opened from one URL: `%s` is a directory in development, `gs://<bucket>` the bucket the stack makes for the application on Cloud Run, and the same code runs against either. The data level builds the resource client over the store (`resource.WithFileStore`), so the generated handlers stream uploads into it (`@upload`), serve a row's file from it (`@file`), and delete an object the moment the transaction that released it commits. Objects are named by UUID and held by rows; an object no row holds is an orphan, and the orphaned-file cleanup (`filestore.Cleanup`, run by `%s %s`) deletes the ones older than two days, refusing to run when no row holds any key. The cleanup runs as the application's job process, started once a day by the scheduled method `CleanUpFiles` (`@schedule(%q)`), which Cloud Scheduler calls under the scheduler's token and which starts the job through the job driver (`resource/jobs/cloudrun`, its settings embedded in the site level's environment); the service starts its job, and the cleanup never runs inside a request.\n\n", fileStoreDevURL, jobsCmdDir, cleanupCommand, cleanupSchedule)
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
