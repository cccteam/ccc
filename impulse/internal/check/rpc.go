package check

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
)

// rpcExecute verifies the agreement between an application's RPC methods and the
// handlers generated for them. Every @rpc struct declares Execute in one of the two
// forms the generator classifies by signature: the transaction form takes
// resource.ReadWriteTransaction second, the client form *resource.Client. Every
// generated RPC handler calls Execute: a generator that could not type-check a
// method once emitted a decode-only handler that answers requests without running
// it, a fail-open bug that compiles cleanly. And no TxnRunner or DBRunner interface
// stands in for the classification any more: the generator stopped consulting them
// when it began reading the signature, so one left behind is dead code.
type rpcExecute struct{}

func (rpcExecute) Name() string { return "rpc-execute" }

func (rpcExecute) Describe() string {
	return "every @rpc struct declares an Execute the generator classifies, and every generated RPC handler calls it"
}

func (c rpcExecute) Run(_ context.Context, env *Env) Result {
	a := env.App
	var details, cleanups []string
	checked, headerless, methods := 0, 0, 0

	for _, g := range a.Generators {
		handlers, rpcDir := g.HandlersDir(), g.RPCDir()
		if handlers == "" || rpcDir == "" {
			continue
		}
		files, err := filepath.Glob(filepath.Join(a.Abs(handlers), "zz_gen_*.go"))
		if err != nil {
			return fail(c.Name(), errors.Wrap(err, "filepath.Glob()").Error())
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return fail(c.Name(), errors.Wrap(err, "os.ReadFile()").Error())
			}
			source := sourceOf(data)
			if source == "" {
				headerless++

				continue
			}
			if source != rpcDir {
				continue
			}
			checked++
			if !strings.Contains(string(data), ".Execute(") {
				details = append(details, a.Rel(f)+": no Execute call; the handler decodes and returns without running the method")
			}
		}

		pkg, err := readRPCPackage(a.Abs(rpcDir), a.Rel)
		if err != nil {
			return fail(c.Name(), err.Error())
		}
		methods += len(pkg.methods)
		details = append(details, pkg.findings()...)
		cleanups = append(cleanups, pkg.legacy...)
	}

	if checked == 0 && headerless > 0 {
		return warn(c.Name(), fmt.Sprintf("cannot identify RPC handlers: %d zz_gen file(s) in the handlers directory carry no Source header, so the resource generator did not write them", headerless))
	}
	if checked == 0 && methods == 0 {
		return skip(c.Name(), "no generated RPC handlers")
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d RPC finding(s) (regenerate and read the generator output)", len(details)), append(details, cleanups...)...)
	}
	summary := fmt.Sprintf("%d RPC method(s) declare a recognized Execute and %d generated handler(s) call it", methods, checked)
	if len(cleanups) > 0 {
		return warn(c.Name(), summary+"; an interface the generator no longer consults remains", cleanups...)
	}

	return pass(c.Name(), summary)
}

// rpcPackage is what the check reads from the RPC package's hand-written files:
// the @rpc structs, the Execute methods by receiver, and interfaces left over
// from before the generator classified by signature.
type rpcPackage struct {
	methods []rpcStruct
	execute map[string]executeDecl
	legacy  []string
}

type rpcStruct struct {
	name string
	pos  string
}

type executeDecl struct {
	pos     string
	params  []string
	results []string
}

// legacyInterfaces are the interface names the generator once matched RPC
// structs against. It reads the Execute signature now.
var legacyInterfaces = map[string]bool{"TxnRunner": true, "DBRunner": true}

// readRPCPackage parses the hand-written Go files of the RPC package directory:
// generated files carry the generator's own output and test files are not
// declarations. The read is syntactic, the way the tool reads everything: the
// generator's type-checked classification is the authority, and this check is
// the floor under an application pinned to an older generator.
func readRPCPackage(dir string, rel func(string) string) (*rpcPackage, error) {
	pkg := &rpcPackage{execute: make(map[string]executeDecl)}
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Glob()")
	}
	sort.Strings(files)
	fset := token.NewFileSet()
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasPrefix(base, "zz_gen_") || strings.HasSuffix(base, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		file, err := parser.ParseFile(fset, f, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.Wrap(err, "parser.ParseFile()")
		}
		at := func(p token.Pos) string {
			position := fset.Position(p)

			return fmt.Sprintf("%s:%d", rel(position.Filename), position.Line)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				pkg.readTypes(d, at)
			case *ast.FuncDecl:
				pkg.readExecute(d, at)
			}
		}
	}

	return pkg, nil
}

func (p *rpcPackage) readTypes(d *ast.GenDecl, at func(token.Pos) string) {
	if d.Tok != token.TYPE {
		return
	}
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		switch ts.Type.(type) {
		case *ast.StructType:
			doc := ts.Doc
			if doc == nil && len(d.Specs) == 1 {
				doc = d.Doc
			}
			if hasAnnotation(doc, "@rpc") {
				p.methods = append(p.methods, rpcStruct{name: ts.Name.Name, pos: at(ts.Pos())})
			}
		case *ast.InterfaceType:
			if legacyInterfaces[ts.Name.Name] {
				p.legacy = append(p.legacy, fmt.Sprintf("%s: interface %s is no longer consulted; the generator classifies RPC methods by the Execute signature, so delete it", at(ts.Pos()), ts.Name.Name))
			}
		}
	}
}

func (p *rpcPackage) readExecute(d *ast.FuncDecl, at func(token.Pos) string) {
	if d.Recv == nil || d.Name.Name != "Execute" || len(d.Recv.List) != 1 {
		return
	}
	recv := d.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	id, ok := recv.(*ast.Ident)
	if !ok {
		return
	}
	p.execute[id.Name] = executeDecl{pos: at(d.Pos()), params: fieldTypes(d.Type.Params), results: fieldTypes(d.Type.Results)}
}

// findings lists every @rpc struct whose Execute is missing or of a shape the
// generator does not classify.
func (p *rpcPackage) findings() []string {
	var out []string
	for _, m := range p.methods {
		exec, ok := p.execute[m.name]
		if !ok {
			out = append(out, fmt.Sprintf("%s: %s declares @rpc but no Execute method; the generator refuses it", m.pos, m.name))

			continue
		}
		if !exec.recognized() {
			out = append(out, fmt.Sprintf("%s: %s.Execute(%s) (%s) is neither form the generator classifies: Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error or Execute(ctx context.Context, client *resource.Client, rpcClient *Client) error", exec.pos, m.name, strings.Join(exec.params, ", "), strings.Join(exec.results, ", ")))
		}
	}

	return out
}

// recognized reports whether the declaration has the shape of either form: three
// parameters with context first and a transaction or resource client second, a
// pointer third, and error as the only or last result.
func (e executeDecl) recognized() bool {
	if len(e.params) != 3 || e.params[0] != "context.Context" {
		return false
	}
	if e.params[1] != "resource.ReadWriteTransaction" && e.params[1] != "*resource.Client" {
		return false
	}
	if !strings.HasPrefix(e.params[2], "*") {
		return false
	}

	return (len(e.results) == 1 || len(e.results) == 2) && e.results[len(e.results)-1] == "error"
}

// fieldTypes renders a parameter or result list's types in declaration order,
// one entry per name (or per unnamed field).
func fieldTypes(list *ast.FieldList) []string {
	if list == nil {
		return nil
	}
	var out []string
	for _, f := range list.List {
		text := exprText(f.Type)
		n := max(len(f.Names), 1)
		for range n {
			out = append(out, text)
		}
	}

	return out
}

func exprText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprText(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return "*" + exprText(x.X)
	case *ast.Ellipsis:
		return "..." + exprText(x.Elt)
	case *ast.ArrayType:
		return "[]" + exprText(x.Elt)
	default:
		return fmt.Sprintf("%T", e)
	}
}

// hasAnnotation reports whether a doc comment carries the annotation on a line of
// its own, the way the generator's scanner reads struct annotations.
func hasAnnotation(doc *ast.CommentGroup, annotation string) bool {
	if doc == nil {
		return false
	}
	for line := range strings.Lines(doc.Text()) {
		if strings.HasPrefix(strings.TrimSpace(line), annotation) {
			return true
		}
	}

	return false
}

// sourceOf returns the directory named by the generated file's "// Source:" header line,
// cleaned, or empty.
func sourceOf(data []byte) string {
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if src, ok := strings.CutPrefix(line, "// Source:"); ok {
			return path.Clean(strings.TrimSpace(src))
		}
		if line != "" && !strings.HasPrefix(line, "//") {
			return ""
		}
	}

	return ""
}
