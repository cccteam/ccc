package app

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
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
