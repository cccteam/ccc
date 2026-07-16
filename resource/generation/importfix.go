package generation

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// notLocallyFixable is the pseudo-qualifier reported when the import block's shape
// (dot or blank imports, comments inside the block, interleaved declarations)
// prevents local fixing, deferring the file to goimports.
const notLocallyFixable = "<import block not locally fixable>"

// importFixer resolves the import block of a rendered template locally, without
// goimports' filesystem and subprocess machinery: it prunes declared imports the
// file does not reference and adds imports for referenced qualifiers it can
// resolve from known — the packages of every parsed field type plus a stdlib
// seed. A qualifier it cannot resolve is reported back so the caller can fall
// back to full goimports resolution for that file.
type importFixer struct {
	// known maps a package qualifier to its import path.
	known map[string]string
	// conflicted holds qualifiers that map to more than one import path; a file
	// referencing one cannot be resolved locally.
	conflicted map[string][]string
}

// newImportFixer builds a fixer from authoritative imports (package names known
// from type-checked parse data) and assumedPaths (import paths whose package
// name can only be assumed from the path, e.g. the configured localPackages).
func newImportFixer(imports []fixerImport, assumedPaths []string) *importFixer {
	f := &importFixer{
		known:      stdlibImports(),
		conflicted: make(map[string][]string),
	}

	for _, imp := range imports {
		f.add(imp.name, imp.path)
	}

	for _, path := range assumedPaths {
		f.addAssumed(path)
	}

	return f
}

func (f *importFixer) add(name, path string) {
	if existing, ok := f.known[name]; ok {
		if existing != path {
			if !slices.Contains(f.conflicted[name], existing) {
				f.conflicted[name] = append(f.conflicted[name], existing)
			}
			if !slices.Contains(f.conflicted[name], path) {
				f.conflicted[name] = append(f.conflicted[name], path)
			}
		}

		return
	}

	f.known[name] = path
}

// addAssumed registers a weak entry whose package name is assumed from the
// import path: it never displaces or conflicts with an existing entry, because
// an authoritative (type-checked) name always outranks a path-derived guess.
func (f *importFixer) addAssumed(path string) {
	name := assumedPackageName(path)
	if _, ok := f.known[name]; ok {
		return
	}
	if len(f.conflicted[name]) > 0 {
		return
	}

	f.known[name] = path
}

// fix rewrites src's import block to exactly the set the file references.
// It returns the fixed source and the qualifiers it could not resolve; when
// unknown is non-empty (or on error) the returned bytes must be discarded and
// the caller should fall back to goimports resolution on the original src.
func (f *importFixer) fix(fileName string, src []byte) (fixed []byte, unknown []string, err error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fileName, src, parser.ParseComments)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "parser.ParseFile(): file: %s", fileName)
	}

	decls := importDecls(file)
	if !fixable(file, decls) {
		return nil, []string{notLocallyFixable}, nil
	}

	declared := make(map[string]*ast.ImportSpec)
	for _, decl := range decls {
		for _, spec := range decl.Specs {
			imp, ok := spec.(*ast.ImportSpec)
			if !ok {
				continue
			}

			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "strconv.Unquote(%s): file: %s", imp.Path.Value, fileName)
			}

			name := assumedPackageName(path)
			if imp.Name != nil {
				name = imp.Name.Name
			}

			declared[name] = imp
		}
	}

	used := referencedQualifiers(file)

	keep := make([]fixerImport, 0, len(used))
	for _, name := range used {
		switch {
		case declared[name] != nil:
			path, _ := strconv.Unquote(declared[name].Path.Value)
			keep = append(keep, fixerImport{name: name, path: path})
		case len(f.conflicted[name]) > 0:
			unknown = append(unknown, fmt.Sprintf("%s (ambiguous: %s)", name, strings.Join(f.conflicted[name], ", ")))
		case f.known[name] != "":
			keep = append(keep, fixerImport{name: name, path: f.known[name]})
		default:
			unknown = append(unknown, name)
		}
	}

	if len(unknown) > 0 {
		return nil, unknown, nil
	}

	return spliceImports(fset, src, decls, keep, file.Name.End()), nil, nil
}

// fixerImport is a single resolvable import: the package's declared name
// (qualifier) and its import path.
type fixerImport struct {
	name string
	path string
}

// stdlibImports seeds the qualifiers of standard-library packages that template
// text may reference without declaring. Qualifiers the generated code resolves
// to third-party packages (errors -> go-playground/errors, cmp -> go-cmp/cmp)
// must stay out of this list.
func stdlibImports() map[string]string {
	return map[string]string{
		"bytes":   "bytes",
		"context": "context",
		"fmt":     "fmt",
		"http":    "net/http",
		"iter":    "iter",
		"json":    "encoding/json",
		"maps":    "maps",
		"reflect": "reflect",
		"slices":  "slices",
		"strconv": "strconv",
		"strings": "strings",
		"time":    "time",
		"url":     "net/url",
	}
}

// importDecls returns the file's import declarations in source order.
func importDecls(file *ast.File) []*ast.GenDecl {
	var decls []*ast.GenDecl
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			decls = append(decls, gen)
		}
	}

	return decls
}

// fixable reports whether the import declarations are simple enough to rewrite
// wholesale: contiguous at the top of the file, free of comments, and free of
// dot or blank imports. Rendered templates always satisfy this; anything else
// is left to goimports.
func fixable(file *ast.File, decls []*ast.GenDecl) bool {
	for i, decl := range decls {
		if i > 0 && declsBetween(file, decls[i-1], decl) {
			return false
		}

		for _, spec := range decl.Specs {
			imp, ok := spec.(*ast.ImportSpec)
			if !ok {
				return false
			}
			if imp.Name != nil && (imp.Name.Name == "_" || imp.Name.Name == ".") {
				return false
			}
		}
	}

	if len(decls) > 0 {
		start, end := decls[0].Pos(), decls[len(decls)-1].End()
		for _, group := range file.Comments {
			if group.Pos() >= start && group.End() <= end {
				return false
			}
		}
	}

	return true
}

// declsBetween reports whether any other declaration sits between a and b.
func declsBetween(file *ast.File, a, b *ast.GenDecl) bool {
	for _, decl := range file.Decls {
		if decl.Pos() > a.End() && decl.End() < b.Pos() {
			return true
		}
	}

	return false
}

// referencedQualifiers returns the sorted set of identifiers used as package
// qualifiers: the X of a SelectorExpr that does not resolve to a local object.
// This mirrors goimports' own reference collection.
func referencedQualifiers(file *ast.File) []string {
	seen := make(map[string]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		if ident.Obj == nil {
			seen[ident.Name] = struct{}{}
		}

		return true
	})

	return slices.Sorted(maps.Keys(seen))
}

// versionSuffix matches major-version import path elements (v2, v3...).
var versionSuffix = regexp.MustCompile(`^v\d+$`)

// assumedPackageName derives a package name from an import path the way
// goimports does for unnamed imports: the last path element, skipping a
// trailing major-version element.
func assumedPackageName(path string) string {
	base := path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		if versionSuffix.MatchString(base[i+1:]) {
			base = base[:i]
		}
	}
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}

	return base
}

// spliceImports replaces the file's import declarations with a canonical block
// holding exactly imports: two groups (standard library, then everything else),
// each sorted by path, named specs only where the package name cannot be assumed
// from the path. packageEnd is the insertion point when the file has no import
// declaration.
func spliceImports(fset *token.FileSet, src []byte, decls []*ast.GenDecl, imports []fixerImport, packageEnd token.Pos) []byte {
	block := renderImportBlock(imports)

	var start, end int
	if len(decls) > 0 {
		start = fset.Position(decls[0].Pos()).Offset
		end = fset.Position(decls[len(decls)-1].End()).Offset
	} else {
		if block == "" {
			return src
		}
		start = fset.Position(packageEnd).Offset
		end = start
		block = "\n\n" + block
	}

	out := make([]byte, 0, len(src)+len(block))
	out = append(out, src[:start]...)
	out = append(out, block...)
	out = append(out, src[end:]...)

	return out
}

func renderImportBlock(imports []fixerImport) string {
	if len(imports) == 0 {
		return ""
	}

	var std, other []fixerImport
	for _, imp := range imports {
		if root, _, _ := strings.Cut(imp.path, "/"); strings.Contains(root, ".") {
			other = append(other, imp)
		} else {
			std = append(std, imp)
		}
	}

	byPath := func(a, b fixerImport) int { return strings.Compare(a.path, b.path) }
	slices.SortFunc(std, byPath)
	slices.SortFunc(other, byPath)

	spec := func(imp fixerImport) string {
		if assumedPackageName(imp.path) != imp.name {
			return imp.name + " " + strconv.Quote(imp.path)
		}

		return strconv.Quote(imp.path)
	}

	if len(std)+len(other) == 1 {
		return "import " + spec(append(std, other...)[0])
	}

	var sb strings.Builder
	sb.WriteString("import (\n")
	for _, imp := range std {
		sb.WriteString("\t")
		sb.WriteString(spec(imp))
		sb.WriteString("\n")
	}
	if len(std) > 0 && len(other) > 0 {
		sb.WriteString("\n")
	}
	for _, imp := range other {
		sb.WriteString("\t")
		sb.WriteString(spec(imp))
		sb.WriteString("\n")
	}
	sb.WriteString(")")

	return sb.String()
}
