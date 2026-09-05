package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"

	"github.com/go-playground/errors/v5"
)

// generationImportPath is the import path of the resource generator package whose
// option calls make up a generator program.
const generationImportPath = "github.com/cccteam/ccc/resource/generation"

// Generator is one resource generator program: a call to
// generation.NewResourceGenerator with its positional arguments and option calls.
type Generator struct {
	// File is the root-relative path of the Go file holding the program.
	File string
	// ResourcePackageDir is the first positional argument: the resource package directory.
	ResourcePackageDir string
	// MigrationSources is the second positional argument: the migration source URLs.
	MigrationSources []string
	// LocalPackages is the third positional argument: the packages the generator loads.
	LocalPackages []string
	// Options are the option calls passed after the positional arguments, in order.
	Options []Call
	// Problems are the places the program could not be read as literal configuration:
	// unknown options, non-literal arguments, wrong arities.
	Problems []Problem
}

// Call is one option call such as generation.GenerateHandlers("app").
type Call struct {
	// Name is the option constructor's name, without the package qualifier.
	Name string
	// Pos is the call's position as file:line.
	Pos string
	// Args are the call's arguments in order.
	Args []Arg
}

// ArgKind classifies an option argument.
type ArgKind int

// The argument kinds a generator program may use. Anything else is ArgOther, which the
// tool cannot interpret and reports as a problem.
const (
	ArgOther ArgKind = iota
	ArgString
	ArgBool
	ArgStringList
	ArgStringMap
	ArgBoolMap
	ArgCall
	ArgComposite
)

// Arg is one literal argument of an option call.
type Arg struct {
	Kind ArgKind
	Str  string
	Bool bool
	List []string
	// Map holds string- and bool-valued maps; bool values are rendered as "true"/"false".
	Map map[string]string
	// Call is set for ArgCall: a nested option such as generation.GenerateEnums().
	Call *Call
	// Text is the source text of an argument the tool cannot interpret.
	Text string
}

// Problem is one place a generator program departs from literal configuration.
type Problem struct {
	Pos     string
	Message string
}

func (p Problem) String() string {
	return p.Pos + ": " + p.Message
}

// String renders a call as source-like text for messages.
func (c Call) String() string {
	return c.Name + "(...)"
}

// Option returns the first option call with the name, or false.
func (g *Generator) Option(name string) (Call, bool) {
	for _, c := range g.Options {
		if c.Name == name {
			return c, true
		}
	}

	return Call{}, false
}

// OptionsNamed returns every option call with the name, in order.
func (g *Generator) OptionsNamed(name string) []Call {
	var calls []Call
	for _, c := range g.Options {
		if c.Name == name {
			calls = append(calls, c)
		}
	}

	return calls
}

// firstString returns the first argument of the named option when it is a string.
func (g *Generator) firstString(name string) string {
	c, ok := g.Option(name)
	if !ok || len(c.Args) == 0 || c.Args[0].Kind != ArgString {
		return ""
	}

	return path.Clean(c.Args[0].Str)
}

// HandlersDir is the GenerateHandlers target directory, or empty when the generator
// emits no handlers (a shared generator).
func (g *Generator) HandlersDir() string { return g.firstString("GenerateHandlers") }

// RoutesDir is the GenerateRoutes target directory, or empty.
func (g *Generator) RoutesDir() string { return g.firstString("GenerateRoutes") }

// HandlerTestsDir is the GenerateHandlerTests target directory, or empty.
func (g *Generator) HandlerTestsDir() string { return g.firstString("GenerateHandlerTests") }

// RPCDir is the WithRPC package directory, or empty.
func (g *Generator) RPCDir() string { return g.firstString("WithRPC") }

// EmulatorVersion is the WithSpannerEmulatorVersion argument, or empty.
func (g *Generator) EmulatorVersion() string {
	c, ok := g.Option("WithSpannerEmulatorVersion")
	if !ok || len(c.Args) == 0 || c.Args[0].Kind != ArgString {
		return ""
	}

	return c.Args[0].Str
}

// TSTarget is one GenerateTypescript target.
type TSTarget struct {
	// Dir is the root-relative target directory.
	Dir string
	// Outlet is the ForOutlet name, or empty for the default outlet.
	Outlet string
	// Pos is the position of the GenerateTypescript call.
	Pos string
}

// TypescriptTargets returns every GenerateTypescript target, in order.
func (g *Generator) TypescriptTargets() []TSTarget {
	var targets []TSTarget
	for _, c := range g.OptionsNamed("GenerateTypescript") {
		if len(c.Args) == 0 || c.Args[0].Kind != ArgString {
			continue
		}
		t := TSTarget{Dir: path.Clean(c.Args[0].Str), Pos: c.Pos}
		for _, a := range c.Args[1:] {
			if a.Kind == ArgCall && a.Call.Name == "ForOutlet" && len(a.Call.Args) == 1 && a.Call.Args[0].Kind == ArgString {
				t.Outlet = a.Call.Args[0].Str
			}
		}
		targets = append(targets, t)
	}

	return targets
}

// parseGenerator reads one Go source file and returns the generator program it holds, or
// nil when the file makes no generation.NewResourceGenerator call.
func parseGenerator(rel string, src []byte) (*Generator, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	pkgName := generationLocalName(f)
	if pkgName == "" {
		return nil, nil
	}

	var call *ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if call != nil {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok && isQualified(c.Fun, pkgName, "NewResourceGenerator") {
			call = c

			return false
		}

		return true
	})
	if call == nil {
		return nil, nil
	}

	r := &reader{fset: fset, pkg: pkgName, g: &Generator{File: rel}}
	r.readProgram(call)

	return r.g, nil
}

// generationLocalName returns the local name the file imports the generation package
// under, or empty when the file does not import it.
func generationLocalName(f *ast.File) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != generationImportPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}

		return path.Base(p)
	}

	return ""
}

// isQualified reports whether fun is the selector pkg.name.
func isQualified(fun ast.Expr, pkg, name string) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)

	return ok && id.Name == pkg
}

// reader walks one NewResourceGenerator call and fills a Generator.
type reader struct {
	fset *token.FileSet
	pkg  string
	g    *Generator
}

func (r *reader) pos(n ast.Node) string {
	p := r.fset.Position(n.Pos())

	return fmt.Sprintf("%s:%d", p.Filename, p.Line)
}

func (r *reader) problemf(n ast.Node, format string, args ...any) {
	r.g.Problems = append(r.g.Problems, Problem{Pos: r.pos(n), Message: fmt.Sprintf(format, args...)})
}

// positionalArgs is the number of arguments NewResourceGenerator takes before the options:
// ctx, resourcePackageDir, migrationSourceURL, localPackages.
const positionalArgs = 4

func (r *reader) readProgram(call *ast.CallExpr) {
	if len(call.Args) < positionalArgs {
		r.problemf(call, "NewResourceGenerator needs %d positional arguments, found %d", positionalArgs, len(call.Args))

		return
	}

	if s, ok := stringLit(call.Args[1]); ok {
		r.g.ResourcePackageDir = path.Clean(s)
	} else {
		r.problemf(call.Args[1], "resource package directory is not a string literal")
	}
	if l, ok := stringList(call.Args[2]); ok {
		r.g.MigrationSources = l
	} else {
		r.problemf(call.Args[2], "migration sources are not a []string literal")
	}
	if l, ok := stringList(call.Args[3]); ok {
		r.g.LocalPackages = l
	} else {
		r.problemf(call.Args[3], "local packages are not a []string literal")
	}

	for _, arg := range call.Args[positionalArgs:] {
		c, ok := r.readCall(arg, kindResourceOption)
		if !ok {
			continue
		}
		r.g.Options = append(r.g.Options, c)
	}
}

// readCall reads one option call, validating it against the known option set for the
// expected option kind. It returns false when the argument is not an option call at all.
func (r *reader) readCall(expr ast.Expr, want optionKind) (Call, bool) {
	ce, ok := expr.(*ast.CallExpr)
	if !ok {
		r.problemf(expr, "expected a %s call, found %s", want, exprText(expr))

		return Call{}, false
	}
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok || !isQualified(ce.Fun, r.pkg, sel.Sel.Name) {
		r.problemf(expr, "expected a %s.<Option>() call, found %s", r.pkg, exprText(expr))

		return Call{}, false
	}

	c := Call{Name: sel.Sel.Name, Pos: r.pos(ce)}
	spec, known := knownOptions[c.Name]
	if !known {
		r.problemf(ce, "unknown option %s.%s: this impulse release does not know it", r.pkg, c.Name)
	} else if spec.kind != want {
		r.problemf(ce, "%s is a %s, but a %s is expected here", c.Name, spec.kind, want)
	}

	for i, a := range ce.Args {
		var expect paramKind
		switch {
		case !known:
			expect = paramAny
		case i < len(spec.params):
			expect = spec.params[i]
		case spec.variadic != paramNone:
			expect = spec.variadic
		default:
			r.problemf(a, "%s takes %d argument(s), found %d", c.Name, len(spec.params), len(ce.Args))
			expect = paramAny
		}
		c.Args = append(c.Args, r.readArg(a, c.Name, expect))
	}
	if known && len(ce.Args) < len(spec.params) {
		r.problemf(ce, "%s takes %d argument(s), found %d", c.Name, len(spec.params), len(ce.Args))
	}

	return c, true
}

// readArg reads one argument, checking it against the expected parameter kind.
func (r *reader) readArg(expr ast.Expr, option string, expect paramKind) Arg {
	switch expect {
	case paramTSOption, paramOutletOption:
		c, ok := r.readCall(expr, expect.optionKind())
		if !ok {
			return Arg{Kind: ArgOther, Text: exprText(expr)}
		}

		return Arg{Kind: ArgCall, Call: &c}
	case paramComposite:
		if _, ok := expr.(*ast.CompositeLit); ok {
			return Arg{Kind: ArgComposite, Text: exprText(expr)}
		}
	case paramString, paramBool, paramStringMap, paramBoolMap, paramAny, paramNone:
	}

	arg := literalArg(expr)
	if arg.Kind == ArgOther {
		r.problemf(expr, "%s argument %s is not a literal", option, arg.Text)

		return arg
	}
	if expect != paramAny && expect.argKind() != arg.Kind {
		r.problemf(expr, "%s argument %s should be a %s", option, exprText(expr), expect)
	}

	return arg
}

// literalArg interprets a literal expression.
func literalArg(expr ast.Expr) Arg {
	if s, ok := stringLit(expr); ok {
		return Arg{Kind: ArgString, Str: s}
	}
	if b, ok := boolLit(expr); ok {
		return Arg{Kind: ArgBool, Bool: b}
	}
	if l, ok := stringList(expr); ok {
		return Arg{Kind: ArgStringList, List: l}
	}
	if m, kind, ok := mapLit(expr); ok {
		return Arg{Kind: kind, Map: m}
	}

	return Arg{Kind: ArgOther, Text: exprText(expr)}
}

func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}

	return s, true
}

// boolLit reads the predeclared identifiers true and false.
func boolLit(expr ast.Expr) (value, ok bool) {
	id, isIdent := expr.(*ast.Ident)
	if !isIdent {
		return false, false
	}
	switch id.Name {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func stringList(expr ast.Expr) ([]string, bool) {
	cl, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	if _, isArray := cl.Type.(*ast.ArrayType); !isArray {
		return nil, false
	}
	list := make([]string, 0, len(cl.Elts))
	for _, e := range cl.Elts {
		s, ok := stringLit(e)
		if !ok {
			return nil, false
		}
		list = append(list, s)
	}

	return list, true
}

// mapLit reads a map[string]string or map[string]bool literal.
func mapLit(expr ast.Expr) (map[string]string, ArgKind, bool) {
	cl, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, ArgOther, false
	}
	if _, isMap := cl.Type.(*ast.MapType); !isMap {
		return nil, ArgOther, false
	}
	m := make(map[string]string, len(cl.Elts))
	kind := ArgStringMap
	for _, e := range cl.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return nil, ArgOther, false
		}
		k, ok := stringLit(kv.Key)
		if !ok {
			return nil, ArgOther, false
		}
		if s, ok := stringLit(kv.Value); ok {
			m[k] = s

			continue
		}
		if b, ok := boolLit(kv.Value); ok {
			m[k] = strconv.FormatBool(b)
			kind = ArgBoolMap

			continue
		}

		return nil, ArgOther, false
	}

	return m, kind, true
}

// exprText renders an expression compactly for messages.
func exprText(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.BasicLit:
		return e.Value
	case *ast.SelectorExpr:
		return exprText(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return exprText(e.Fun) + "(...)"
	case *ast.CompositeLit:
		if e.Type != nil {
			return exprText(e.Type) + "{...}"
		}

		return "{...}"
	case *ast.ArrayType:
		return "[]" + exprText(e.Elt)
	case *ast.MapType:
		return "map[" + exprText(e.Key) + "]" + exprText(e.Value)
	default:
		return fmt.Sprintf("%T", expr)
	}
}
