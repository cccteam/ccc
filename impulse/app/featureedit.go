package app

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"regexp"
	"strconv"

	"github.com/go-playground/errors/v5"
)

// FeaturesFile is the file impulse declares feature flags in, inside a resources package.
const FeaturesFile = "features.go"

// featureStub is the doc comment a declared flag starts with: the description the
// flags dialog shows until the author replaces it.
const featureStub = "// %[1]s is a feature flag. Replace this sentence with what the feature turns on:\n// the comment is the description the feature flags dialog shows beside its switch.\n"

// featuresFileLead introduces a new features file.
const featuresFileLead = `// The application's feature flags: one resource.Feature constant per flag, its value
// the flag's name as the FeatureFlags table holds it and its doc comment the description
// the flags dialog shows. @feature(<Constant>) on a resource, a method or a field puts it
// behind the flag; the deploy's MigrateFeatures writes the table from these declarations.
// impulse add feature declares one here and impulse remove feature takes it out.
`

// AddFeatureConstant returns the file's source with a resource.Feature constant appended:
// the doc stub and the constant, with the resource import added when the file lacks it.
// An empty src starts the file, in the package pkg.
func AddFeatureConstant(rel string, src []byte, pkg, constant, name string) ([]byte, error) {
	declaration := fmt.Sprintf(featureStub+"const %[1]s resource.Feature = %[2]s\n", constant, strconv.Quote(name))
	if len(bytes.TrimSpace(src)) == 0 {
		text := "package " + pkg + "\n\nimport \"" + resourceImportPath + "\"\n\n" + featuresFileLead + "\n" + declaration
		out, err := format.Source([]byte(text))
		if err != nil {
			return nil, errors.Wrapf(err, "format.Source(): %s", rel)
		}

		return out, nil
	}
	withImport, err := AddImport(rel, src, resourceImportPath)
	if err != nil {
		return nil, err
	}
	out, err := format.Source(append(append(bytes.TrimRight(withImport, "\n"), "\n\n"...), declaration...))
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): %s after adding %s", rel, constant)
	}

	return out, nil
}

// RemoveFeatureConstant returns the file's source without the named constant (its doc
// comment and its line, or the whole declaration when it stood alone), dropping the
// resource import when nothing reads it any more, and reports whether the file declares
// nothing else now, so the caller deletes it.
func RemoveFeatureConstant(rel string, src []byte, constant string) (out []byte, empty bool, err error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return nil, false, err
	}
	start, end, found, err := constantSpan(p, constant)
	if err != nil {
		return nil, false, errors.Wrapf(err, "%s", rel)
	}
	if !found {
		return nil, false, errors.Wrapf(ErrNoAnchor, "%s declares no constant %s", rel, constant)
	}
	out, err = p.splice(rel, start, end, "")
	if err != nil {
		return nil, false, err
	}
	rest, err := parseSource(rel, out)
	if err != nil {
		return nil, false, err
	}
	declarations := 0
	for _, d := range rest.file.Decls {
		if gen, ok := d.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			continue
		}
		declarations++
	}
	if declarations == 0 {
		return out, true, nil
	}
	if pkg := localImportName(rest.file, resourceImportPath); pkg != "" && !usesPackage(rest.file, pkg) {
		out, err = dropImport(rel, out, resourceImportPath)
		if err != nil {
			return nil, false, err
		}
	}

	return out, false, nil
}

// constantSpan finds the source span of the named constant's declaration with its doc
// comment: the whole const declaration when the constant is its only spec, the spec's
// lines otherwise. It is an error when the spec declares other names beside it.
func constantSpan(p *parsed, constant string) (start, end int, found bool, err error) {
	for _, decl := range p.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || !namesConstant(vs, constant) {
				continue
			}
			if len(vs.Names) > 1 {
				return 0, 0, false, errors.Newf("constant %s is declared beside %d other name(s) in one spec; remove it by hand", constant, len(vs.Names)-1)
			}
			if len(gen.Specs) == 1 {
				start = p.offset(gen.Pos())
				if gen.Doc != nil {
					start = p.offset(gen.Doc.Pos())
				}
				end = p.offset(gen.End())
			} else {
				start = p.offset(vs.Pos())
				if vs.Doc != nil {
					start = p.offset(vs.Doc.Pos())
				}
				end = p.offset(vs.End())
				if vs.Comment != nil {
					end = p.offset(vs.Comment.End())
				}
			}

			return lineStart(p.src, start), lineEnd(p.src, end), true, nil
		}
	}

	return 0, 0, false, nil
}

func namesConstant(vs *ast.ValueSpec, constant string) bool {
	for _, name := range vs.Names {
		if name.Name == constant {
			return true
		}
	}

	return false
}

// lineStart is the offset of the line holding offset.
func lineStart(src []byte, offset int) int {
	return bytes.LastIndexByte(src[:offset], '\n') + 1
}

// lineEnd is the offset just past the newline ending the line holding offset.
func lineEnd(src []byte, offset int) int {
	i := bytes.IndexByte(src[offset:], '\n')
	if i < 0 {
		return len(src)
	}

	return offset + i + 1
}

// usesPackage reports whether the file selects anything from the package imported under
// the name.
func usesPackage(f *ast.File, pkg string) bool {
	used := false
	ast.Inspect(f, func(n ast.Node) bool {
		if used {
			return false
		}
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg {
				used = true

				return false
			}
		}

		return true
	})

	return used
}

// dropImport removes the import of the path from the file: the spec's line in a block,
// or the whole declaration when it stands alone.
func dropImport(rel string, src []byte, importPath string) ([]byte, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return nil, err
	}
	for _, decl := range p.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gen.Specs {
			is, ok := spec.(*ast.ImportSpec)
			if !ok {
				continue
			}
			path, err := strconv.Unquote(is.Path.Value)
			if err != nil || path != importPath {
				continue
			}
			start, end := p.offset(is.Pos()), p.offset(is.End())
			if len(gen.Specs) == 1 {
				start, end = p.offset(gen.Pos()), p.offset(gen.End())
			}

			return p.splice(rel, lineStart(p.src, start), lineEnd(p.src, end), "")
		}
	}

	return src, nil
}

// RemoveFeatureAnnotations returns the Go source without every @feature(<constant>)
// annotation: a comment line that is the annotation alone goes whole, and the annotation
// leaves a line it shares with other text. The count says how many went.
func RemoveFeatureAnnotations(rel string, src []byte, constant string) (out []byte, removed int, err error) {
	annotation := `@` + featureKeyword + `\([ \t]*` + regexp.QuoteMeta(constant) + `[ \t]*\)`
	lineRE := regexp.MustCompile(`(?m)^[ \t]*//[ \t]*` + annotation + `[ \t]*\r?\n`)
	tokenRE := regexp.MustCompile(`[ \t]*` + annotation)
	out = src
	for _, re := range []*regexp.Regexp{lineRE, tokenRE} {
		matches := re.FindAllIndex(out, -1)
		removed += len(matches)
		out = re.ReplaceAll(out, nil)
	}
	if removed == 0 {
		return src, 0, nil
	}
	formatted, err := format.Source(out)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "format.Source(): %s after removing @%s(%s)", rel, featureKeyword, constant)
	}

	return formatted, removed, nil
}
