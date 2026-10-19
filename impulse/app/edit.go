package app

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// generationPackage is the name option text is written against; InsertOptions adjusts it
// to the file's own import name.
const generationPackage = "generation."

// InsertOptions returns the program's source with option calls added after the last
// option named after, or after the last option when after is empty. Each option is
// written as source text against the generation package under its own name
// (generation.WithRouterOutlet(...)), and is adjusted to the file's local import name.
// The edit is textual, so the file's comments and layout survive; the result is
// formatted.
func InsertOptions(rel string, src []byte, after string, options []string) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := generationLocalName(f)
	if pkg == "" {
		return nil, errors.Newf("%s does not import %s", rel, generationImportPath)
	}
	call := findGeneratorCall(f, pkg)
	if call == nil {
		return nil, errors.Newf("%s makes no %s.NewResourceGenerator call", rel, pkg)
	}
	if len(call.Args) < positionalArgs {
		return nil, errors.Newf("%s: NewResourceGenerator needs %d positional arguments, found %d", rel, positionalArgs, len(call.Args))
	}

	anchor := call.Args[len(call.Args)-1]
	if after != "" {
		found := false
		for _, a := range call.Args[positionalArgs:] {
			if ce, ok := a.(*ast.CallExpr); ok && isQualified(ce.Fun, pkg, after) {
				anchor, found = a, true
			}
		}
		if !found {
			return nil, errors.Newf("%s: no %s option to insert after", rel, after)
		}
	}

	end := fset.Position(anchor.End()).Offset
	var b bytes.Buffer
	b.Write(src[:end])
	rest := src[end:]
	trimmed := bytes.TrimLeft(rest, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == ',' {
		// One option per line, comma-terminated: the new lines follow the anchor's comma.
		comma := len(rest) - len(trimmed) + 1
		b.Write(rest[:comma])
		rest = rest[comma:]
		for _, opt := range options {
			b.WriteString("\n" + qualify(opt, pkg) + ",")
		}
	} else {
		// The anchor is the last argument without a trailing comma.
		for _, opt := range options {
			b.WriteString(",\n" + qualify(opt, pkg))
		}
	}
	b.Write(rest)

	out, err := format.Source(b.Bytes())
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): %s after inserting options", rel)
	}

	return out, nil
}

// OptionText returns the source text of the last option call named name whose first
// argument is the string literal first, or empty when the program has none.
func OptionText(rel string, src []byte, name, first string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return "", errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := generationLocalName(f)
	call := findGeneratorCall(f, pkg)
	if pkg == "" || call == nil || len(call.Args) < positionalArgs {
		return "", nil
	}
	text := ""
	for _, a := range call.Args[positionalArgs:] {
		ce, ok := a.(*ast.CallExpr)
		if !ok || !isQualified(ce.Fun, pkg, name) || len(ce.Args) == 0 {
			continue
		}
		if s, ok := stringLit(ce.Args[0]); ok && s == first {
			text = string(src[fset.Position(ce.Pos()).Offset:fset.Position(ce.End()).Offset])
		}
	}

	return text, nil
}

// qualify rewrites option text written against "generation." to the file's import name.
func qualify(option, pkg string) string {
	if pkg == strings.TrimSuffix(generationPackage, ".") {
		return option
	}

	return strings.ReplaceAll(option, generationPackage, pkg+".")
}

// findGeneratorCall returns the file's NewResourceGenerator call, or nil.
func findGeneratorCall(f *ast.File, pkg string) *ast.CallExpr {
	var call *ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if call != nil {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok && isQualified(c.Fun, pkg, "NewResourceGenerator") {
			call = c

			return false
		}

		return true
	})

	return call
}

// RemoveOptions returns the program's source with every option call named name whose
// first argument is the string literal first removed, along with the comment lines that
// introduce it, and how many were removed. The edit is textual, so the rest of the file's
// comments and layout survive; the result is formatted.
func RemoveOptions(rel string, src []byte, name, first string) (out []byte, removed int, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, 0, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := generationLocalName(f)
	if pkg == "" {
		return nil, 0, errors.Newf("%s does not import %s", rel, generationImportPath)
	}
	call := findGeneratorCall(f, pkg)
	if call == nil {
		return nil, 0, errors.Newf("%s makes no %s.NewResourceGenerator call", rel, pkg)
	}
	if len(call.Args) < positionalArgs {
		return nil, 0, errors.Newf("%s: NewResourceGenerator needs %d positional arguments, found %d", rel, positionalArgs, len(call.Args))
	}

	type span struct{ start, end int }
	var spans []span
	for i := positionalArgs; i < len(call.Args); i++ {
		ce, ok := call.Args[i].(*ast.CallExpr)
		if !ok || !isQualified(ce.Fun, pkg, name) || len(ce.Args) == 0 {
			continue
		}
		if s, ok := stringLit(ce.Args[0]); !ok || s != first {
			continue
		}
		start := fset.Position(ce.Pos()).Offset
		// The comment lines between the previous argument and this one introduce it.
		prevLine := fset.Position(call.Args[i-1].End()).Line
		for _, cg := range f.Comments {
			if fset.Position(cg.Pos()).Line > prevLine && fset.Position(cg.End()).Offset <= start {
				start = min(start, fset.Position(cg.Pos()).Offset)
			}
		}
		start = bytes.LastIndexByte(src[:start], '\n') + 1
		end := fset.Position(ce.End()).Offset
		// Through the trailing comma and the end of the line.
		if rest := bytes.TrimLeft(src[end:], " \t"); len(rest) > 0 && rest[0] == ',' {
			end = len(src) - len(rest) + 1
		}
		if rest := bytes.TrimLeft(src[end:], " \t\r"); len(rest) > 0 && rest[0] == '\n' {
			end = len(src) - len(rest) + 1
		}
		spans = append(spans, span{start, end})
	}
	if len(spans) == 0 {
		return src, 0, nil
	}
	var b bytes.Buffer
	at := 0
	for _, s := range spans {
		b.Write(src[at:s.start])
		at = s.end
	}
	b.Write(src[at:])

	out, err = format.Source(b.Bytes())
	if err != nil {
		return nil, 0, errors.Wrapf(err, "format.Source(): %s after removing options", rel)
	}

	return out, len(spans), nil
}

// AddAuthMethod returns the program's source with a sign-in method declaration appended
// to every Auth option naming importPath (on GenerateRoutes or a WithRouterOutlet), and
// how many were edited. declaration is the method as a program writes it
// (generation.GoogleMethod("directory").Labeled("Company directory"), see
// MethodDeclaration). An Auth option already declaring a method of that registration
// name is left alone and not counted. The edit is textual, so the rest of the file's
// comments and layout survive; the result is formatted.
func AddAuthMethod(rel string, src []byte, importPath, name, declaration string) (out []byte, edited int, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, 0, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := generationLocalName(f)
	if pkg == "" {
		return nil, 0, errors.Newf("%s does not import %s", rel, generationImportPath)
	}
	call := findGeneratorCall(f, pkg)
	if call == nil {
		return nil, 0, errors.Newf("%s makes no %s.NewResourceGenerator call", rel, pkg)
	}
	text := declaration
	if pkg != "generation" {
		text = pkg + strings.TrimPrefix(declaration, "generation")
	}

	var inserts []int // the offset of each Auth call's closing parenthesis
	ast.Inspect(call, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || !isQualified(ce.Fun, pkg, optAuth) || len(ce.Args) < 1 {
			return true
		}
		if s, ok := stringLit(ce.Args[0]); !ok || s != importPath {
			return true
		}
		for _, arg := range ce.Args[1:] {
			if declaresMethod(arg, pkg, name) {
				return true
			}
		}
		inserts = append(inserts, fset.Position(ce.Rparen).Offset)

		return true
	})
	if len(inserts) == 0 {
		return src, 0, nil
	}

	var b bytes.Buffer
	at := 0
	for _, offset := range inserts {
		b.Write(src[at:offset])
		if offset > 0 && src[offset-1] != '(' {
			b.WriteString(", ")
		}
		b.WriteString(text)
		at = offset
	}
	b.Write(src[at:])

	out, err = format.Source(b.Bytes())
	if err != nil {
		return nil, 0, errors.Wrapf(err, "format.Source(): %s after adding the %s method", rel, name)
	}

	return out, len(inserts), nil
}

// declaresMethod reports whether an Auth option's argument declares the registration
// name: a method constructor taking it as its name, or PasswordMethod() for password.
func declaresMethod(arg ast.Expr, pkg, name string) bool {
	ce, ok := arg.(*ast.CallExpr)
	if !ok {
		return false
	}
	if sel, ok := ce.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == labeledMethod {
		if inner, ok := sel.X.(*ast.CallExpr); ok {
			ce = inner
		}
	}
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok || !isQualified(ce.Fun, pkg, sel.Sel.Name) {
		return false
	}
	kind, known := authMethodConstructors[sel.Sel.Name]
	if !known {
		return false
	}
	if kind == MethodPassword {
		return name == PasswordRegistration
	}
	if len(ce.Args) != 1 {
		return false
	}
	s, ok := stringLit(ce.Args[0])

	return ok && s == name
}

// MoveDefaultOutlet returns the program's source with the default outlet's declaration
// moved: GenerateRoutes' prefix becomes prefix, and the mount path of its WebApp option,
// when it declares one, becomes mount. The edit is textual, so the rest of the file's
// comments and layout survive; the result is formatted.
func MoveDefaultOutlet(rel string, src []byte, prefix, mount string) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := generationLocalName(f)
	if pkg == "" {
		return nil, errors.Newf("%s does not import %s", rel, generationImportPath)
	}
	call := findGeneratorCall(f, pkg)
	if call == nil {
		return nil, errors.Newf("%s makes no %s.NewResourceGenerator call", rel, pkg)
	}
	var routes *ast.CallExpr
	for _, a := range call.Args[min(positionalArgs, len(call.Args)):] {
		if ce, ok := a.(*ast.CallExpr); ok && isQualified(ce.Fun, pkg, optGenerateRoutes) {
			routes = ce
		}
	}
	if routes == nil || len(routes.Args) < 2 {
		return nil, errors.Newf("%s: no %s with a prefix to move", rel, optGenerateRoutes)
	}

	type span struct {
		start, end int
		text       string
	}
	offset := func(pos token.Pos) int { return fset.Position(pos).Offset }
	spans := []span{{offset(routes.Args[1].Pos()), offset(routes.Args[1].End()), strconv.Quote(prefix)}}
	for _, a := range routes.Args[2:] {
		ce, ok := a.(*ast.CallExpr)
		if !ok || !isQualified(ce.Fun, pkg, optWebApp) || len(ce.Args) != 1 {
			continue
		}
		spans = append(spans, span{offset(ce.Args[0].Pos()), offset(ce.Args[0].End()), strconv.Quote(mount)})
	}

	var b bytes.Buffer
	at := 0
	for _, s := range spans {
		b.Write(src[at:s.start])
		b.WriteString(s.text)
		at = s.end
	}
	b.Write(src[at:])

	out, err := format.Source(b.Bytes())
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): %s after moving the default outlet", rel)
	}

	return out, nil
}
