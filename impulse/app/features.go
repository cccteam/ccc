package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
)

// FeatureFlag is one feature flag declaration: a constant of type resource.Feature in a
// resources package, whose identifier @feature names, whose value is the flag's name as
// the FeatureFlags table holds it, and whose doc comment is the description the flags
// dialog shows.
type FeatureFlag struct {
	File string
	Line int
	// Constant is the constant's identifier: what @feature(<Constant>) names, and the key
	// of the browser's generated Feature constants (Feature.<Constant>).
	Constant string
	// Name is the constant's value, the flag's name.
	Name string
	// Description is the constant's doc comment on one line, as the generator reads it.
	Description string
	// Package is the import path of the package declaring the constant.
	Package string
}

// FeatureGate is one @feature(<Constant>) annotation: a resource, method or field put
// behind a flag.
type FeatureGate struct {
	File string
	Line int
	// Constant is the identifier the annotation names.
	Constant string
	// Target is what the annotation gates: the struct's name, or Struct.Field for a
	// field.
	Target string
}

// FeatureUse is one hand-written reference to a flag's constant outside its declaration
// and its gates: Go code reading the flag (resources.Debriefs) or browser code reading
// the generated constant (Feature.Debriefs).
type FeatureUse struct {
	File string
	Line int
	// Constant is the flag's constant.
	Constant string
	// Test reports a reference in a Go test file or a browser spec.
	Test bool
}

// The resource package's import path and the type of a flag constant, and the keyword of
// the gate annotation.
const (
	resourceImportPath = "github.com/cccteam/ccc/resource"
	featureTypeName    = "Feature"
	featureKeyword     = "feature"
)

// FeatureNameRE is the shape of a flag's name, as the resource package validates it:
// lowercase letters, digits and underscores, opening with a letter, at most 64
// characters.
var FeatureNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// featureGateRE captures the identifier an @feature(...) annotation names.
var featureGateRE = regexp.MustCompile(`@` + featureKeyword + `\(\s*([^)]*?)\s*\)`)

// parseFeatureFlags returns every constant of type resource.Feature the file declares.
func parseFeatureFlags(rel string, src []byte, pkgPath string) ([]FeatureFlag, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := localImportName(f, resourceImportPath)
	if pkg == "" {
		return nil, nil
	}
	var flags []FeatureFlag
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs.Type == nil || !isQualified(vs.Type, pkg, featureTypeName) {
				continue
			}
			doc := vs.Doc
			if doc == nil && len(gen.Specs) == 1 {
				doc = gen.Doc
			}
			for i, name := range vs.Names {
				flag := FeatureFlag{File: rel, Line: fset.Position(name.Pos()).Line, Constant: name.Name, Package: pkgPath}
				if i < len(vs.Values) {
					flag.Name, _ = stringLit(vs.Values[i])
				}
				if doc != nil {
					flag.Description = strings.Join(strings.Fields(doc.Text()), " ")
				}
				flags = append(flags, flag)
			}
		}
	}

	return flags, nil
}

// parseFeatureGates returns every @feature annotation in the file: on a struct's doc
// comment, and on a field's doc or line comment.
func parseFeatureGates(rel string, src []byte) ([]FeatureGate, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	var gates []FeatureGate
	add := func(doc *ast.CommentGroup, target string) {
		if doc == nil {
			return
		}
		for _, c := range doc.List {
			for _, m := range featureGateRE.FindAllStringSubmatch(c.Text, -1) {
				gates = append(gates, FeatureGate{File: rel, Line: fset.Position(c.Pos()).Line, Constant: m[1], Target: target})
			}
		}
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, isStruct := ts.Type.(*ast.StructType)
			if !isStruct {
				continue
			}
			doc := ts.Doc
			if doc == nil && len(gen.Specs) == 1 {
				doc = gen.Doc
			}
			add(doc, ts.Name.Name)
			for _, field := range st.Fields.List {
				name := ""
				if len(field.Names) > 0 {
					name = field.Names[0].Name
				}
				add(field.Doc, ts.Name.Name+"."+name)
				add(field.Comment, ts.Name.Name+"."+name)
			}
		}
	}

	return gates, nil
}

// FeatureUses finds every hand-written reference to a declared flag's constant: in Go,
// the identifier in the declaring package or the qualified selector from another package,
// outside the declaration itself (an annotation is a comment and never an identifier);
// in the browser applications, Feature.<Constant> in TypeScript and templates. Generated
// files and build products are not read. The tree is opened as a root, so the walk stays
// inside it.
func (a *App) FeatureUses() ([]FeatureUse, error) {
	if len(a.Features) == 0 {
		return nil, nil
	}
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()

	var uses []FeatureUse
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "fs.WalkDir()")
		}
		if d.IsDir() {
			if p != "." && skippedDirs[d.Name()] {
				return fs.SkipDir
			}

			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "zz_gen_") {
			return nil
		}
		switch {
		case strings.HasSuffix(name, ".go"):
			data, err := root.ReadFile(filepath.FromSlash(p))
			if err != nil {
				return errors.Wrap(err, "os.Root.ReadFile()")
			}
			found, err := a.goFeatureUses(p, data)
			if err != nil {
				return err
			}
			uses = append(uses, found...)
		case webSourceExt[path.Ext(name)]:
			if _, inWeb := a.WebAppFor(p); !inWeb {
				return nil
			}
			data, err := root.ReadFile(filepath.FromSlash(p))
			if err != nil {
				return errors.Wrap(err, "os.Root.ReadFile()")
			}
			uses = append(uses, a.webFeatureUses(p, data)...)
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "fs.WalkDir()")
	}
	sort.Slice(uses, func(i, j int) bool {
		if uses[i].File != uses[j].File {
			return uses[i].File < uses[j].File
		}

		return uses[i].Line < uses[j].Line
	})

	return uses, nil
}

// webSourceExt are the browser source files a flag's constant is read in.
var webSourceExt = map[string]bool{".ts": true, ".html": true}

// isWebSpec reports a browser spec file.
func isWebSpec(name string) bool {
	return strings.HasSuffix(name, ".spec.ts") || strings.HasSuffix(name, ".test.ts")
}

// goFeatureUses finds the flags' constants referenced in one Go file: bare identifiers
// in the declaring package, qualified selectors elsewhere.
func (a *App) goFeatureUses(rel string, src []byte) ([]FeatureUse, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	own := a.packagePath(path.Dir(rel))
	imports := fileImports(f)
	test := strings.HasSuffix(rel, "_test.go")
	byConstant := map[string]FeatureFlag{}
	for _, flag := range a.Features {
		byConstant[flag.Constant] = flag
	}
	// declared are the identifiers a constant declaration names: the declaration itself
	// is not a use, while a value it takes from another flag is.
	declared := map[*ast.Ident]bool{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				for _, name := range vs.Names {
					declared[name] = true
				}
			}
		}
	}

	var uses []FeatureUse
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if use, ok := qualifiedUse(x, imports, byConstant, rel, fset, test); ok {
				uses = append(uses, use)

				return false
			}
		case *ast.Ident:
			flag, ok := byConstant[x.Name]
			if !ok || declared[x] || flag.Package == "" || flag.Package != own {
				return true
			}
			uses = append(uses, FeatureUse{File: rel, Line: fset.Position(x.Pos()).Line, Constant: x.Name, Test: test})
		}

		return true
	})

	return uses, nil
}

// qualifiedUse reads pkg.Constant, where pkg imports the declaring package.
func qualifiedUse(sel *ast.SelectorExpr, imports map[string]string, byConstant map[string]FeatureFlag, rel string, fset *token.FileSet, test bool) (FeatureUse, bool) {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return FeatureUse{}, false
	}
	flag, ok := byConstant[sel.Sel.Name]
	if !ok || imports[id.Name] != flag.Package {
		return FeatureUse{}, false
	}

	return FeatureUse{File: rel, Line: fset.Position(sel.Pos()).Line, Constant: sel.Sel.Name, Test: test}, true
}

// webFeatureUses finds Feature.<Constant> in one browser source file.
func (a *App) webFeatureUses(rel string, src []byte) []FeatureUse {
	var uses []FeatureUse
	test := isWebSpec(path.Base(rel))
	for i, line := range strings.Split(string(src), "\n") {
		for _, flag := range a.Features {
			if strings.Contains(line, featureTypeName+"."+flag.Constant) && webUseRE(flag.Constant).MatchString(line) {
				uses = append(uses, FeatureUse{File: rel, Line: i + 1, Constant: flag.Constant, Test: test})
			}
		}
	}

	return uses
}

// webUseRE matches the generated constant for one flag as a whole word.
func webUseRE(constant string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + featureTypeName + `\.` + regexp.QuoteMeta(constant) + `\b`)
}

// FeatureConstant is the Go identifier a flag of the name takes when impulse declares
// it: the name in PascalCase (cargo_manifest becomes CargoManifest).
func FeatureConstant(name string) string {
	var b strings.Builder
	for _, part := range strings.Split(name, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}

	return b.String()
}

// FeatureByName finds the declarations of the flag named name.
func (a *App) FeatureByName(name string) []FeatureFlag {
	var flags []FeatureFlag
	for _, flag := range a.Features {
		if flag.Name == name {
			flags = append(flags, flag)
		}
	}

	return flags
}

// GatesOf lists the gates naming the constant.
func (a *App) GatesOf(constant string) []FeatureGate {
	var gates []FeatureGate
	for _, g := range a.FeatureGates {
		if g.Constant == constant {
			gates = append(gates, g)
		}
	}

	return gates
}
