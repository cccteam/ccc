// config.go reads the config package: the level structs, the fields under each with
// their env tags, the level constructors, and the auth's directory literal that says
// which field feeds which part of the registration.

package derive

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// The levels, lowest first, with what each says about its readers. The names are the
// skeleton's; the struct of level L is <L>Config and its constructor new<L>Configuration.
var levelNames = []struct{ name, description string }{
	{LevelCore, "every process"},
	{LevelData, "every process that opens the database"},
	{LevelSite, "the served site"},
}

// config is what the config package declares.
type config struct {
	dir string
	pkg string
	// levels are the levels found, in order.
	levels []Level
	// variables are the env-tagged fields placed at a level, in the reader's order.
	variables []Variable
	// directory maps a directory role to the config field the literal feeds it from.
	directory map[Role]string
	// image are the variables the Dockerfile sets.
	image map[string]bool
	// funcs are the package's top-level function names.
	funcs map[string]bool
	// structs are the package's struct types by name.
	structs map[string]*structDecl
	// envTemplate maps a variable to the value the development template sets, when the
	// template sets one.
	envTemplate map[string]string
}

// structDecl is one struct type with its fields.
type structDecl struct {
	file   string
	fields []fieldDecl
}

// fieldDecl is one field of a struct.
type fieldDecl struct {
	name     string
	typeName string
	line     int
	doc      string
	// secret is the secret tag's value as written ("true", "false"), or empty.
	secret string
	// tag is the env tag's value, or empty.
	tag string
}

// readConfig parses the package holding the env tags.
func readConfig(a *app.App) (*config, error) {
	if len(a.EnvTags) == 0 {
		return nil, errors.New("the application declares no env tags: nothing to configure")
	}
	dir := path.Dir(a.EnvTags[0].File)
	for _, t := range a.EnvTags {
		if path.Dir(t.File) != dir {
			return nil, errors.Newf("env tags in more than one package (%s and %s); the config package must hold them all", dir, path.Dir(t.File))
		}
	}

	cfg := &config{dir: dir, funcs: map[string]bool{}, structs: map[string]*structDecl{}, directory: map[Role]string{}}
	authLocals := authImportNames(a)
	for _, rel := range a.GoFiles() {
		if path.Dir(rel) != dir {
			continue
		}
		if err := cfg.readFile(a, rel, authLocals); err != nil {
			return nil, err
		}
	}
	if err := cfg.placeLevels(); err != nil {
		return nil, err
	}
	if err := cfg.placeVariables(a.EnvTags); err != nil {
		return nil, err
	}
	image, err := dockerfileEnv(a)
	if err != nil {
		return nil, err
	}
	if image == nil {
		image = cfg.seededImage()
	}
	cfg.image = image
	cfg.envTemplate, err = envTemplateValues(a)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// authImportNames maps each auth package's import path to the set of auths, so a file
// importing one can be read for its Directory literal.
func authImportNames(a *app.App) map[string]string {
	paths := map[string]string{}
	for _, p := range a.AuthPackages {
		paths[p.Path] = p.Name
	}

	return paths
}

// readFile records one file's structs, functions, and directory literal.
func (c *config) readFile(a *app.App, rel string, authPaths map[string]string) error {
	src, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return errors.Wrap(err, "parser.ParseFile()")
	}
	c.pkg = f.Name.Name

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				c.funcs[d.Name.Name] = true
			}
		case *ast.GenDecl:
			c.readStructs(fset, rel, d)
		}
	}

	locals := map[string]bool{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if _, isAuth := authPaths[p]; isAuth {
			local := path.Base(p)
			if imp.Name != nil {
				local = imp.Name.Name
			}
			locals[local] = true
		}
	}
	if len(locals) > 0 {
		c.readDirectory(f, locals)
	}

	return nil
}

// readStructs records the struct types a type declaration declares.
func (c *config) readStructs(fset *token.FileSet, rel string, d *ast.GenDecl) {
	if d.Tok != token.TYPE {
		return
	}
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}
		decl := &structDecl{file: rel}
		for _, field := range st.Fields.List {
			fd := fieldDecl{typeName: typeString(field.Type), line: fset.Position(field.Pos()).Line}
			if field.Doc != nil {
				fd.doc = strings.TrimSpace(field.Doc.Text())
			}
			if field.Tag != nil {
				tags := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
				fd.tag, _ = tags.Lookup("env")
				fd.secret, _ = tags.Lookup(secretTag)
			}
			if len(field.Names) == 0 {
				fd.name = path.Base(fd.typeName)
				decl.fields = append(decl.fields, fd)

				continue
			}
			for _, name := range field.Names {
				named := fd
				named.name = name.Name
				decl.fields = append(decl.fields, named)
			}
		}
		c.structs[ts.Name.Name] = decl
	}
}

// typeString writes a field's type the way the source does, for the simple shapes the
// config package uses.
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.ArrayType:
		return "[]" + typeString(t.Elt)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	default:
		return ""
	}
}

// readDirectory records the auth's Directory literal: each key it assigns from a field of
// the environment (env.<Field>) feeds the role the key names.
func (c *config) readDirectory(f *ast.File, authLocals map[string]bool) {
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Directory" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || !authLocals[pkg.Name] {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			value, ok := kv.Value.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if role, known := directoryRoles[key.Name]; known {
				c.directory[role] = value.Sel.Name
			}
		}

		return true
	})
}

// placeLevels finds the level structs and their constructors.
func (c *config) placeLevels() error {
	for _, l := range levelNames {
		name, decl := c.structNamed(l.name + "config")
		if decl == nil {
			return errors.Newf("the config package declares no %sConfig struct", l.name)
		}
		level := Level{Name: l.name, File: decl.file, Struct: name, Description: l.description}
		for fn := range c.funcs {
			if strings.EqualFold(fn, "new"+l.name+"configuration") {
				level.Constructor = fn
			}
		}
		c.levels = append(c.levels, level)
	}

	return nil
}

// structNamed finds a struct by name, ignoring case.
func (c *config) structNamed(name string) (string, *structDecl) {
	for n, decl := range c.structs {
		if strings.EqualFold(n, name) {
			return n, decl
		}
	}

	return "", nil
}

// placeVariables gives every env tag the reader found its level, struct, and field. A
// level's variables are the tagged fields of its struct and of the structs its untagged
// fields are typed as.
func (c *config) placeVariables(tags []app.EnvTag) error {
	located := map[string]Variable{}
	for _, level := range c.levels {
		c.collect(level.Name, level.Struct, located)
	}
	for _, t := range tags {
		v, ok := located[t.File+":"+strconv.Itoa(t.Line)]
		if !ok {
			return errors.Newf("%s:%d: %s is declared outside every configuration level", t.File, t.Line, t.Name)
		}
		v.Name, v.Required, v.HasDefault = t.Name, t.Required, t.HasDefault
		c.variables = append(c.variables, v)
	}

	return nil
}

// collect records the tagged fields of the struct and of the structs it nests, keyed by
// file and line.
func (c *config) collect(level, structName string, located map[string]Variable) {
	decl := c.structs[structName]
	if decl == nil {
		return
	}
	for _, f := range decl.fields {
		if f.tag != "" {
			v := Variable{Level: level, Struct: structName, Field: f.name, File: decl.file, Line: f.line, Type: f.typeName, Doc: f.doc, SecretTag: f.secret}
			v.Default, v.HasDefault = tagDefault(f.tag)
			located[decl.file+":"+strconv.Itoa(f.line)] = v

			continue
		}
		if nested := strings.TrimPrefix(f.typeName, "*"); c.structs[nested] != nil {
			c.collect(level, nested, located)
		}
	}
}

// tagDefault reads the default option of an env tag.
func tagDefault(tag string) (string, bool) {
	for _, opt := range strings.Split(tag, ",")[1:] {
		if value, ok := strings.CutPrefix(strings.TrimSpace(opt), "default="); ok {
			return value, true
		}
	}

	return "", false
}

// seededImage is what the Dockerfile bedrock seeds sets, for an application that has
// none yet, so the first render and the next agree: the version variable, and every
// variable whose default names a bundle of the browser workspace (<web>/dist/<name>).
func (c *config) seededImage() map[string]bool {
	image := map[string]bool{}
	for i := range c.variables {
		v := &c.variables[i]
		if v.Name == varVersion || (v.HasDefault && BundleRE.MatchString(v.Default)) {
			image[v.Name] = true
		}
	}

	return image
}
