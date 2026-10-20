package derive

import (
	"strings"
	"testing"

	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/impulse/app"
)

// The go.mod shapes the drivers' remedy is read off: resource before the drivers'
// release, at a pseudo-version of a commit before it, at the release, after it, replaced,
// and not required.
const (
	goModHead = "module example.com/apps/harbor\n\ngo 1.25\n\n"

	goModBefore       = goModHead + "require github.com/cccteam/ccc/resource v0.13.0\n"
	goModPseudoBefore = goModHead + "require github.com/cccteam/ccc/resource v0.13.1-0.20261010150715-b61a62d06b0b\n"
	goModAt           = goModHead + "require github.com/cccteam/ccc/resource v0.14.0\n"
	goModAfter        = goModHead + "require github.com/cccteam/ccc/resource v0.15.0\n"
	goModReplaced     = goModBefore + "\nreplace github.com/cccteam/ccc/resource => ../resource\n"
	goModNone         = goModHead + "require github.com/cccteam/ccc v0.3.3\n"
)

// impulseFirst is the remedy for an application behind the drivers' release, whose
// go.mod requires resource at the version.
func impulseFirst(version string) string {
	return "run impulse upgrade first (go.mod requires resource " + version + ", before v0.14.0, the release with the drivers): its provider-drivers step embeds the driver settings in the configuration; then bedrock upgrade"
}

// parseGoMod parses a go.mod text.
func parseGoMod(t *testing.T, text string) *modfile.File {
	t.Helper()

	f, err := modfile.Parse("go.mod", []byte(text), nil)
	if err != nil {
		t.Fatalf("modfile.Parse() error = %v", err)
	}

	return f
}

// TestResourceBeforeDrivers reads the drivers' remedy fork off a go.mod: the resource
// requirement before the drivers' release, a pseudo-version of a commit before it
// included, is behind and answers its version; one at or after the release, a replaced
// one and none are not.
func TestResourceBeforeDrivers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		goMod string
		want  string
	}{
		{name: "resource v0.13.0, before the release, is behind", goMod: goModBefore, want: "v0.13.0"},
		{name: "a pseudo-version of a commit before the release is behind", goMod: goModPseudoBefore, want: "v0.13.1-0.20261010150715-b61a62d06b0b"},
		{name: "resource v0.14.0, the release, is not", goMod: goModAt},
		{name: "resource v0.15.0, after the release, is not", goMod: goModAfter},
		{name: "a replaced resource is not, whatever the requirement says", goMod: goModReplaced},
		{name: "no resource requirement is not", goMod: goModNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resourceBeforeDrivers(parseGoMod(t, tt.goMod)); got != tt.want {
				t.Errorf("resourceBeforeDrivers() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDriverRemedy holds each refusal over a driver's settings missing from the
// configuration to the remedy its go.mod decides: a job process without the job driver's
// settings, no database identity, and a Firestore database without the live driver's
// project variable. Behind the drivers' release, every one ends in impulse upgrade first,
// naming the version go.mod requires; at or after it, with a replace, or with no resource
// requirement, each ends in the hand edit by the driver's alias.
func TestDriverRemedy(t *testing.T) {
	t.Parallel()

	site := Process{Name: siteProcess, Dir: ".", Main: mainFile, Levels: []string{LevelCore, LevelData, LevelSite}}
	jobs := &Process{Name: jobsProcess, Dir: jobsDir, Main: jobsDir, Levels: []string{LevelCore, LevelData}}
	database := Variable{Name: varFirestoreDatabase, Role: RoleFirestoreDatabase, Level: LevelData, Struct: "dataConfig", Field: "FirestoreDatabase"}
	kinds := []struct {
		name string
		// refuse runs the refusal over a model whose remedy fork is set.
		refuse func(t *testing.T, behind string) error
		// handEdit is the remedy at or after the drivers' release.
		handEdit string
	}{
		{
			name: "a job process without the job driver's settings",
			refuse: func(_ *testing.T, behind string) error {
				m := Model{Sites: []Site{{Name: "harbor", Dir: ".", Process: site}}, Jobs: jobs, Variables: []Variable{{Name: "APP_OTHER", Level: LevelSite}}, behindDrivers: behind}

				return m.jobsTemplate()
			},
			handEdit: "embed the job driver's settings (jobstarter.Settings, resource/jobs/cloudrun) in the served site's configuration",
		},
		{
			name: "no database identity",
			refuse: func(_ *testing.T, behind string) error {
				m := Model{Variables: []Variable{{Name: "APP_OTHER", Level: LevelData}}, behindDrivers: behind}

				return m.classify(&config{})
			},
			handEdit: "embed the database driver's settings (database.Settings, resource/database/spanner) in the data level's configuration",
		},
		{
			name: "a Firestore database without the live driver's project variable",
			refuse: func(t *testing.T, behind string) error {
				t.Helper()

				m := Model{Variables: []Variable{database}, behindDrivers: behind}

				return m.firestore(&app.App{Root: t.TempDir()})
			},
			handEdit: "embed the live driver's settings (liveservice.Settings, resource/live/firestore) in the data level's configuration, in place of the variables declared by hand",
		},
	}
	tests := []struct {
		name  string
		goMod string
		// behind is the version the remedy names when the go.mod is behind, else empty
		// and the remedy is the kind's hand edit.
		behind string
	}{
		{name: "resource v0.13.0: impulse upgrade first", goMod: goModBefore, behind: "v0.13.0"},
		{name: "a pseudo-version before v0.14.0: impulse upgrade first", goMod: goModPseudoBefore, behind: "v0.13.1-0.20261010150715-b61a62d06b0b"},
		{name: "resource v0.14.0: the hand edit", goMod: goModAt},
		{name: "resource v0.15.0: the hand edit", goMod: goModAfter},
		{name: "a replace directive: the hand edit", goMod: goModReplaced},
		{name: "no resource requirement: the hand edit", goMod: goModNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			behind := resourceBeforeDrivers(parseGoMod(t, tt.goMod))
			if behind != tt.behind {
				t.Fatalf("resourceBeforeDrivers() = %q, want %q", behind, tt.behind)
			}
			for _, k := range kinds {
				want := k.handEdit
				if tt.behind != "" {
					want = impulseFirst(tt.behind)
				}
				err := k.refuse(t, behind)
				if err == nil || !strings.HasSuffix(err.Error(), "; "+want) {
					t.Errorf("%s: error = %v, want it to end in %q", k.name, err, want)
				}
			}
		})
	}
}
