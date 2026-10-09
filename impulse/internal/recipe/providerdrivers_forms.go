// Code generated from the skeleton's forms by the recipe's author; the forms are the
// exact texts the skeleton carried before and after the provider drivers, so the
// provider-drivers recipe rewrites only what it recognizes.

package recipe

import "regexp"

// The data level's forms: the SpannerSettings struct declaring the variables itself, and
// the struct embedding the database driver's settings.
// spannerStructOld is the SpannerSettings struct declaring the database's variables itself.
const spannerStructOld = "// SpannerSettings identifies the application's database. It is the first half of the\n// data level, loadable on its own so cmd/bootstrap can create the instance and database\n// before any client opens against them.\ntype SpannerSettings struct {\n\tProjectID    string `env:\"GOOGLE_CLOUD_SPANNER_PROJECT,required\"`\n\tInstanceID   string `env:\"GOOGLE_CLOUD_SPANNER_INSTANCE_ID,required\"`\n\tDatabaseName string `env:\"GOOGLE_CLOUD_SPANNER_DATABASE_NAME,required\"`\n}\n"

// spannerStructNew is the struct embedding the database driver's settings.
const spannerStructNew = `// SpannerSettings is the database driver's variables (spanner.Settings: the project, the
// instance and the database), embedded so the driver declares them and the tools read its
// declaration. It is the first half of the data level, loadable on its own so
// cmd/bootstrap can create the instance and database before any client opens against
// them. A struct of its own, since the live driver's settings sit beside it in the data
// level and one struct cannot embed two types named Settings.
type SpannerSettings struct {
	spanner.Settings
}
`

// databasePathFunc is the method the driver's settings carry now; it goes.
const databasePathFunc = `// DatabasePath returns the fully qualified Spanner database path.
func (s SpannerSettings) DatabasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", s.ProjectID, s.InstanceID, s.DatabaseName)
}

`

// firestoreStructStart opens the FirestoreSettings declarations: the struct and the methods the level declared on it.
const firestoreStructStart = `// FirestoreSettings identifies the Firestore database the live service runs on, beside
`

// firestoreOriginsFunc is the last of those methods; the region ends with it.
const firestoreOriginsFunc = `func (s FirestoreSettings) BrowserOrigins() []string {`

// firestoreStructNew is the struct embedding the live driver's settings, which carry the methods now.
const firestoreStructNew = `// FirestoreSettings is the live driver's variables (livefirestore.Settings: the project
// and the database the live service runs on, beside the Spanner database, the web API key
// the browser initializes the SDK with, and the emulator host), embedded so the driver
// declares them and the tools read its declaration. The database id is how a deployment
// hands the database to the application (APP_FIRESTORE_DATABASE), with its project
// (GOOGLE_CLOUD_FIRESTORE_PROJECT); the emulator host is how the development stack does.
// The live service is required: the driver refuses to open with neither the database nor
// the emulator set.
type FirestoreSettings struct {
	livefirestore.Settings
}
`

// dataFieldsOld are DataConfiguration's Spanner client and resource client fields.
const dataFieldsOld = `	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
`

// dataFieldsNew is the database driver's field, which holds both.
const dataFieldsNew = `	// database is the database driver: the Spanner client over the database and the
	// resource client over it.
	database *spanner.Driver
`

// dataOwnsOld is the DataConfiguration comment's line naming the clients it owns.
const dataOwnsOld = `// owns the Spanner client, the resource client over it, the live service over the
`

// dataOwnsNew names the driver.
const dataOwnsNew = `// owns the database driver (the Spanner client over the database and the resource
// client over it, the one the handlers read and write through), the live service over the
`

// databaseOpenOld opens the Spanner client.
const databaseOpenOld = `	spannerClient, err := cloudspanner.NewClient(ctx, env.Spanner.DatabasePath())
	if err != nil {
		return nil, errors.Wrap(err, "spanner.NewClient()")
	}
`

// databaseOpenNew opens the database driver.
const databaseOpenNew = `	// The database driver: the Spanner client over the database the settings name, and
	// the resource client over it.
	database, err := spanner.Open(ctx, env.Spanner.Settings)
	if err != nil {
		return nil, errors.Wrap(err, "spanner.Open()")
	}
`

// filesBlockOld opens the file store after the Spanner client, as the files transition wired it; the driver takes the store's options, so the block moves before it.
const filesBlockOld = `	// The file store belongs beside the database: the resource client is built over it
	// (resource.WithFileStore), so the generated handlers read and write files through the
	// client, and a committed transaction's released objects are deleted from the store. It
	// opens from APP_FILE_STORE (files.go): a directory in development, a bucket on
	// Cloud Run. A process whose variable is unset runs without a store, and the server
	// refuses to start when its routes need one.
	files, err := openFileStore(ctx, env.FileStores)
	if err != nil {
		return nil, err
	}

`

// filesBlockNew opens the file store before the database driver.
const filesBlockNew = `	// The file store belongs beside the database: the database driver builds the resource
	// client over it (resource.WithFileStore), so the generated handlers read and write files
	// through the client, and a committed transaction's released objects are deleted from the
	// store. It opens from APP_FILE_STORE (files.go): a directory in development, a bucket on
	// Cloud Run. A process whose variable is unset runs without a store, and the server
	// refuses to start when its routes need one.
	files, err := openFileStore(ctx, env.FileStores)
	if err != nil {
		return nil, err
	}

`

// liveOpenOld opens the live service through the level's own openLive.
const liveOpenOld = `	// through it.
	liveService, err := openLive(ctx, env)
	if err != nil {
		return nil, err
	}
`

// liveOpenNew opens the live driver.
const liveOpenNew = `	// through it. The live driver opens the Firestore database the settings name, or the
	// emulator, under the Spanner project where the settings name none, since the emulator
	// takes any project id. The live service is required: the generated handlers serve the
	// live pages through it, and the application's instances signal each other through its
	// signals document (a feature flag flip, a policy write), so a configuration naming
	// neither, or naming the database without its project, fails the start here.
	liveService, err := livefirestore.Open(ctx, env.Firestore.Settings, env.Spanner.ProjectID)
	if err != nil {
		return nil, errors.Wrap(err, "firestore.Open()")
	}
`

// openLiveFunc is the function that resolved the live service's project and opened it; the driver does both.
const openLiveFunc = `func openLive(ctx context.Context, env *dataConfig) (*livefirestore.Service, error) {`

// closeOld closes the Spanner client.
const closeOld = `	c.spannerClient.Close()
`

// closeNew closes the database driver.
const closeNew = `	c.database.Close()
`

// dataLiteralOld sets the clients on the level.
const dataLiteralOld = `		spannerClient:     spannerClient,
		resourceClient:    resource.NewSpannerClient(spannerClient),
`

// dataLiteralFilesOld sets the clients on the level, the resource client over the file store.
const dataLiteralFilesOld = `		spannerClient:     spannerClient,
		resourceClient:    resource.NewSpannerClient(spannerClient, fileStoreOptions(files)...),
`

// dataLiteralNew sets the driver on the level.
const dataLiteralNew = `		database:          database,
`

// resourceClientDocOld is the ResourceClient accessor's comment.
const resourceClientDocOld = `// ResourceClient returns the database client the resource layer uses.
`

// resourceClientDocNew names the driver.
const resourceClientDocNew = `// ResourceClient returns the database client the resource layer uses: the driver's.
`

// cloudSpannerImportLine is the Spanner client's import, aliased; it goes.
const cloudSpannerImportLine = `	cloudspanner "cloud.google.com/go/spanner"
`

// The site level's forms: the starter built from the environment, as the files
// transition wrote it (commented) and as Lodestar carries it (bare), and the job driver.
// siteFieldCommentedOld is the starter field as the files transition wrote it.
const siteFieldCommentedOld = `	// jobs is the job process's starter (scheduled.go).
	jobs jobs.Starter
`

// siteFieldNew is the job driver's field.
const siteFieldNew = `	// jobs is the job driver: the starter of this build's job, or of none (scheduled.go).
	jobs *cloudrun.Driver
`

// siteOpenCommentedOld builds the starter from the environment, as the files transition wrote it.
const siteOpenCommentedOld = `	// The job process's starter (scheduled.go): it names the job of this build from the template
	// job the stack sets in APP_JOBS_TEMPLATE and the version the image bakes in, and without a
	// template refuses every start.
	starter, err := jobs.FromEnvironment(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "jobs.FromEnvironment()")
	}
`

// siteOpenOld builds the starter from the environment, bare.
const siteOpenOld = `	starter, err := jobs.FromEnvironment(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "jobs.FromEnvironment()")
	}
`

// siteOpenNew opens the job driver.
const siteOpenNew = `	// The job driver (scheduled.go): it names the job of this build from the template job the
	// stack sets in APP_JOBS_TEMPLATE, the setting the site's environment embeds, and the
	// version the image bakes in, and without a template refuses every start.
	starter, err := cloudrun.Open(ctx, env.Settings, data.AppVersion())
	if err != nil {
		return nil, errors.Wrap(err, "cloudrun.Open()")
	}
`

// jobDriverSettingsField embeds the job driver's settings in the site's environment struct.
const jobDriverSettingsField = `
// The Cloud Run job driver's variables: the template job this build's job is named
// from.
cloudrun.Settings`

// siteCloseHead opens SiteConfiguration's Close, wherever the package declares it.
const siteCloseHead = `func (c *SiteConfiguration) Close() {`

// siteCloseOld is a Close that releases the levels below alone.
const siteCloseOld = `// Close releases the levels below.
func (c *SiteConfiguration) Close() {
	c.DataConfiguration.Close()
}
`

// siteCloseNew releases the job driver first.
const siteCloseNew = `// Close releases the job driver, then the levels below.
func (c *SiteConfiguration) Close() {
	c.jobs.Close()
	c.DataConfiguration.Close()
}
`

// siteCloseSource is the Close a site level without one gains.
const siteCloseSource = `
// Close releases the job driver, then the levels below.
func (c *SiteConfiguration) Close() {
	c.jobs.Close()
	c.DataConfiguration.Close()
}
`

// jobsAccessorDocOld is the Jobs accessor's comment as the files transition wrote it.
const jobsAccessorDocOld = `// Jobs starts the application's job process: the job of this build, named from the
// template job the stack sets in APP_JOBS_TEMPLATE and the version the image bakes in
// (resource/jobs), or a starter that refuses where no template is configured.
`

// jobsAccessorDocNew is the comment the transition writes on the drivers.
const jobsAccessorDocNew = `// Jobs starts the application's job process: the job driver (resource/jobs/cloudrun),
// which names the job of this build from the template job the stack sets in
// APP_JOBS_TEMPLATE and the version the image bakes in, or refuses every start where no
// template is configured.
`

// siteFieldRE matches a bare starter field of SiteConfiguration (jobs jobs.Starter,
// however aligned), and siteEnvStructRE the site's environment struct where it is read.
var (
	siteFieldRE     = regexp.MustCompile(`(?m)^(\t+)jobs\s+jobs\.Starter$`)
	siteEnvStructRE = regexp.MustCompile(`(?m)^\s*env := &(\w+)\{\}`)
)
