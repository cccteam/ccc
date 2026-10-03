package transition

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// AddFeature declares a feature flag: a resource.Feature constant with a doc stub in the
// resources package, the flag's row in the development seed (off), and a regeneration,
// so the generated Features() lists it and the browser's Feature union carries it. What
// the flag gates, who flips it and where the dialog's link lives are the agent's.
type AddFeature struct {
	// Name is the flag's name: lowercase letters, digits and underscores, opening with a
	// letter, at most 64 characters.
	Name string
	// Site names the site whose resources package declares the flag, in the sites
	// layout; empty in the flat layout, which has one.
	Site string
}

// RemoveFeature retires a feature flag: the constant, every @feature annotation naming
// it and its seed row go, the tree regenerates, and the hand-written code that still
// names the flag is listed for the agent, since the remaining code runs unconditionally.
type RemoveFeature struct {
	// Name is the flag's name.
	Name string
}

// The development seed file the flags' rows live in, by its stem, and the process the
// seeded rows name as their writer.
const (
	featureSeedStem   = "dev_feature_flags"
	featureSeedWriter = "Process devseed"
	featureFlagsTable = "FeatureFlags"
)

// featureSeedLead introduces the seed file.
const featureSeedLead = `-- The feature flags' development state: one row per declared flag, Enabled as
-- development and the test environments start with it. impulse add feature writes the
-- row off; set Enabled to TRUE to start with the feature on. The deploy's MigrateFeatures
-- keeps Enabled where a row exists and rewrites the description from the constant's doc
-- comment, so this row decides the state and nothing else; impulse remove feature
-- deletes it.
`

// featureSeedDownLead introduces the seed's down file.
const featureSeedDownLead = "-- Removes the seeded feature flag rows.\n"

// Command is the impulse command line for the transition.
func (f AddFeature) Command() string {
	if f.Site != "" {
		return fmt.Sprintf("impulse add feature %s --site %s", f.Name, f.Site)
	}

	return "impulse add feature " + f.Name
}

// Constant is the Go identifier the flag takes.
func (f AddFeature) Constant() string {
	return app.FeatureConstant(f.Name)
}

// Validate checks the flag against the application before anything is changed.
func (f AddFeature) Validate(a *app.App) error {
	if !app.FeatureNameRE.MatchString(f.Name) {
		return errors.Newf("feature name %q: a name is 1 to 64 characters of [a-z0-9_] opening with a letter, such as debriefs or cargo_manifest", f.Name)
	}
	if _, err := featureSite(a, f.Site); err != nil {
		return err
	}
	for _, flag := range a.Features {
		switch {
		case flag.Name == f.Name:
			return errors.Newf("%s:%d: %s already declares the flag %q", flag.File, flag.Line, flag.Constant, f.Name)
		case flag.Constant == f.Constant():
			return errors.Newf("%s:%d: %s already declares the flag %q, and %q would take the same constant", flag.File, flag.Line, flag.Constant, flag.Name, f.Name)
		}
	}

	return nil
}

// featureSite resolves the site whose resources package declares the application's
// flags: the one site of a flat application, or the named site of the sites layout.
func featureSite(a *app.App, name string) (*app.Site, error) {
	p := a.Profile()
	if len(p.Sites) == 0 {
		return nil, errors.New("no generator program emits handlers; the application has no resources package to declare a flag in")
	}
	if p.Layout == app.LayoutFlat {
		if name != "" {
			return nil, errors.Newf("the application is flat, with one resources package; --site is for the sites layout")
		}

		return &p.Sites[0], nil
	}
	if name == "" {
		return nil, errors.Newf("the application is in the sites layout: --site names the site whose resources package declares the flag (%s)", siteNames(p))
	}
	for i := range p.Sites {
		if p.Sites[i].Name == name {
			return &p.Sites[i], nil
		}
	}

	return nil, errors.Newf("no site named %s (the sites are %s)", name, siteNames(p))
}

// Apply makes the deterministic half of the transition: the constant, the seed row, and
// the regeneration.
func (f AddFeature) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := f.Validate(a); err != nil {
		return nil, err
	}
	site, err := featureSite(a, f.Site)
	if err != nil {
		return nil, err
	}
	ch := &Change{Command: f.Command()}
	if err := f.writeConstant(a, site.Generator.ResourcePackageDir, ch); err != nil {
		return nil, err
	}
	if err := f.writeSeedRow(a, devSeedDir(site), ch); err != nil {
		return nil, err
	}

	generate(ctx, a, exec, ch, fmt.Sprintf("go generate ./... failed, so Features() does not list %s yet; fix the cause and run it:", f.Constant()), fmt.Sprintf("ran go generate ./..., which listed %s in Features() and added %q to the browser's Feature union", f.Constant(), f.Name))

	return ch, nil
}

// writeConstant declares the flag in the resources package's features file, starting the
// file when the package has none.
func (f AddFeature) writeConstant(a *app.App, resourceDir string, ch *Change) error {
	rel := path.Join(resourceDir, app.FeaturesFile)
	src, mode, err := readFile(a, rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
		src, mode = nil, 0o644
	case err != nil:
		return err
	}
	pkg, err := packageNameIn(a, resourceDir)
	if err != nil {
		return err
	}
	out, err := app.AddFeatureConstant(rel, src, pkg, f.Constant(), f.Name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(rel), out, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: declared %s (%q) with a doc stub; the comment is the description the flags dialog shows, so replace it", rel, f.Constant(), f.Name)

	return nil
}

// writeSeedRow adds the flag's row, off, to the development seed's feature flags file,
// starting the file at the next data migration number when the seed has none.
func (f AddFeature) writeSeedRow(a *app.App, dir string, ch *Change) error {
	up, down, err := featureSeedFiles(a, dir)
	if err != nil {
		return err
	}
	row := fmt.Sprintf("INSERT INTO %s (Name, Description, Enabled, UpdatedAt, UpdatedBy)\n  VALUES ('%s', '', FALSE, PENDING_COMMIT_TIMESTAMP(), '%s');\n", featureFlagsTable, f.Name, featureSeedWriter)
	removal := fmt.Sprintf("DELETE FROM %s WHERE Name = '%s';\n", featureFlagsTable, f.Name)
	if up == "" {
		n := 1
		if _, err := os.Stat(a.Abs(dir)); err == nil {
			if n, err = nextMigration(a.Abs(dir)); err != nil {
				return err
			}
		}
		base := fmt.Sprintf("%06d_%s", n, featureSeedStem)
		up, down = path.Join(dir, base+".up.sql"), path.Join(dir, base+".down.sql")
		if err := writeNew(a, up, featureSeedLead+"\n"+row); err != nil {
			return err
		}
		if err := writeNew(a, down, featureSeedDownLead+"\n"+removal); err != nil {
			return err
		}
		ch.didf("%s and .down.sql: the development seed's feature flags file, with the %q row off; set Enabled to TRUE there to start development and the test environments with the feature on", up, f.Name)

		return nil
	}
	if err := appendStatement(a, up, row); err != nil {
		return err
	}
	if err := appendStatement(a, down, removal); err != nil {
		return err
	}
	ch.didf("%s and .down.sql: the %q row, off; set Enabled to TRUE there to start development and the test environments with the feature on", up, f.Name)

	return nil
}

// appendStatement adds a statement to the end of a seed file.
func appendStatement(a *app.App, rel, statement string) error {
	data, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	text := strings.TrimRight(string(data), "\n") + "\n\n" + statement
	if err := os.WriteFile(a.Abs(rel), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}

	return nil
}

// featureSeedFiles finds the development seed's feature flags file and its down file,
// root-relative, or empty when the seed has none.
func featureSeedFiles(a *app.App, dir string) (up, down string, err error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if errors.Is(err, os.ErrNotExist) {
		return "", "", nil
	}
	if err != nil {
		return "", "", errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_"+featureSeedStem+".up.sql") {
			continue
		}

		return path.Join(dir, name), path.Join(dir, strings.TrimSuffix(name, ".up.sql")+".down.sql"), nil
	}

	return "", "", nil
}

// devSeedDir is the development seed directory beside a site's schema migrations.
func devSeedDir(site *app.Site) string {
	migrations := strings.TrimPrefix(site.Generator.MigrationSources[0], fileScheme)

	return path.Join(path.Dir(migrations), "devseed")
}

// packageNameIn reads the package clause of the first non-test Go file in the directory,
// or the directory's name when it holds none.
func packageNameIn(a *app.App, dir string) (string, error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return "", errors.Wrapf(err, "os.ReadDir(): the resources package at %s", dir)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(a.Abs(path.Join(dir, e.Name())))
		if err != nil {
			continue
		}
		if name, err := app.PackageName(e.Name(), src); err == nil {
			return name, nil
		}
	}

	return path.Base(dir), nil
}

// Meaning explains a feature flag in this framework and names the wiring left to do.
func (f AddFeature) Meaning() string {
	c, n := f.Constant(), f.Name
	var b strings.Builder
	fmt.Fprintf(&b, "A feature flag is a release switch owned by the application: the code behind `%s` ships before the feature is turned on, the switch differs per environment (each environment's `FeatureFlags` table holds its own row, and `MigrateFeatures` writes a new flag off at deploy), and the flag is removed once the feature is permanent or abandoned (`impulse remove feature %s`). Per-user and per-tenant enablement are permissions, not flags, and a switch meant to stay forever is not one either.\n\n", c, n)
	b.WriteString("Left to wire, in this order:\n\n")
	items := []string{
		fmt.Sprintf("Gate something. `@feature(%s)` on the struct of a `@resource`, `@virtual`, `@computed` or `@rpc` gates it whole; on a field of a table, view or computed struct it gates the field (a key, the tenant key, the state column, a `@file` key and a column a create must supply cannot be gated). Then run `go generate ./...`. Off means absent: the routes answer the outlet's own 404, the permission digest leaves the resource, its fields and the method out (so the browser's navigation, buttons and generated routes never show them), and a gated field is unknown to the decoders (400) and left out of every response. Code with no resource behind it reads the flag through the App's copy, `a.FeatureSet().Enabled(resources.%[1]s)`, and the browser through the generated constant `Feature.%[1]s`: `*cccFeature=\"Feature.%[1]s\"` beside `*cccHasPermission`, `canMatch: [featureMatch(Feature.%[1]s)]` on the application's own routes, `feature: Feature.%[1]s` on a menu item (`@cccteam/resource-angular`, entry `auth-feature`). A misspelled flag fails to compile. `impulse check` (`feature-flags`) fails while the flag gates nothing and is read nowhere.", c),
		fmt.Sprintf("Describe it. The constant's doc comment in `%s` is the description the flags dialog shows beside the switch; replace the stub with one sentence saying what the feature turns on. The deploy rewrites the table's description from it at every release.", app.FeaturesFile),
		fmt.Sprintf("Flipping. The generated method `SetFeature` (`POST <prefix>/set-feature` with `{\"name\":%q,\"enabled\":true}`) flips the row, records the flip in `FeatureFlagChanges`, signals the other instances through the live service's signals document, the `features` kind (each reloads its copy; the five-minute backstop reread covers a missed signal) and answers the flag as written. Execute on `SetFeature`, with List and Read on `FeatureFlags`, is the `FeatureAdministrator` role in the roles file, global; who holds it is decided per application and environment, and the development login `admin` holds it in the bootstrap identities. The library's dialog, `FeatureFlagsDialogComponent` (`openFeatureFlagsDialog(inject(MatDialog))` from `@cccteam/resource-angular/ccc-feature-flags`), lists every flag with its description, a switch and how long it has been on in this environment, and refreshes the enabled set and the digest after a flip; the application decides where its link lives (the console's account menu is the usual place), and the digest decides what it may do: no Execute, no switches; no List, no dialog.", n),
		fmt.Sprintf("Development. The row `impulse add feature` wrote in `schema/devseed` is the flag's state in development and in the test environments the pipeline seeds: set its Enabled to TRUE to start with the feature on, since the deploy's `MigrateFeatures` keeps Enabled where a row exists. Production has no seed, so `%s` is off there until someone with the role turns it on.", n),
		"Tests. Once something is gated the generator writes `zz_gen_features_test.go` into the handler tests package: every gated route and field driven in both states of the flag, flipped in the test database through `resource.SetFeatureEnabled`. The authorization suite's `newTestHandler` builds the App over the test database, which reads its flags as it is built, so nothing there changes by hand.",
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}

	return b.String()
}

// Command is the impulse command line for the transition.
func (r RemoveFeature) Command() string {
	return "impulse remove feature " + r.Name
}

// Validate checks that the flag is declared before anything is changed.
func (r RemoveFeature) Validate(a *app.App) error {
	if !app.FeatureNameRE.MatchString(r.Name) {
		return errors.Newf("feature name %q: a name is 1 to 64 characters of [a-z0-9_] opening with a letter", r.Name)
	}
	if len(a.FeatureByName(r.Name)) == 0 {
		return errors.Newf("no resource.Feature constant declares the flag %q (the flags are %s)", r.Name, featureNames(a))
	}

	return nil
}

// featureNames lists the application's flags by name, or says there are none.
func featureNames(a *app.App) string {
	if len(a.Features) == 0 {
		return "none"
	}
	names := make([]string, 0, len(a.Features))
	for _, flag := range a.Features {
		names = append(names, flag.Name)
	}
	sort.Strings(names)

	return strings.Join(names, ", ")
}

// usesNote is the sentence the brief carries over the hand-written uses a removal leaves.
const usesNote = "The hand-written code below still names the flag. Its constant is gone, so each line fails to compile (Go) or to type-check (TypeScript) until it changes, and the remaining code runs unconditionally: inline the on branch to make the feature permanent, or delete the feature's code to abandon it."

// Apply makes the deterministic half of the transition: the constant, the annotations and
// the seed row go, the tree regenerates, and the hand-written uses are listed.
func (r RemoveFeature) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := r.Validate(a); err != nil {
		return nil, err
	}
	ch := &Change{Command: r.Command()}
	flags := a.FeatureByName(r.Name)
	uses, err := a.FeatureUses()
	if err != nil {
		return nil, err
	}
	for i := range flags {
		flag := &flags[i]
		if err := r.removeConstant(a, flag, ch); err != nil {
			return nil, err
		}
		if err := r.removeAnnotations(a, flag.Constant, ch); err != nil {
			return nil, err
		}
		for _, use := range uses {
			if use.Constant != flag.Constant {
				continue
			}
			what := path.Base(path.Dir(flag.File)) + "." + flag.Constant
			if path.Ext(use.File) != ".go" {
				what = "Feature." + flag.Constant
			}
			ch.Uses = append(ch.Uses, fmt.Sprintf("%s:%d: %s", use.File, use.Line, what))
		}
	}
	ch.UsesNote = usesNote
	if err := r.removeSeedRows(a, ch); err != nil {
		return nil, err
	}

	generate(ctx, a, exec, ch, fmt.Sprintf("go generate ./... failed, so Features() still lists %q; fix the cause and run it:", r.Name), fmt.Sprintf("ran go generate ./..., which dropped %q from Features() and from the browser's Feature union", r.Name))

	return ch, nil
}

// removeConstant deletes the flag's declaration, and its file when nothing else is left
// in it.
func (RemoveFeature) removeConstant(a *app.App, flag *app.FeatureFlag, ch *Change) error {
	src, mode, err := readFile(a, flag.File)
	if err != nil {
		return err
	}
	out, empty, err := app.RemoveFeatureConstant(flag.File, src, flag.Constant)
	if err != nil {
		return err
	}
	if empty {
		if err := os.Remove(a.Abs(flag.File)); err != nil {
			return errors.Wrap(err, "os.Remove()")
		}
		ch.didf("deleted %s, which declared %s (%q) and nothing else", flag.File, flag.Constant, flag.Name)

		return nil
	}
	if err := os.WriteFile(a.Abs(flag.File), out, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: removed %s (%q)", flag.File, flag.Constant, flag.Name)

	return nil
}

// removeAnnotations takes every @feature annotation naming the constant out of the files
// that carry one.
func (RemoveFeature) removeAnnotations(a *app.App, constant string, ch *Change) error {
	byFile := map[string][]string{}
	var files []string
	for _, g := range a.GatesOf(constant) {
		if _, seen := byFile[g.File]; !seen {
			files = append(files, g.File)
		}
		byFile[g.File] = append(byFile[g.File], g.Target)
	}
	sort.Strings(files)
	for _, rel := range files {
		src, mode, err := readFile(a, rel)
		if err != nil {
			return err
		}
		out, removed, err := app.RemoveFeatureAnnotations(rel, src, constant)
		if err != nil {
			return err
		}
		if removed == 0 {
			continue
		}
		if err := os.WriteFile(a.Abs(rel), out, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		ch.didf("%s: removed @feature(%s) from %s, which the next regeneration serves unconditionally", rel, constant, strings.Join(byFile[rel], ", "))
	}

	return nil
}

// removeSeedRows deletes the flag's row from every site's development seed.
func (r RemoveFeature) removeSeedRows(a *app.App, ch *Change) error {
	p := a.Profile()
	seen := map[string]bool{}
	for i := range p.Sites {
		site := &p.Sites[i]
		if len(site.Generator.MigrationSources) == 0 || !strings.HasPrefix(site.Generator.MigrationSources[0], fileScheme) {
			continue
		}
		dir := devSeedDir(site)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		up, down, err := featureSeedFiles(a, dir)
		if err != nil {
			return err
		}
		if up == "" {
			continue
		}
		removedUp, err := removeStatements(a, up, featureRowRE(r.Name))
		if err != nil {
			return err
		}
		removedDown, err := removeStatements(a, down, featureRemovalRE(r.Name))
		if err != nil {
			return err
		}
		if removedUp+removedDown > 0 {
			ch.didf("%s and .down.sql: removed the %q row; the table's row goes at the next deploy, when MigrateFeatures deletes what the release no longer declares, and its FeatureFlagChanges rows stay as the audit", up, r.Name)
		}
	}

	return nil
}

// featureRowRE matches the seed's INSERT of one flag, with the blank line before it.
func featureRowRE(name string) *regexp.Regexp {
	return regexp.MustCompile(`\n*INSERT INTO ` + featureFlagsTable + `[^;]*'` + regexp.QuoteMeta(name) + `'[^;]*;\n?`)
}

// featureRemovalRE matches the seed's DELETE of one flag, with the blank line before it.
func featureRemovalRE(name string) *regexp.Regexp {
	return regexp.MustCompile(`\n*DELETE FROM ` + featureFlagsTable + ` WHERE Name = '` + regexp.QuoteMeta(name) + `';\n?`)
}

// removeStatements deletes every statement the pattern matches from a seed file and
// reports how many went. A missing file holds nothing to remove.
func removeStatements(a *app.App, rel string, re *regexp.Regexp) (int, error) {
	data, mode, err := readFile(a, rel)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	matches := re.FindAllIndex(data, -1)
	if len(matches) == 0 {
		return 0, nil
	}
	out := strings.TrimRight(string(re.ReplaceAll(data, []byte("\n"))), "\n") + "\n"
	if err := os.WriteFile(a.Abs(rel), []byte(out), mode); err != nil {
		return 0, errors.Wrap(err, "os.WriteFile()")
	}

	return len(matches), nil
}

// Meaning explains what a removal leaves.
func (r RemoveFeature) Meaning() string {
	return fmt.Sprintf("The flag %q is gone from the code: its constant, every `@feature` naming it, its seed row, and after regeneration its entry in `Features()` and in the browser's `Feature` union and constants. Everything the flag gated is served unconditionally now, and the remaining hand-written code that read it (listed above) runs unconditionally too: inline the on branch to make the feature permanent, or delete the feature's code to abandon it. Nothing in the database refuses the removal: the `FeatureFlags` row goes at the next deploy, when `MigrateFeatures` deletes what the release no longer declares, and its `FeatureFlagChanges` rows stay as the record of every flip.", r.Name)
}
