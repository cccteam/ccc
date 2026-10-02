package check

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// featureFlags verifies the feature flags: the FeatureFlags and FeatureFlagChanges tables
// the generated routes and the deploy's MigrateFeatures read are in the schema as the
// resource module the application pins declares them (resource.FeatureFlagsDDL, read
// from the module's source in the module cache, so the comparison is against the library
// the application builds with), and every declared flag is a switch wired to something: a
// @feature annotation gating a resource, a field or a method, or a read in the
// application's own Go or browser code. A flag declared twice, and an annotation naming
// no declared constant, are findings too. The generator holds the rest (an unknown
// identifier or a malformed name is refused at generation).
type featureFlags struct{}

// featureFlagsName is the check's name.
const featureFlagsName = "feature-flags"

// The resource module whose declaration the schema is compared with, the function that
// declares the statements, the identifier of the Spanner case in it, and the generated
// file that says the generator emits feature flags.
const (
	resourceModule        = "github.com/cccteam/ccc/resource"
	featureDDLFunc        = "FeatureFlagsDDL"
	spannerDBTypeIdent    = "SpannerDBType"
	featuresGeneratedFile = "zz_gen_features.go"
)

func (featureFlags) Name() string { return featureFlagsName }

func (featureFlags) Describe() string {
	return "the FeatureFlags and FeatureFlagChanges tables match resource.FeatureFlagsDDL in the resource module the application pins, and every declared flag gates something or is read somewhere"
}

// Meaning explains the obligation for the handoff brief.
func (featureFlags) Meaning() string {
	return "A feature flag is a release switch: a `resource.Feature` constant in the resources package (`const Debriefs resource.Feature = \"debriefs\"`, its doc comment the description), `@feature(Debriefs)` on the struct of a `@resource`, `@virtual`, `@computed` or `@rpc` or on a field to put it behind the flag (off means absent: 404 on the routes, left out of the permission digest, a gated field unknown to the decoders), and a read where no resource stands behind the feature: `a.FeatureSet().Enabled(resources.Debriefs)` in Go, the generated `Feature.Debriefs` in the browser (`*cccFeature`, `featureMatch`, `feature` on a menu item). Each line under the check is one of three findings. A table that differs from the library's statement is brought to `resource.FeatureFlagsDDL(resource.SpannerDBType)` by a new migration, never by editing a committed one. A declared flag that gates nothing and is read nowhere is a switch wired to nothing: gate or read it, or retire it with `impulse remove feature <name>`. A flag declared twice, or an annotation naming no declared constant, is fixed in the declarations."
}

func (c featureFlags) Run(ctx context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}
	if !featuresGenerated(a, p) {
		if len(a.Features) == 0 {
			return skip(c.Name(), "the generator emits no feature flags (no "+featuresGeneratedFile+" in a resources package)")
		}
		details := make([]string, 0, len(a.Features))
		for _, flag := range a.Features {
			details = append(details, fmt.Sprintf("%s:%d: %s (%q) is declared, but the generator emits no feature flags (no %s in %s); move the resource pin to a version that carries them and regenerate", flag.File, flag.Line, flag.Constant, flag.Name, featuresGeneratedFile, path.Dir(flag.File)))
		}

		return fail(c.Name(), fmt.Sprintf("%d feature flag problem(s)", len(details)), details...)
	}

	tables, unread, err := migrationTables(a, p)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	var details []string
	library, source, problem, err := libraryFeatureDDL(ctx, env)
	switch {
	case err != nil:
		return fail(c.Name(), err.Error())
	case problem != "":
		details = append(details, problem)
	default:
		details = append(details, c.tableFindings(a, p, tables, library, source)...)
	}
	flagDetails, flagLines, err := c.flagFindings(a)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	details = append(details, flagDetails...)
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d feature flag problem(s)", len(details)), append(details, unread...)...)
	}
	declared := "no flag declared"
	if len(a.Features) > 0 {
		declared = fmt.Sprintf("%d flag(s) declared, each gating or read", len(a.Features))
	}

	return passWithDetails(c.Name(), "FeatureFlags and FeatureFlagChanges match the resource module's statements; "+declared, append(flagLines, unread...)...)
}

// featuresGenerated reports whether a site's generator wrote the feature declarations
// file into its resources package: the sign that the generator emits feature flags, so
// the application's App reads the FeatureFlags table at start.
func featuresGenerated(a *app.App, p app.Profile) bool {
	for i := range p.Sites {
		if _, err := os.Stat(a.Abs(path.Join(p.Sites[i].Generator.ResourcePackageDir, featuresGeneratedFile))); err == nil {
			return true
		}
	}

	return false
}

// tableFindings compares each table the library declares with the migration that
// creates it.
func (featureFlags) tableFindings(a *app.App, p app.Profile, tables, library map[string]string, source string) []string {
	names := make([]string, 0, len(library))
	for table := range library {
		names = append(names, table)
	}
	sort.Strings(names)
	var details []string
	for _, table := range names {
		file, ok := tables[table]
		if !ok {
			details = append(details, fmt.Sprintf("no migration creates the %s table, which the generated feature flag routes and the deploy's MigrateFeatures read; copy its statement from resource.%s(resource.%s) (%s) into the next migration under %s", table, featureDDLFunc, spannerDBTypeIdent, source, migrationsDir(p)))

			continue
		}
		statement, err := migrationStatement(a, file, table)
		if err != nil {
			details = append(details, err.Error())

			continue
		}
		if statement != library[table] {
			details = append(details, fmt.Sprintf("%s: CREATE TABLE %s differs from resource.%s(resource.%s) in %s; the library reads the table by those columns, so bring it to the statement in a new migration", file, table, featureDDLFunc, spannerDBTypeIdent, source))
		}
	}

	return details
}

// migrationsDir is the first site's schema migrations directory, for the messages.
func migrationsDir(p app.Profile) string {
	for i := range p.Sites {
		for _, src := range p.Sites[i].Generator.MigrationSources {
			if strings.HasPrefix(src, fileScheme) {
				return strings.TrimPrefix(src, fileScheme)
			}
		}
	}

	return "the schema migrations"
}

// flagFindings checks the declarations: each flag gates something or is read somewhere
// outside tests, no name is declared twice, and every annotation names a declared
// constant. It also renders one line per flag for the passing report.
func (featureFlags) flagFindings(a *app.App) (details, lines []string, err error) {
	uses, err := a.FeatureUses()
	if err != nil {
		return nil, nil, err
	}
	reads := map[string]int{}
	for _, u := range uses {
		if !u.Test {
			reads[u.Constant]++
		}
	}
	byName := map[string]app.FeatureFlag{}
	constants := map[string]bool{}
	for _, flag := range a.Features {
		constants[flag.Constant] = true
		if first, dup := byName[flag.Name]; dup {
			details = append(details, fmt.Sprintf("%s:%d: %s declares the flag %q, which %s already declares at %s:%d; MigrateFeatures refuses two declarations of one name", flag.File, flag.Line, flag.Constant, flag.Name, first.Constant, first.File, first.Line))

			continue
		}
		byName[flag.Name] = flag
		gates := a.GatesOf(flag.Constant)
		if len(gates) == 0 && reads[flag.Constant] == 0 {
			details = append(details, fmt.Sprintf("%s:%d: %s (%q) gates nothing and is read nowhere: put @feature(%s) on a resource, a field or a method, read it (a.FeatureSet().Enabled(%s.%s) in Go, Feature.%s in the browser), or remove it (impulse remove feature %s)", flag.File, flag.Line, flag.Constant, flag.Name, flag.Constant, path.Base(path.Dir(flag.File)), flag.Constant, flag.Constant, flag.Name))

			continue
		}
		lines = append(lines, fmt.Sprintf("%s (%s): %d gate(s), %d read(s)", flag.Constant, flag.Name, len(gates), reads[flag.Constant]))
	}
	for _, g := range a.FeatureGates {
		if !constants[g.Constant] {
			details = append(details, fmt.Sprintf("%s:%d: @feature(%s) on %s names no resource.Feature constant the application declares; the generator refuses it", g.File, g.Line, g.Constant, g.Target))
		}
	}

	return details, lines, nil
}

// libraryFeatureDDL reads the Spanner statements of resource.FeatureFlagsDDL from the
// resource module the application pins, found through go list in the module cache (or
// the checkout a replace or workspace points at), by the table each creates, normalized
// for comparison. source names the file they were read from. A module that cannot be
// found or carries no such function is a problem the check reports as a finding; a file
// that cannot be read is an error.
func libraryFeatureDDL(ctx context.Context, env *Env) (statements map[string]string, source, problem string, err error) {
	dir, problem := resourceModuleDir(ctx, env)
	if problem != "" {
		return nil, "", problem, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", "", errors.Wrapf(err, "os.ReadDir(): the resource module at %s", dir)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, "", "", errors.Wrap(err, "os.ReadFile()")
		}
		if !bytes.Contains(data, []byte("func "+featureDDLFunc+"(")) {
			continue
		}
		raw, err := spannerDDL(name, data)
		if err != nil {
			return nil, "", "", err
		}
		statements = make(map[string]string, len(raw))
		for _, stmt := range raw {
			m := createTableRE.FindStringSubmatch(stmt)
			if m == nil {
				continue
			}
			statements[m[1]] = normalizeSQL(stmt)
		}
		if len(statements) == 0 {
			return nil, "", fmt.Sprintf("%s in %s declares no CREATE TABLE statement for %s, so the schema cannot be compared with it", featureDDLFunc, filepath.Join(dir, name), spannerDBTypeIdent), nil
		}

		return statements, path.Join(path.Base(dir), name), "", nil
	}

	return nil, "", fmt.Sprintf("the resource module at %s declares no %s: its version predates feature flags, so the schema cannot be compared with it", dir, featureDDLFunc), nil
}

// resourceModuleDir asks go list where the resource module's source is: its directory
// in the module cache, or the checkout a replace or a workspace points at. A module go
// list cannot place is the problem returned.
func resourceModuleDir(ctx context.Context, env *Env) (dir, problem string) {
	out, err := env.Exec.Run(ctx, env.App.Root, nil, "go", "list", "-m", "-f", "{{.Dir}}", resourceModule)
	dir = strings.TrimSpace(string(out))
	if err != nil || dir == "" || strings.ContainsAny(dir, "\n") {
		return "", fmt.Sprintf("the resource module's source could not be found (go list -m -f {{.Dir}} %s): %s; run go mod download and check again", resourceModule, dir)
	}

	return dir, ""
}

// spannerDDL extracts the string literals the Spanner case of FeatureFlagsDDL returns.
func spannerDDL(name string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	var statements []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != featureDDLFunc || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok || !namesIdent(clause.List, spannerDBTypeIdent) {
				return true
			}
			for _, stmt := range clause.Body {
				ast.Inspect(stmt, func(n ast.Node) bool {
					lit, ok := n.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					if s, err := strconv.Unquote(lit.Value); err == nil {
						statements = append(statements, s)
					}

					return true
				})
			}

			return false
		})
	}

	return statements, nil
}

// namesIdent reports whether one of the case expressions is the identifier, bare or
// qualified.
func namesIdent(exprs []ast.Expr, name string) bool {
	for _, e := range exprs {
		switch x := e.(type) {
		case *ast.Ident:
			if x.Name == name {
				return true
			}
		case *ast.SelectorExpr:
			if x.Sel.Name == name {
				return true
			}
		}
	}

	return false
}

// migrationStatement reads the CREATE TABLE statement for the table from a migration
// file, normalized for comparison.
func migrationStatement(a *app.App, rel, table string) (string, error) {
	data, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return "", errors.Wrap(err, "os.ReadFile()")
	}
	for stmt := range strings.SplitSeq(string(data), ";") {
		stmt = stripSQLComments(stmt)
		m := createTableRE.FindStringSubmatch(stmt)
		if len(m) > 1 && m[1] == table {
			return normalizeSQL(stmt), nil
		}
	}

	return "", errors.Newf("%s: no CREATE TABLE %s statement could be read, though the file names the table", rel, table)
}

// stripSQLComments drops the -- comments from a statement's text.
func stripSQLComments(stmt string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(stmt, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// normalizeSQL collapses a statement's whitespace and drops its terminator, so a copied
// statement compares equal to the library's however it is indented.
func normalizeSQL(stmt string) string {
	return strings.TrimSuffix(strings.Join(strings.Fields(stmt), " "), ";")
}
