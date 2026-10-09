package recipe

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

// The files the provider-drivers recipe rewrites.
const (
	dataFile      = "pkg/config/data.go"
	dataTestFile  = "pkg/config/data_test.go"
	siteFile      = "pkg/config/site.go"
	scheduledFile = "pkg/config/scheduled.go"
)

// The imports the rewrite adds and drops.
const (
	databaseDriverImport = "github.com/cccteam/ccc/resource/database/spanner"
	jobDriverImport      = "github.com/cccteam/ccc/resource/jobs/cloudrun"
	jobsImport           = "github.com/cccteam/ccc/resource/jobs"
	scheduledImport      = "github.com/cccteam/ccc/resource/scheduled"
	resourceImport       = "github.com/cccteam/ccc/resource"
	fmtImport            = "fmt"
	slicesImport         = "slices"
)

// ProviderDrivers moves an application's database, live service and job starter from its
// config package into the drivers (resource/database/spanner, resource/live/firestore,
// resource/jobs/cloudrun): the data level embeds the database and live drivers' settings
// in its SpannerSettings and FirestoreSettings and opens the drivers, the site level
// embeds the job driver's settings and opens it, and the level's own resolution of the
// live service's project, with its test, is the live driver's.
type ProviderDrivers struct{}

// Name is the recipe's short name.
func (ProviderDrivers) Name() string { return "provider-drivers" }

// Meaning says what the change means in this framework.
func (ProviderDrivers) Meaning() string {
	return "The database, the live service and the job starter are opened by drivers in the shape the cloud driver set (resource/database/spanner, resource/live/firestore, resource/jobs/cloudrun): each declares the variables it reads on a Settings struct the configuration embeds, and the configuration opens it with Open and closes it with Close. Nothing in the application names a Spanner client, resolves the live service's project or reads the job template's variable any more; moving to another provider swaps the driver import and the embedded settings."
}

// dataMarkers are the signs of the old form in the data level.
var dataMarkers = []marker{
	{text: "cloudspanner.NewClient(", what: "the data level opens the Spanner client itself"},
	{text: "livefirestore.New(", what: "the data level opens the live service itself"},
	{text: "ProjectID    string `env:\"GOOGLE_CLOUD_SPANNER_PROJECT,required\"`", what: "the data level declares the database's variables itself"},
	{text: "ProjectID string `env:\"GOOGLE_CLOUD_FIRESTORE_PROJECT\"`", what: "the data level declares the live service's variables itself"},
}

// dataTestMarker is the sign of the old form in the data level's test.
var dataTestMarker = marker{text: "func TestFirestoreProject(", what: "the data level tests the live service's project resolution, which the live driver's tests cover"}

// siteMarker is the sign of the old form in the site level.
var siteMarker = marker{text: "jobs.FromEnvironment(", what: "the site level builds the job starter from the environment itself"}

// Detect names what in the application is in the old form.
func (ProviderDrivers) Detect(_ context.Context, a *app.App) ([]string, error) {
	var found []string
	for _, f := range []struct {
		rel     string
		markers []marker
	}{{dataFile, dataMarkers}, {dataTestFile, []marker{dataTestMarker}}, {siteFile, []marker{siteMarker}}} {
		data, err := os.ReadFile(a.Abs(f.rel))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}

			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		for _, m := range f.markers {
			if i := bytes.Index(data, []byte(m.text)); i >= 0 {
				found = append(found, fmt.Sprintf("%s:%d: %s", f.rel, 1+bytes.Count(data[:i], []byte("\n")), m.what))
			}
		}
	}

	return found, nil
}

// Apply rewrites the files whose old form it recognizes and reports the rest.
func (r ProviderDrivers) Apply(_ context.Context, a *app.App, _ check.Execer) (*transition.Change, error) {
	ch := &transition.Change{}
	if err := r.rewriteData(a, ch); err != nil {
		return nil, err
	}
	if err := r.removeDataTest(a, ch); err != nil {
		return nil, err
	}
	if err := r.rewriteSite(a, ch); err != nil {
		return nil, err
	}

	return ch, nil
}

// rewriteData moves the data level onto the database and live drivers.
func (ProviderDrivers) rewriteData(a *app.App, ch *transition.Change) error {
	src, mode, ok, err := read(a, dataFile)
	if err != nil || !ok || !hasMarker(src, dataMarkers) {
		return err
	}
	edited, reason := moveDataLevel(src)
	if reason != "" {
		ch.Skipped = append(ch.Skipped, fmt.Sprintf("%s: %s, so the data level was not rewritten; embed spanner.Settings in SpannerSettings and livefirestore.Settings in FirestoreSettings, open the drivers with spanner.Open (the Spanner client and the resource client are its) and livefirestore.Open (which resolves the live service's project), close the database driver in Close, and drop the Spanner client, the resource client and openLive", dataFile, reason))

		return nil
	}
	if err := write(a, dataFile, edited, mode); err != nil {
		return err
	}
	ch.Did = append(ch.Did, dataFile+": the data level embeds the database and live drivers' settings and opens the drivers, which hold the Spanner client, the resource client and the live service")
	if i := strings.Index(edited, "spannerClient"); i >= 0 {
		ch.Skipped = append(ch.Skipped, fmt.Sprintf("%s:%d: the data level still names spannerClient, which the driver holds now; read it as database.SpannerClient", dataFile, 1+strings.Count(edited[:i], "\n")))
	}

	return nil
}

// rewrite is a source under edit, with the first part of the old form the edits did not
// recognize; once a part is unrecognized the later edits do nothing, and the source is
// not written.
type rewrite struct {
	src    string
	reason string
}

// replace replaces the one occurrence of old, or records what was not found.
func (r *rewrite) replace(old, replacement, what string) {
	if r.reason != "" {
		return
	}
	if !strings.Contains(r.src, old) {
		r.reason = what + " is not in the skeleton's form"

		return
	}
	r.src = strings.Replace(r.src, old, replacement, 1)
}

// replaceEither replaces the one occurrence of the first of the old forms the source
// holds, or records what was not found.
func (r *rewrite) replaceEither(olds []string, replacement, what string) {
	if r.reason != "" {
		return
	}
	for _, old := range olds {
		if strings.Contains(r.src, old) {
			r.src = strings.Replace(r.src, old, replacement, 1)

			return
		}
	}
	r.reason = what + " is not in the skeleton's form"
}

// cutFunc removes the function the head line opens, with the doc comment above it and
// the blank line after it, or records that it was not found.
func (r *rewrite) cutFunc(head, what string) {
	if r.reason != "" {
		return
	}
	i := strings.Index(r.src, head)
	if i < 0 {
		r.reason = what + " is not in the skeleton's form"

		return
	}
	start := i
	for start > 0 {
		lineStart := strings.LastIndex(r.src[:start-1], "\n") + 1
		if !strings.HasPrefix(r.src[lineStart:start], "// ") {
			break
		}
		start = lineStart
	}
	end := strings.Index(r.src[i:], "\n}\n")
	if end < 0 {
		r.reason = what + " does not end"

		return
	}
	end += i + len("\n}\n")
	if strings.HasPrefix(r.src[end:], "\n") {
		end++
	}
	r.src = r.src[:start] + r.src[end:]
}

// moveDataLevel rewrites the data level's source from the skeleton's hand-wired form to
// the drivers, in the skeleton's form with or without the file store, or says which
// part of the form it did not recognize.
func moveDataLevel(src string) (edited, reason string) {
	r := &rewrite{src: src}
	r.replace(spannerStructOld, spannerStructNew, "the SpannerSettings struct")
	r.replace(databasePathFunc, "", "the DatabasePath method")
	r.replaceFirestoreSettings()
	r.replace(dataFieldsOld, dataFieldsNew, "the Spanner and resource client fields of DataConfiguration")
	r.replace(dataOwnsOld, dataOwnsNew, "the DataConfiguration comment")
	r.replaceDatabaseOpen()
	r.replace(liveOpenOld, liveOpenNew, "the live service's opening")
	r.cutFunc(openLiveFunc, "the openLive function")
	r.replace(closeOld, closeNew, "the Spanner client's close")
	r.replace(resourceClientDocOld, resourceClientDocNew, "the ResourceClient comment")
	r.replace(cloudSpannerImportLine, "", "the Spanner client's import, aliased cloudspanner")
	r.replace("\t\""+resourceImport+"\"\n", "\t\""+resourceImport+"\"\n\t\""+databaseDriverImport+"\"\n", "the resource import")
	if r.reason != "" {
		return "", r.reason
	}
	r.src = strings.ReplaceAll(r.src, "(ctx, spannerClient, ", "(ctx, database.SpannerClient, ")
	r.src = strings.ReplaceAll(r.src, "c.resourceClient", "c.database.ResourceClient")

	return dropUnusedImports(r.src, fmtImport, slicesImport), ""
}

// replaceFirestoreSettings replaces the FirestoreSettings struct and the methods the
// level declared on it (the project resolution, the browser origins) with the embedding
// of the live driver's settings, which carries them now.
func (r *rewrite) replaceFirestoreSettings() {
	if r.reason != "" {
		return
	}
	start := strings.Index(r.src, firestoreStructStart)
	if start < 0 {
		r.reason = "the FirestoreSettings struct is not in the skeleton's form"

		return
	}
	origins := strings.Index(r.src[start:], firestoreOriginsFunc)
	if origins < 0 {
		r.reason = "the FirestoreSettings methods are not in the skeleton's form (no BrowserOrigins)"

		return
	}
	end := strings.Index(r.src[start+origins:], "\n}\n")
	if end < 0 {
		r.reason = "the FirestoreSettings methods are not in the skeleton's form (BrowserOrigins does not end)"

		return
	}
	end += start + origins + len("\n}\n")
	region := r.src[start:end]
	if strings.Count(region, "\nfunc ") != 3 || !strings.Contains(region, "var firebaseOrigins") || !strings.Contains(region, "func (s FirestoreSettings) Project(") {
		r.reason = "the FirestoreSettings declarations are not the skeleton's (Configured, Project, firebaseOrigins and BrowserOrigins)"

		return
	}
	r.src = r.src[:start] + firestoreStructNew + r.src[end:]
}

// replaceDatabaseOpen replaces the Spanner client's opening with the database driver's,
// and, where the file store is opened after the client as the files transition wired
// it, moves the store's opening before the driver's and hands the driver the store's
// options, which the resource client took.
func (r *rewrite) replaceDatabaseOpen() {
	if r.reason != "" {
		return
	}
	open, oldLiteral := databaseOpenNew, dataLiteralOld
	if strings.Contains(r.src, filesBlockOld) {
		r.src = strings.Replace(r.src, filesBlockOld, "", 1)
		open = filesBlockNew + strings.Replace(databaseOpenNew, "env.Spanner.Settings)", "env.Spanner.Settings, fileStoreOptions(files)...)", 1)
		oldLiteral = dataLiteralFilesOld
	}
	r.replace(databaseOpenOld, open, "the Spanner client's opening")
	r.replace(oldLiteral, dataLiteralNew, "the DataConfiguration literal's clients")
}

// removeDataTest deletes the data level's test when the live service's project
// resolution is all it tests: the live driver's tests cover it.
func (ProviderDrivers) removeDataTest(a *app.App, ch *transition.Change) error {
	src, _, ok, err := read(a, dataTestFile)
	if err != nil || !ok || !strings.Contains(src, dataTestMarker.text) {
		return err
	}
	if strings.Count(src, "\nfunc Test") != 1 {
		ch.Skipped = append(ch.Skipped, dataTestFile+": the file tests more than the live service's project resolution, so it was not deleted; delete TestFirestoreProject, which the live driver's tests cover (livefirestore.Open)")

		return nil
	}
	if err := os.Remove(a.Abs(dataTestFile)); err != nil {
		return errors.Wrap(err, "os.Remove()")
	}
	ch.Did = append(ch.Did, dataTestFile+": deleted; the live service's project resolution it tested is the live driver's, tested there")

	return nil
}

// rewriteSite moves the site level onto the job driver: the settings embedded in the
// site's environment struct, the driver opened where the starter was built, and closed
// with the level.
func (r ProviderDrivers) rewriteSite(a *app.App, ch *transition.Change) error {
	src, mode, ok, err := read(a, siteFile)
	if err != nil || !ok || !strings.Contains(src, siteMarker.text) {
		return err
	}
	edited, reason := moveSiteLevel(src)
	if reason != "" {
		ch.Skipped = append(ch.Skipped, fmt.Sprintf("%s: %s, so the site level was not rewritten; embed cloudrun.Settings in the site's environment struct, open the driver with cloudrun.Open(ctx, env.Settings, data.AppVersion()) where jobs.FromEnvironment was called, hold it in the jobs field, and close it in Close", siteFile, reason))

		return nil
	}
	if err := write(a, siteFile, edited, mode); err != nil {
		return err
	}
	ch.Did = append(ch.Did, siteFile+": the site level embeds the job driver's settings and opens the driver, which names the job of this build and starts it")
	if err := r.rewordJobsAccessor(a); err != nil {
		return err
	}

	return r.closeDriver(a, ch)
}

// moveSiteLevel rewrites the site level's source from the hand-wired starter to the job
// driver, in the form the files transition wrote or the form Lodestar carries, or says
// which part it did not recognize.
func moveSiteLevel(src string) (edited, reason string) {
	r := &rewrite{src: src}
	if strings.Contains(r.src, siteFieldCommentedOld) {
		r.replace(siteFieldCommentedOld, siteFieldNew, "the jobs field")
	} else {
		var replaced bool
		r.src, replaced = replaceRegexp(r.src, siteFieldRE, "${1}jobs *cloudrun.Driver")
		if !replaced {
			return "", "the jobs field is not a jobs.Starter of SiteConfiguration"
		}
	}
	r.replaceEither([]string{siteOpenCommentedOld, siteOpenOld}, siteOpenNew, "the starter's building (starter, err := jobs.FromEnvironment(ctx))")
	if r.reason != "" {
		return "", r.reason
	}
	m := siteEnvStructRE.FindStringSubmatch(r.src)
	if m == nil {
		return "", "no environment struct (env := &T{}) is read to embed cloudrun.Settings in"
	}
	embedded, err := app.AddStructField(siteFile, []byte(r.src), m[1], jobDriverSettingsField)
	if err != nil {
		return "", "the environment struct " + m[1] + " took no field (" + err.Error() + ")"
	}
	r.src = string(embedded)
	r.replace("\t\""+scheduledImport+"\"\n", "\t\""+jobDriverImport+"\"\n\t\""+scheduledImport+"\"\n", "the scheduled import")
	if r.reason != "" {
		return "", r.reason
	}

	return dropUnusedImports(r.src, jobsImport), ""
}

// rewordJobsAccessor brings the Jobs accessor's comment, as the files transition wrote
// it, to what the transition writes on the drivers; an accessor worded otherwise keeps
// its words.
func (ProviderDrivers) rewordJobsAccessor(a *app.App) error {
	for _, rel := range []string{scheduledFile, siteFile} {
		src, mode, ok, err := read(a, rel)
		if err != nil || !ok || !strings.Contains(src, jobsAccessorDocOld) {
			if err != nil {
				return err
			}

			continue
		}

		return write(a, rel, strings.Replace(src, jobsAccessorDocOld, jobsAccessorDocNew, 1), mode)
	}

	return nil
}

// closeDriver releases the job driver with the site level: a Close the site declares in
// the skeleton's form gains the call, a site without one gains a Close in the scheduled
// file the files transition wrote, or in the site file, and any other Close is left to
// the agent.
func (ProviderDrivers) closeDriver(a *app.App, ch *transition.Change) error {
	rel, src, mode, err := siteCloseFile(a)
	if err != nil {
		return err
	}
	if rel != "" {
		if !strings.Contains(src, siteCloseOld) {
			ch.Skipped = append(ch.Skipped, rel+": SiteConfiguration's Close is not in a form the recipe knows, so the job driver is not closed; call c.jobs.Close() before the levels below close")

			return nil
		}
		if err := write(a, rel, strings.Replace(src, siteCloseOld, siteCloseNew, 1), mode); err != nil {
			return err
		}
		ch.Did = append(ch.Did, rel+": Close releases the job driver before the levels below")

		return nil
	}
	rel = siteFile
	if _, _, ok, err := read(a, scheduledFile); err != nil {
		return err
	} else if ok {
		rel = scheduledFile
	}
	src, mode, _, err = read(a, rel)
	if err != nil {
		return err
	}
	if err := write(a, rel, src+siteCloseSource, mode); err != nil {
		return err
	}
	ch.Did = append(ch.Did, rel+": Close releases the job driver, then the levels below")

	return nil
}

// siteCloseFile finds the config package's file declaring SiteConfiguration's Close, or
// none.
func siteCloseFile(a *app.App) (rel, src string, mode fs.FileMode, err error) {
	files, err := filepath.Glob(a.Abs("pkg/config/*.go"))
	if err != nil {
		return "", "", 0, errors.Wrap(err, "filepath.Glob()")
	}
	for _, abs := range files {
		if strings.HasSuffix(abs, "_test.go") {
			continue
		}
		rel := "pkg/config/" + filepath.Base(abs)
		src, mode, _, err := read(a, rel)
		if err != nil {
			return "", "", 0, err
		}
		if strings.Contains(src, siteCloseHead) {
			return rel, src, mode, nil
		}
	}

	return "", "", 0, nil
}

// replaceRegexp replaces the first match of the pattern and reports whether one was.
func replaceRegexp(src string, re *regexp.Regexp, replacement string) (string, bool) {
	loc := re.FindStringIndex(src)
	if loc == nil {
		return src, false
	}

	return src[:loc[0]] + re.ReplaceAllString(src[loc[0]:loc[1]], replacement) + src[loc[1]:], true
}

// hasMarker reports whether the source carries any of the markers.
func hasMarker(src string, markers []marker) bool {
	for _, m := range markers {
		if strings.Contains(src, m.text) {
			return true
		}
	}

	return false
}
