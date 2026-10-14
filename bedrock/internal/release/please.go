package release

// release-please, which cuts every application's releases: each application's stack
// renders a release trigger that runs it in Cloud Build as the application's release
// identity, as the release GitHub App, on every push to the default branch and the hotfix
// lines (bedrock deploy release). PleaseVersion is the version the trigger's step runs,
// through npx; a bedrock release moves it. PleaseConfig and PleaseManifest are its two
// files at the application root, which bedrock render seeds once and release-please
// reads: the configuration, and the manifest holding the last released version.
const (
	PleaseVersion  = "17.11.2"
	PleaseConfig   = "release-please-config.json"
	PleaseManifest = ".release-please-manifest.json"
)
