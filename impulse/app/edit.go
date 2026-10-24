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
// name: a method constructor taking it as its name, or PasswordMethod() for password,
// whatever label and flags are chained on it.
func declaresMethod(arg ast.Expr, pkg, name string) bool {
	declared, _, ok := declaredMethod(arg, pkg)

	return ok && declared == name
}

// declaredMethod reads the registration name an Auth option's argument declares, with
// the flags chained on it (InvitedUsers, SyncsRoles, OpenSignUp, UsernameIsEmail): a
// method constructor taking the name as its one string literal, or PasswordMethod() for
// password, under .Labeled(label) and the flags in any order. Anything else is not a
// declaration.
func declaredMethod(arg ast.Expr, pkg string) (name string, flags map[string]bool, ok bool) {
	decl, _, flags, ok := splitChain(arg)
	if !ok {
		return "", nil, false
	}
	ce, ok := decl.(*ast.CallExpr)
	if !ok {
		return "", nil, false
	}
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok || !isQualified(ce.Fun, pkg, sel.Sel.Name) {
		return "", nil, false
	}
	kind, known := authMethodConstructors[sel.Sel.Name]
	if !known {
		return "", nil, false
	}
	if kind == MethodPassword {
		return PasswordRegistration, flags, true
	}
	if len(ce.Args) != 1 {
		return "", nil, false
	}
	s, ok := stringLit(ce.Args[0])
	if !ok {
		return "", nil, false
	}

	return s, flags, true
}

// ChainMethodFlag returns the program's source with a flag chained on the declaration of
// the method registered under name in every Auth option naming importPath, and how many
// declarations were edited. flag is the flag's method as the generation package spells
// it (SyncsRoles), appended after whatever the declaration chains already
// (generation.GoogleMethod("directory").Labeled("Company directory").SyncsRoles()). A
// declaration chaining the flag is left alone and not counted, so the edit is safe to
// run twice. The edit is textual, so the rest of the file's comments and layout survive;
// the result is formatted.
func ChainMethodFlag(rel string, src []byte, importPath, name, flag string) (out []byte, edited int, err error) {
	switch flag {
	case invitedUsersMethod, syncsRolesMethod, openSignUpMethod, usernameIsEmailMethod:
	default:
		return nil, 0, errors.Newf("%q is not a flag a method declaration chains: %s, %s, %s or %s", flag, invitedUsersMethod, syncsRolesMethod, openSignUpMethod, usernameIsEmailMethod)
	}
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

	// The offset each declaration ends at, in source order: the inspection walks the
	// generator call depth first and the Auth options' arguments never overlap, so the
	// offsets ascend as collected.
	var ends []int
	ast.Inspect(call, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || !isQualified(ce.Fun, pkg, optAuth) || len(ce.Args) < 1 {
			return true
		}
		if s, ok := stringLit(ce.Args[0]); !ok || s != importPath {
			return true
		}
		for _, arg := range ce.Args[1:] {
			declared, flags, ok := declaredMethod(arg, pkg)
			if !ok || declared != name || flags[flag] {
				continue
			}
			ends = append(ends, fset.Position(arg.End()).Offset)
		}

		return true
	})
	if len(ends) == 0 {
		return src, 0, nil
	}

	var b bytes.Buffer
	at := 0
	for _, offset := range ends {
		b.Write(src[at:offset])
		b.WriteString("." + flag + "()")
		at = offset
	}
	b.Write(src[at:])

	out, err = format.Source(b.Bytes())
	if err != nil {
		return nil, 0, errors.Wrapf(err, "format.Source(): %s after chaining .%s() on the %s method", rel, flag, name)
	}

	return out, len(ends), nil
}

// BindOutletAuth returns the program's source with the outlet's Auth option set to
// declaration (generation.Auth("<import path>", generation.PasswordMethod()), as a program
// writes it), and whether an Auth option was there to replace: the one the outlet
// declared is replaced, or the declaration is added after the outlet's prefix when it
// declared none. outlet is the outlet's name: the reserved default names the
// GenerateRoutes declaration, any other a WithRouterOutlet. The edit is textual, so the
// rest of the file's comments and layout survive; the result is formatted.
func BindOutletAuth(rel string, src []byte, outlet, declaration string) (out []byte, replaced bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, false, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := generationLocalName(f)
	if pkg == "" {
		return nil, false, errors.Newf("%s does not import %s", rel, generationImportPath)
	}
	call := findGeneratorCall(f, pkg)
	if call == nil {
		return nil, false, errors.Newf("%s makes no %s.NewResourceGenerator call", rel, pkg)
	}
	decl := findOutletCall(call, pkg, outlet)
	if decl == nil {
		return nil, false, errors.Newf("%s declares no %s outlet", rel, outlet)
	}
	text := qualify(declaration, pkg)
	var b bytes.Buffer
	for _, arg := range decl.Args[outletPositionalArgs:] {
		ce, ok := arg.(*ast.CallExpr)
		if !ok || !isQualified(ce.Fun, pkg, optAuth) {
			continue
		}
		b.Write(src[:fset.Position(ce.Pos()).Offset])
		b.WriteString(text)
		b.Write(src[fset.Position(ce.End()).Offset:])
		out, err = format.Source(b.Bytes())
		if err != nil {
			return nil, false, errors.Wrapf(err, "format.Source(): %s after binding the %s outlet", rel, outlet)
		}

		return out, true, nil
	}
	// No Auth option: the declaration follows the prefix, on a line of its own when the
	// outlet's options are one per line.
	end := fset.Position(decl.Args[outletPositionalArgs-1].End()).Offset
	b.Write(src[:end])
	rest := src[end:]
	trimmed := bytes.TrimLeft(rest, " \t")
	if len(trimmed) > 1 && trimmed[0] == ',' && (trimmed[1] == '\n' || trimmed[1] == '\r') {
		comma := len(rest) - len(trimmed) + 1
		b.Write(rest[:comma])
		b.WriteString("\n" + text + ",")
		b.Write(rest[comma:])
	} else {
		b.WriteString(", " + text)
		b.Write(rest)
	}
	out, err = format.Source(b.Bytes())
	if err != nil {
		return nil, false, errors.Wrapf(err, "format.Source(): %s after binding the %s outlet", rel, outlet)
	}

	return out, false, nil
}

// outletPositionalArgs is the number of arguments an outlet declaration takes before its
// options: GenerateRoutes(targetDir, prefix, ...) and WithRouterOutlet(name, prefix, ...).
const outletPositionalArgs = 2

// findOutletCall returns the outlet's declaration among the generator call's options: the
// GenerateRoutes call for the default outlet, the WithRouterOutlet call naming any other;
// nil when the program declares no such outlet.
func findOutletCall(call *ast.CallExpr, pkg, outlet string) *ast.CallExpr {
	if len(call.Args) < positionalArgs {
		return nil
	}
	for _, a := range call.Args[positionalArgs:] {
		ce, ok := a.(*ast.CallExpr)
		if !ok || len(ce.Args) < outletPositionalArgs {
			continue
		}
		if outlet == DefaultOutletName {
			if isQualified(ce.Fun, pkg, optGenerateRoutes) {
				return ce
			}

			continue
		}
		if s, ok := stringLit(ce.Args[0]); ok && s == outlet && isQualified(ce.Fun, pkg, optWithRouterOutlet) {
			return ce
		}
	}

	return nil
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
