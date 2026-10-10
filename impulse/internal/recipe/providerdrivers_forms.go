// Code generated from the skeleton's forms by the recipe's author; the forms are the
// exact texts the skeleton carried before and after the provider drivers, so the
// provider-drivers recipe rewrites only what it recognizes.

package recipe

import "regexp"

// The data level's forms: the SpannerSettings struct declaring the variables itself, and
// the struct embedding the database driver's settings, named for the kind.
// spannerStructOld is the SpannerSettings struct declaring the database's variables itself.
const spannerStructOld = "// SpannerSettings identifies the application's database. It is the first half of the\n// data level, loadable on its own so cmd/bootstrap can create the instance and database\n// before any client opens against them.\ntype SpannerSettings struct {\n\tProjectID    string `env:\"GOOGLE_CLOUD_SPANNER_PROJECT,required\"`\n\tInstanceID   string `env:\"GOOGLE_CLOUD_SPANNER_INSTANCE_ID,required\"`\n\tDatabaseName string `env:\"GOOGLE_CLOUD_SPANNER_DATABASE_NAME,required\"`\n}\n"

// databaseStructNew is the struct embedding the database driver's settings, under the
// alias the driver is bound by.
const databaseStructNew = `// DatabaseSettings is the database driver's variables (database.Settings, whichever
// driver the import names: the Spanner driver's project, instance, database and emulator
// host), embedded so the driver declares them and the tools read its declaration. It is
// the first half of the data level, loadable on its own so cmd/bootstrap can create the
// instance and database before any client opens against them. A struct of its own, since
// the live driver's settings sit beside it in the data level and one struct cannot embed
// two types named Settings.
type DatabaseSettings struct {
	database.Settings
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

// liveStructNew is the struct embedding the live driver's settings, which carry the methods now, under the alias the driver is bound by.
const liveStructNew = `// LiveSettings is the live driver's variables (liveservice.Settings, whichever driver the
// import names: the Firestore driver's project and database the live service runs on,
// beside the application's database, the web API key the browser initializes the SDK
// with, and the emulator host), embedded so the driver declares them and the tools read
// its declaration. The database id is how a deployment hands the database to the
// application (APP_FIRESTORE_DATABASE), with its project (GOOGLE_CLOUD_FIRESTORE_PROJECT);
// the emulator host is how the development stack does. The live service is required: the
// driver refuses to open with neither the database nor the emulator set.
type LiveSettings struct {
	liveservice.Settings
}
`

// dataFieldsOld are DataConfiguration's Spanner client and resource client fields.
const dataFieldsOld = `	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
`

// dataFieldsNew is the database driver's field, which holds both.
const dataFieldsNew = `	// database is the database driver: the Spanner client over the database and the
	// resource client over it.
	database *database.Driver
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

// databaseOpenNew opens the database driver, into a variable that does not shadow the
// alias.
const databaseOpenNew = `	// The database driver: the Spanner client over the database the settings name, and
	// the resource client over it.
	db, err := database.Open(ctx, env.Database.Settings)
	if err != nil {
		return nil, errors.Wrap(err, "database.Open()")
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

// liveOpenNew opens the live driver, which resolves the emulator's project itself.
const liveOpenNew = `	// through it. The live driver opens the Firestore database the settings name, or the
	// emulator, under the driver's own project id where the settings name none, since the
	// emulator takes any project id. The live service is required: the generated handlers
	// serve the live pages through it, and the application's instances signal each other
	// through its signals document (a feature flag flip, a policy write), so a configuration
	// naming neither, or naming the database without its project, fails the start here.
	liveService, err := liveservice.Open(ctx, env.Live.Settings)
	if err != nil {
		return nil, errors.Wrap(err, "liveservice.Open()")
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
const dataLiteralNew = `		database:          db,
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

// seamComment names the Spanner client the auths open on as the one vendor-specific seam
// left in the level, above the first auth built over it.
const seamComment = `	// The auth opens its stores on the driver's Spanner client: the one vendor-specific
	// seam left in the level, pending the PostgreSQL client (cccteam/ccc#852).
`

// spannerAccessorOld is the accessor of the database identity, named for the vendor.
const spannerAccessorOld = `// Spanner returns the database identity.
func (c *DataConfiguration) Spanner() SpannerSettings {
	return c.env.Spanner
}
`

// databaseAccessorNew is the accessor named for the kind.
const databaseAccessorNew = `// Database returns the database identity.
func (c *DataConfiguration) Database() DatabaseSettings {
	return c.env.Database
}
`

// dataConfigFieldsOld are the two drivers' settings on the level's environment struct,
// named for the vendors.
const dataConfigFieldsOld = "\tSpanner   SpannerSettings\n\tFirestore FirestoreSettings\n"

// dataConfigFieldsNew names them for the kinds.
const dataConfigFieldsNew = "\tDatabase DatabaseSettings\n\tLive     LiveSettings\n"

// The core level's forms: the cloud driver under its package name, as the cloud-driver
// recipe left it (its embedded settings are cloudSettingsField), and under the alias.
// cloudImportOld is the cloud driver's import under its package name.
const cloudImportOld = "\t\"" + gcpImport + "\"\n"

// cloudImportNew binds it under the alias.
const cloudImportNew = "\t" + cloudAlias + " \"" + gcpImport + "\"\n"

// cloudSettingsNew is the embedded settings under the alias.
const cloudSettingsNew = "\t// The cloud driver's variables (cloud.Settings, whichever driver the import names: the\n\t// Google Cloud driver's logging project and trace sampling).\n\tcloud.Settings\n"

// cloudFieldOld is the driver's field on coreConfiguration under the package name.
const cloudFieldOld = "\tcloud *gcp.Driver\n"

// cloudFieldNew is the field under the alias.
const cloudFieldNew = "\tcloud *cloud.Driver\n"

// cloudOpenOld opens the driver under the package name, into a variable named as the alias.
const cloudOpenOld = `	cloud, err := gcp.Open(ctx, env.Settings, env.ServiceName)
	if err != nil {
		return nil, errors.Wrap(err, "gcp.Open()")
	}

	return &coreConfiguration{env: env, cloud: cloud}, nil
`

// cloudOpenNew opens the driver under the alias, into a variable that does not shadow it.
const cloudOpenNew = `	driver, err := cloud.Open(ctx, env.Settings, env.ServiceName)
	if err != nil {
		return nil, errors.Wrap(err, "cloud.Open()")
	}

	return &coreConfiguration{env: env, cloud: driver}, nil
`

// The site level's forms: the starter built from the environment, as the files
// transition wrote it (commented) and as Lodestar carries it (bare), and the job driver.
// siteFieldCommentedOld is the starter field as the files transition wrote it.
const siteFieldCommentedOld = `	// jobs is the job process's starter (scheduled.go).
	jobs jobs.Starter
`

// siteFieldNew is the job driver's field, under the alias the driver is bound by.
const siteFieldNew = `	// jobs is the job driver: the starter of this build's job, or of none (scheduled.go).
	jobs *jobstarter.Driver
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
	starter, err := jobstarter.Open(ctx, env.Settings, data.AppVersion())
	if err != nil {
		return nil, errors.Wrap(err, "jobstarter.Open()")
	}
`

// jobDriverSettingsField embeds the job driver's settings in the site's environment struct.
const jobDriverSettingsField = `
// The job driver's variables (jobstarter.Settings, whichever driver the import names:
// the Cloud Run driver's template job this build's job is named from).
jobstarter.Settings`

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
	seamRE          = regexp.MustCompile(`(?m)^\t\w+, err := \w+\.New\(ctx, db\.SpannerClient, `)
	siteEnvStructRE = regexp.MustCompile(`(?m)^\s*env := &(\w+)\{\}`)
)
