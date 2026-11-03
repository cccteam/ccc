package generation

import (
	"go/ast"
	"go/constant"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

// A virtual or computed resource has no rows of its own: its rows come from the tables
// its query or its body reads, so a commit that touches those tables changes it, and
// the live publish has to know which tables those are. For a virtual resource the
// generator reads them off the query itself: the text behind the struct's Subquery
// method, an embedded file or a string literal, scanned for the schema's table names.
// The scan cannot miss a table, since a query reads nothing it does not name, and it
// may over-include a column that shares a table's name, which costs one needless
// notification. A computed resource is Go, so its author declares the tables with
// @changedBy; the declaration may also add tables to a virtual resource's derived set,
// and it is the only source for a query the generator cannot read (one built at run
// time), which is refused without it. The @rowsOf root, when the resource declares
// one, must be in the set: it is the table whose rows the resource carries, and a change
// to it reaches the resource under the same key (live.Derivations). The resolved sets
// are written into the handler package as the generated live derivations, so what the
// generator concluded is in the diff after every regeneration, and into the TypeScript
// metadata as the resource's live flag.

// subqueryMethodName is the method a virtual resource's query is read from.
const subqueryMethodName = "Subquery"

// publishDecl records what a virtual or computed resource's changes come from: the
// tables its author declared with @changedBy, and once every kind is extracted
// (resolvePublished) the resolved set, the @rowsOf root apart.
type publishDecl struct {
	// changedBy is the declaration's table names as written; hasChangedBy says the
	// declaration was made, so an empty one is refused rather than ignored.
	changedBy    []string
	hasChangedBy bool
	// published is the resolved set; nil while unresolved and for a resource whose
	// changes are not published (a computed resource with no declaration).
	published *publishedSet
}

// publishedSet is the tables a published resource's changes come from.
type publishedSet struct {
	// Root is the table whose rows the resource carries under the same key (@rowsOf);
	// empty when the resource carries no table's rows.
	Root string
	// Tables are the other tables, sorted, the root left out.
	Tables []string
}

// IsPublished reports whether the resource's changes are published: the resolved set
// names at least one table.
func (d *publishDecl) IsPublished() bool {
	return d.published != nil
}

// PublishedRoot is the root table of the published set, empty without one.
func (d *publishDecl) PublishedRoot() string {
	if d.published == nil {
		return ""
	}

	return d.published.Root
}

// PublishedTables are the published set's tables apart from the root.
func (d *publishDecl) PublishedTables() []string {
	if d.published == nil {
		return nil
	}

	return d.published.Tables
}

// declareChangedBy records a struct's @changedBy table names for resolvePublished,
// which runs once every kind is extracted and the schema is known.
func declareChangedBy(annotations genlang.StructAnnotations, dest *publishDecl) {
	if !annotations.Struct.Has(changedByKeyword) {
		return
	}
	dest.hasChangedBy = true
	for invocation := range annotations.Struct.Get(changedByKeyword).Seq() {
		for name := range strings.SplitSeq(invocation, ",") {
			dest.changedBy = append(dest.changedBy, strings.TrimSpace(name))
		}
	}
}

// declareViewDeclarations records a view's struct-scope @rowsOf and @changedBy for the
// resolutions that run once every kind is extracted (resolveViewDeclarations).
func declareViewDeclarations(annotations genlang.StructAnnotations, rowsOf *rowsOfDecl, publish *publishDecl) {
	declareRowsOf(annotations, rowsOf)
	declareChangedBy(annotations, publish)
}

// resolveViewDeclarations resolves the views' struct-scope declarations in their order:
// @rowsOf first, since the published set must hold the root it names, then the tables
// each view's changes come from.
func (c *client) resolveViewDeclarations(resources []*resourceInfo, computed []*computedResource) error {
	if err := c.resolveRowsOf(resources, computed); err != nil {
		return err
	}

	return c.resolvePublished(resources, computed)
}

// rejectChangedBy fails a struct of a kind whose changes need no declaration: a
// table-backed resource's changes are its own rows, and an RPC method has none.
func rejectChangedBy(pStruct *parser.Struct, annotations genlang.StructAnnotations, kind string) error {
	if !annotations.Struct.Has(changedByKeyword) {
		return nil
	}

	return errors.Newf("struct %s: @%s is only valid on @%s and @%s structs, whose rows come from other tables; this %s publishes its own rows", pStruct.Name(), changedByKeyword, virtualKeyword, computedKeyword, kind)
}

// resolvePublished resolves which tables every virtual and computed resource's changes
// come from, once every kind is extracted and the @rowsOf declarations are resolved: a
// virtual resource's set is read off its query text and extended by its declaration, a
// computed resource's is its declaration alone. Every refusal names the fix.
func (c *client) resolvePublished(resources []*resourceInfo, computed []*computedResource) error {
	tables := c.schemaTableNames()
	var errs []error
	for _, res := range resources {
		if !res.IsVirtual {
			continue
		}
		if err := c.resolveVirtualPublished(res, tables); err != nil {
			errs = append(errs, err)
		}
	}
	for _, res := range computed {
		if err := resolveComputedPublished(res, tables); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Wrapf(errors.Join(errs...), "the tables a resource's changes come from (@%s)", changedByKeyword)
	}

	return nil
}

// schemaTableNames maps every schema table's name, lowercased, to its spelling, for the
// case-insensitive matches the query scan and the declaration make.
func (c *client) schemaTableNames() map[string]string {
	tables := make(map[string]string, len(c.tableMap))
	for name := range c.tableMap {
		tables[strings.ToLower(name)] = name
	}

	return tables
}

// resolveVirtualPublished resolves one virtual resource: the tables its query names,
// the ones declared, the root checked.
func (c *client) resolveVirtualPublished(res *resourceInfo, tables map[string]string) error {
	declared, err := declaredTables(res.Name(), &res.publishDecl, tables)
	if err != nil {
		return err
	}
	text, readable, err := c.subqueryText(res)
	if err != nil {
		return err
	}
	if !readable && !res.hasChangedBy {
		return errors.Newf("struct %s: the query behind %s is built at run time, so the generator cannot read which tables it reads and cannot publish the resource's changes to live pages; keep the query an embedded file or a string literal, or declare the tables it reads with @%s(Table, ...)", res.Name(), subqueryMethodName, changedByKeyword)
	}
	var derived []string
	if readable {
		derived = tablesNamed(text, tables)
	}
	set := union(derived, declared)
	if res.HasRowsOf() && !slices.Contains(set, res.RowsOf) {
		if res.hasChangedBy {
			return errors.Newf("struct %s: @%s(%s) leaves out %s, the table @%s names, whose rows the resource carries; declare it with the others", res.Name(), changedByKeyword, strings.Join(res.changedBy, ", "), res.RowsOf, rowsOfKeyword)
		}

		return errors.Newf("struct %s: the query behind %s names no table called %s, the table @%s says the resource carries the rows of; a query over a table's rows names the table, so fix the query or the declaration, or add @%s(%s)", res.Name(), subqueryMethodName, res.RowsOf, rowsOfKeyword, changedByKeyword, res.RowsOf)
	}
	res.published = publishedSetOf(res.RowsOf, set)

	return nil
}

// resolveComputedPublished resolves one computed resource: its declaration alone, the
// root checked; a computed resource with no declaration is not published.
func resolveComputedPublished(res *computedResource, tables map[string]string) error {
	if !res.hasChangedBy {
		return nil
	}
	declared, err := declaredTables(res.Name(), &res.publishDecl, tables)
	if err != nil {
		return err
	}
	if res.HasRowsOf() && !slices.Contains(declared, res.RowsOf) {
		return errors.Newf("struct %s: @%s(%s) leaves out %s, the table @%s names, whose rows the resource carries; declare it with the others", res.Name(), changedByKeyword, strings.Join(res.changedBy, ", "), res.RowsOf, rowsOfKeyword)
	}
	res.published = publishedSetOf(res.RowsOf, declared)

	return nil
}

// declaredTables resolves a declaration's names against the schema, in the schema's
// spelling: an empty declaration and an unknown name are refused.
func declaredTables(structName string, decl *publishDecl, tables map[string]string) ([]string, error) {
	if !decl.hasChangedBy {
		return nil, nil
	}
	resolved := make([]string, 0, len(decl.changedBy))
	for _, name := range decl.changedBy {
		if name == "" {
			return nil, errors.Newf("struct %s: @%s takes the tables whose changes change the resource, by schema name: @%s(Missions, Clients)", structName, changedByKeyword, changedByKeyword)
		}
		table, ok := tables[strings.ToLower(name)]
		if !ok {
			return nil, errors.Newf("struct %s: @%s(%s): %q is not a table in the schema; name the tables the resource reads by their schema names", structName, changedByKeyword, strings.Join(decl.changedBy, ", "), name)
		}
		resolved = append(resolved, table)
	}

	return resolved, nil
}

// publishedSetOf builds the resolved set: the root apart, the rest sorted and unique;
// nil when the set is empty, since a resource reading no table never changes.
func publishedSetOf(root string, set []string) *publishedSet {
	tables := make([]string, 0, len(set))
	for _, table := range set {
		if table != root && !slices.Contains(tables, table) {
			tables = append(tables, table)
		}
	}
	slices.Sort(tables)
	if root == "" && len(tables) == 0 {
		return nil
	}

	return &publishedSet{Root: root, Tables: tables}
}

// union joins two name lists, each name once, in first-seen order.
func union(a, b []string) []string {
	joined := make([]string, 0, len(a)+len(b))
	for _, name := range slices.Concat(a, b) {
		if !slices.Contains(joined, name) {
			joined = append(joined, name)
		}
	}

	return joined
}

// The query scan: comments and string literals go first, so a table named in a remark or
// a label is not read; what is left is split into identifiers. A backticked name and a
// dotted one both yield their identifiers, so Clients, `Clients` and c.Clients all name
// Clients, and a parameter's @ is not an identifier character.
var (
	sqlLineComment   = regexp.MustCompile(`--[^\n]*`)
	sqlBlockComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	sqlStringLiteral = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`)
	sqlIdentifier    = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
)

// tablesNamed returns the schema tables a query's text names, in the schema's spelling,
// sorted and unique.
func tablesNamed(text string, tables map[string]string) []string {
	stripped := sqlBlockComment.ReplaceAllString(text, " ")
	stripped = sqlLineComment.ReplaceAllString(stripped, " ")
	stripped = sqlStringLiteral.ReplaceAllString(stripped, " ")
	var named []string
	for _, identifier := range sqlIdentifier.FindAllString(stripped, -1) {
		table, ok := tables[strings.ToLower(identifier)]
		if ok && !slices.Contains(named, table) {
			named = append(named, table)
		}
	}
	slices.Sort(named)

	return named
}

// subqueryText reads the text of a virtual resource's query: what its Subquery method
// returns first, when that is a constant the compiler knows, a package-level variable
// filled by a go:embed directive, or a concatenation of those. readable is false for a
// query the generator cannot read: a method with no single return, or a value built at
// run time. An error is a loaded-package problem, not a judgment about the query.
func (c *client) subqueryText(res *resourceInfo) (text string, readable bool, err error) {
	pkg := c.loadedPackages[res.PackageName()]
	if pkg == nil {
		return "", false, errors.Newf("struct %s: package %s was not loaded with the run, so its %s method cannot be read", res.Name(), res.PackageName(), subqueryMethodName)
	}
	decl := methodDecl(pkg, res.Name(), subqueryMethodName)
	if decl == nil || decl.Body == nil {
		return "", false, nil
	}
	var returns []*ast.ReturnStmt
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok {
			returns = append(returns, ret)
		}

		return true
	})
	if len(returns) != 1 || len(returns[0].Results) == 0 {
		return "", false, nil
	}

	return constantText(pkg, returns[0].Results[0], 0)
}

// methodDecl finds a method's declaration on the named type, on either receiver form.
func methodDecl(pkg *packages.Package, typeName, method string) *ast.FuncDecl {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != method || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			if receiverTypeName(fn.Recv.List[0].Type) == typeName {
				return fn
			}
		}
	}

	return nil
}

// receiverTypeName is the type a receiver names, the pointer and any type arguments
// stripped.
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.IndexExpr:
		return receiverTypeName(e.X)
	case *ast.IndexListExpr:
		return receiverTypeName(e.X)
	case *ast.Ident:
		return e.Name
	default:
		return ""
	}
}

// constantTextDepth bounds how far constantText follows one variable into another.
const constantTextDepth = 8

// constantText resolves an expression to the string it holds at compile time: a
// constant the type checker evaluated, a parenthesized or concatenated pair of such, or
// a package-level variable whose declaration is a go:embed directive or another such
// expression. Anything else is a run-time value.
func constantText(pkg *packages.Package, expr ast.Expr, depth int) (text string, readable bool, err error) {
	if depth > constantTextDepth {
		return "", false, nil
	}
	if tv, ok := pkg.TypesInfo.Types[expr]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return constant.StringVal(tv.Value), true, nil
	}
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return constantText(pkg, e.X, depth+1)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false, nil
		}
		left, ok, err := constantText(pkg, e.X, depth+1)
		if err != nil || !ok {
			return "", false, err
		}
		right, ok, err := constantText(pkg, e.Y, depth+1)
		if err != nil || !ok {
			return "", false, err
		}

		return left + right, true, nil
	case *ast.Ident:
		return variableText(pkg, e.Name, depth)
	default:
		return "", false, nil
	}
}

// variableText resolves a package-level variable by name: the file its go:embed
// directive names, read from the variable's own directory, or the value it is declared
// with.
func variableText(pkg *packages.Package, name string, depth int) (text string, readable bool, err error) {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				index := slices.IndexFunc(valueSpec.Names, func(ident *ast.Ident) bool {
					return ident.Name == name
				})
				if index < 0 {
					continue
				}
				if pattern, ok := embedPattern(genDecl, valueSpec); ok {
					return embeddedText(pkg, valueSpec, pattern)
				}
				if index < len(valueSpec.Values) {
					return constantText(pkg, valueSpec.Values[index], depth+1)
				}

				return "", false, nil
			}
		}
	}

	return "", false, nil
}

// embedPattern is the one file a go:embed directive on the variable names, from the
// spec's own doc or, for a variable declared alone, the declaration's; a directive with
// several patterns fills no string and is not one.
func embedPattern(genDecl *ast.GenDecl, spec *ast.ValueSpec) (string, bool) {
	for _, group := range []*ast.CommentGroup{spec.Doc, genDecl.Doc} {
		if group == nil {
			continue
		}
		for _, comment := range group.List {
			after, ok := strings.CutPrefix(comment.Text, "//go:embed")
			if !ok {
				continue
			}
			patterns := strings.Fields(after)
			if len(patterns) != 1 {
				return "", false
			}

			return strings.Trim(patterns[0], `"`), true
		}
	}

	return "", false
}

// embeddedText reads the embedded file, named relative to the file that declares the
// variable, as go:embed does.
func embeddedText(pkg *packages.Package, spec *ast.ValueSpec, pattern string) (text string, readable bool, err error) {
	dir := filepath.Dir(pkg.Fset.Position(spec.Pos()).Filename)
	content, err := os.ReadFile(filepath.Join(dir, pattern))
	if err != nil {
		return "", false, errors.Wrapf(err, "reading the embedded query %s", pattern)
	}

	return string(content), true, nil
}

// The generated live derivations: one entry per published virtual or computed resource,
// in name order, each table marked global when the resource backing it has no tenant.

type liveDerivationsData struct {
	Source    string
	Package   string
	Resources []derivedResourceData
}

type derivedResourceData struct {
	Resource string
	Global   bool
	Root     string
	Tables   []sourceTableData
}

type sourceTableData struct {
	Name   string
	Global bool
}

// liveDerivations lists the published virtual and computed resources for the generated
// live derivations file.
func (c *client) liveDerivations() []derivedResourceData {
	var entries []derivedResourceData
	for _, res := range c.resources {
		if !res.IsVirtual || !res.IsPublished() {
			continue
		}
		entries = append(entries, c.derivedResourceData(c.pluralize(res.Name()), !res.IsDomainScoped(), &res.publishDecl))
	}
	for _, res := range c.computedResources {
		if !res.IsPublished() {
			continue
		}
		entries = append(entries, c.derivedResourceData(c.pluralize(res.Name()), !res.IsDomainScoped(), &res.publishDecl))
	}
	slices.SortFunc(entries, func(a, b derivedResourceData) int {
		return strings.Compare(a.Resource, b.Resource)
	})

	return entries
}

func (c *client) derivedResourceData(name string, global bool, decl *publishDecl) derivedResourceData {
	entry := derivedResourceData{Resource: name, Global: global, Root: decl.PublishedRoot()}
	for _, table := range decl.PublishedTables() {
		entry.Tables = append(entry.Tables, sourceTableData{Name: table, Global: c.tableIsGlobal(table)})
	}

	return entry
}

// tableIsGlobal reports whether the table-backed resource over the table has no tenant
// domain; a table no resource backs is never written through a handler, so its answer
// does not matter and is false.
func (c *client) tableIsGlobal(table string) bool {
	for _, res := range c.resources {
		if !res.IsVirtual && c.pluralize(res.Name()) == table {
			return !res.IsDomainScoped()
		}
	}

	return false
}
