package generation

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The record of what changes a published resource lives next to the resource: each
// package holding published virtual or computed resources gets a generated
// LiveDerivations value naming, per resource, the @rowsOf root, the other tables and
// whether the set was read off the query or declared with @changedBy. Every table is
// written as the generated Resource method of the type over it, never as a string, so a
// renamed or dropped table fails to compile there; a table no resource backs is never
// written through a handler, so no publish names it, and the entry's comment says the
// query reads it. The handler package's generated file is the assembly alone: it merges
// the packages' values into the one the generated handlers hand to every publish, so its
// import list follows the packages that publish.

// liveDerivationsValueName is the exported value each publishing package's generated
// file holds.
const liveDerivationsValueName = "LiveDerivations"

// liveDerivationsPackageData renders one package's generated live derivations.
type liveDerivationsPackageData struct {
	Source  string
	Package string
	// ResourcesQualifier prefixes the table-backed resource types the entries name: the
	// resources package's name and a dot, empty when the file is in that package.
	ResourcesQualifier string
	Resources          []derivedResourceData
}

// derivedResourceData is one published resource's entry.
type derivedResourceData struct {
	// Type is the resource's struct type, in the package the file is written to.
	Type string
	// Comment says how the set was found, and names the tables no resource backs.
	Comment string
	Global  bool
	// Root is the struct type over the @rowsOf root, empty without one.
	Root   string
	Tables []sourceTableData
}

// sourceTableData is one table of a set, as the struct type over it.
type sourceTableData struct {
	Type   string
	Global bool
}

// liveDerivationsAssemblyData renders the handler package's assembly of the packages'
// values.
type liveDerivationsAssemblyData struct {
	Source  string
	Package string
	// Values are the packages' LiveDerivations, each qualified by its package name
	// (bare when the package is the handler package itself), in package-name order.
	Values []string
}

// derivationsPackage is one package's published resources, ready to render.
type derivationsPackage struct {
	dir       packageDir
	Resources []derivedResourceData
}

// liveDerivationsByPackage groups the published resources' entries by the package their
// structs are in, in package-name order; a package with no published resource is not
// listed. The virtual and computed packages may be one package.
func (r *resourceGenerator) liveDerivationsByPackage() ([]derivationsPackage, error) {
	byDir := make(map[string]*derivationsPackage)
	add := func(dir packageDir, name string, global bool, decl *publishDecl) error {
		entry, err := r.derivedResourceData(name, global, decl)
		if err != nil {
			return err
		}
		key := filepath.Clean(dir.Dir())
		if byDir[key] == nil {
			byDir[key] = &derivationsPackage{dir: dir}
		}
		byDir[key].Resources = append(byDir[key].Resources, entry)

		return nil
	}
	for _, res := range r.resources {
		if !res.IsVirtual || !res.IsPublished() {
			continue
		}
		if err := add(r.virtual, res.Name(), !res.IsDomainScoped(), &res.publishDecl); err != nil {
			return nil, err
		}
	}
	for _, res := range r.computedResources {
		if !res.IsPublished() {
			continue
		}
		if err := add(r.computed, res.Name(), !res.IsDomainScoped(), &res.publishDecl); err != nil {
			return nil, err
		}
	}
	packages := make([]derivationsPackage, 0, len(byDir))
	for _, pkg := range byDir {
		slices.SortFunc(pkg.Resources, func(a, b derivedResourceData) int {
			return strings.Compare(a.Type, b.Type)
		})
		packages = append(packages, *pkg)
	}
	slices.SortFunc(packages, func(a, b derivationsPackage) int {
		return strings.Compare(a.dir.Package(), b.dir.Package())
	})

	return packages, nil
}

// derivedResourceData renders one published resource's entry: the struct type over
// each table of its set, the tables no resource backs named in the comment instead. The
// @rowsOf root is always a table-backed resource (resolveRowsOf), so a root with none
// is a generator fault.
func (r *resourceGenerator) derivedResourceData(name string, global bool, decl *publishDecl) (derivedResourceData, error) {
	entry := derivedResourceData{Type: name, Global: global}
	if root := decl.PublishedRoot(); root != "" {
		res := r.tableResource(root)
		if res == nil {
			return derivedResourceData{}, errors.Newf("struct %s: no table-backed resource is over %s, the table @%s names", name, root, rowsOfKeyword)
		}
		entry.Root = res.Name()
	}
	var unbacked []string
	for _, table := range decl.PublishedTables() {
		res := r.tableResource(table)
		if res == nil {
			unbacked = append(unbacked, table)

			continue
		}
		entry.Tables = append(entry.Tables, sourceTableData{Type: res.Name(), Global: !res.IsDomainScoped()})
	}
	entry.Comment = derivationComment(r.pluralize(name), decl.hasChangedBy, unbacked)

	return entry, nil
}

// derivationComment is the line above an entry: how the set was found, and the tables
// the set names that no resource backs, which the entry leaves out.
func derivationComment(resource string, declared bool, unbacked []string) string {
	origin := "the tables its query reads"
	if declared {
		origin = fmt.Sprintf("the tables declared with @%s", changedByKeyword)
	}
	comment := fmt.Sprintf("%s: %s.", resource, origin)
	switch len(unbacked) {
	case 0:
	case 1:
		comment += fmt.Sprintf(" The set also names %s, which no resource backs: no write to it is published, so it is left out.", unbacked[0])
	default:
		comment += fmt.Sprintf(" The set also names %s, which no resource backs: no write to them is published, so they are left out.", strings.Join(unbacked, ", "))
	}

	return comment
}

// tableResource is the table-backed resource over the table, nil when none is.
func (c *client) tableResource(table string) *resourceInfo {
	for _, res := range c.resources {
		if !res.IsVirtual && c.pluralize(res.Name()) == table {
			return res
		}
	}

	return nil
}

// packageQualifier is what a file in the from package writes before a name of the to
// package: the package's name and a dot, or nothing when the two are one package.
func packageQualifier(from, to packageDir) string {
	if filepath.Clean(from.Dir()) == filepath.Clean(to.Dir()) {
		return ""
	}

	return to.Package() + "."
}
