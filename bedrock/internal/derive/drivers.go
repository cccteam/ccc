// drivers.go names the provider drivers whose settings a configuration level embeds, and
// settles the remedy a refusal over a driver's settings missing from the configuration
// ends in: which upgrade, decided from the application's go.mod.

package derive

import (
	"fmt"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// resourceModule is the framework module the drivers ship in: the one requirement of the
// application's go.mod the drivers' remedy reads.
const resourceModule = "github.com/cccteam/ccc/resource"

// driversRelease is the resource release that introduced the provider drivers
// (resource/database/spanner, resource/live/firestore, resource/jobs/cloudrun), each
// declaring the variables it reads on a Settings struct the configuration embeds, which
// the stack reads for the variables it derives. impulse's provider-drivers step moves an
// application onto them: its recipe is what embeds the drivers' settings in the
// configuration. A refusal over a driver's settings missing from the configuration forks
// its remedy on this release: an application whose go.mod requires resource before it has
// not walked that step, so the remedy is impulse upgrade, whose step makes the edit, and
// not a hand edit against a resource that has no drivers to embed; one at or after it has
// walked the step, and the remedy is the hand edit, by the alias the skeleton binds the
// driver under.
const driversRelease = "v0.14.0"

// driver is one provider driver whose settings a configuration level embeds, as a remedy
// names it: the kind of driver, the import alias the skeleton and impulse's recipe bind
// it under, and the framework package of the driver.
type driver struct {
	kind  string
	alias string
	path  string
}

// The drivers, each under the neutral alias the skeleton binds it under (database,
// liveservice, jobstarter), so a remedy names the import line as the configuration spells
// it and not the vendor's package name.
var (
	databaseDriver = driver{kind: "database", alias: "database", path: "resource/database/spanner"}
	liveDriver     = driver{kind: "live", alias: "liveservice", path: "resource/live/firestore"}
	jobDriver      = driver{kind: "job", alias: "jobstarter", path: "resource/jobs/cloudrun"}
)

// handEdit is the edit that embeds the driver's settings where the stack reads them
// (embed the job driver's settings (jobstarter.Settings, resource/jobs/cloudrun) in the
// served site's configuration).
func (d driver) handEdit(where string) string {
	return fmt.Sprintf("embed the %s driver's settings (%s.Settings, %s) in %s", d.kind, d.alias, d.path, where)
}

// levelConfiguration names a level's configuration as a remedy places an edit in it.
func levelConfiguration(level string) string {
	return fmt.Sprintf("the %s level's configuration", level)
}

// resourceBeforeDrivers reads whether the application stands before the drivers' release:
// its go.mod requires resource at a version that sorts before driversRelease, the
// pseudo-version of a commit before it included. It answers that version, and empty when
// the application is not read as behind: resource required at or after the release,
// required by no directive, or replaced, since what a replace builds against is not a
// release the rule can place, and the remedy falls to the hand edit.
func resourceBeforeDrivers(f *modfile.File) string {
	if f == nil {
		return ""
	}
	for _, r := range f.Replace {
		if r.Old.Path == resourceModule {
			return ""
		}
	}
	for _, r := range f.Require {
		if r.Mod.Path == resourceModule && semver.Compare(r.Mod.Version, driversRelease) < 0 {
			return r.Mod.Version
		}
	}

	return ""
}

// driverRemedy is how a refusal over the driver's settings missing from the configuration
// ends: for an application behind the drivers' release, impulse upgrade first, whose
// provider-drivers step embeds the settings, then bedrock upgrade; for one at or after
// it, the hand edit that embeds the settings where the stack reads them.
func (m *Model) driverRemedy(d driver, where string) string {
	if m.behindDrivers != "" {
		return fmt.Sprintf("run impulse upgrade first (go.mod requires resource %s, before %s, the release with the drivers): its provider-drivers step embeds the driver settings in the configuration; then bedrock upgrade", m.behindDrivers, driversRelease)
	}

	return d.handEdit(where)
}
