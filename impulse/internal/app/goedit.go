package app

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"

	"github.com/go-playground/errors/v5"
)

// ErrNoAnchor reports a source edit whose anchor (a type, a function, a literal) the file
// does not have. A transition records it as work left to the agent rather than failing.
var ErrNoAnchor = errors.New("no anchor for the edit")

// parsed is one Go file with its positions.
type parsed struct {
	fset *token.FileSet
	file *ast.File
	src  []byte
}

func parseSource(rel string, src []byte) (*parsed, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	return &parsed{fset: fset, file: f, src: src}, nil
}

func (p *parsed) offset(pos token.Pos) int { return p.fset.Position(pos).Offset }

// splice replaces src[start:end] with text and formats the result.
func (p *parsed) splice(rel string, start, end int, text string) ([]byte, error) {
	var b bytes.Buffer
	b.Write(p.src[:start])
	b.WriteString(text)
	b.Write(p.src[end:])
	out, err := format.Source(b.Bytes())
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): %s after the edit", rel)
	}

	return out, nil
}

// typeSpec finds the named type declaration.
func (p *parsed) typeSpec(name string) *ast.TypeSpec {
	var spec *ast.TypeSpec
	ast.Inspect(p.file, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == name {
			spec = ts

			return false
		}

		return spec == nil
	})

	return spec
}

// funcDecl finds the named function.
func (p *parsed) funcDecl(name string) *ast.FuncDecl {
	for _, d := range p.file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
			return fd
		}
	}

	return nil
}

// PackageName returns the file's package clause.
func PackageName(rel string, src []byte) (string, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return "", err
	}

	return p.file.Name.Name, nil
}

// HasImport reports whether the file imports the path.
func HasImport(rel string, src []byte, importPath string) (bool, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return false, err
	}

	return localImportName(p.file, importPath) != "", nil
}

// DeclaresType reports whether the file declares the named type.
func DeclaresType(rel string, src []byte, name string) (bool, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return false, err
	}

	return p.typeSpec(name) != nil, nil
}

// AddStructField appends a field ("name Type") to the named struct type.
func AddStructField(rel string, src []byte, typeName, field string) ([]byte, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return nil, err
	}
	spec := p.typeSpec(typeName)
	if spec == nil {
		return nil, errors.Wrapf(ErrNoAnchor, "%s declares no type %s", rel, typeName)
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return nil, errors.Wrapf(ErrNoAnchor, "%s: %s is not a struct", rel, typeName)
	}

	return p.appendToBlock(rel, st.Fields, field)
}

// AddInterfaceLine appends a method or an embedded interface to the named interface type.
func AddInterfaceLine(rel string, src []byte, typeName, line string) ([]byte, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return nil, err
	}
	spec := p.typeSpec(typeName)
	if spec == nil {
		return nil, errors.Wrapf(ErrNoAnchor, "%s declares no type %s", rel, typeName)
	}
	it, ok := spec.Type.(*ast.InterfaceType)
	if !ok {
		return nil, errors.Wrapf(ErrNoAnchor, "%s: %s is not an interface", rel, typeName)
	}

	return p.appendToBlock(rel, it.Methods, line)
}

// appendToBlock inserts a line before a field list's closing brace.
func (p *parsed) appendToBlock(rel string, fields *ast.FieldList, line string) ([]byte, error) {
	if fields == nil || !fields.Closing.IsValid() {
		return nil, errors.Wrapf(ErrNoAnchor, "%s: the block has no closing brace", rel)
	}
	closing := p.offset(fields.Closing)

	return p.splice(rel, closing, closing, line+"\n")
}

// AddLiteralElement appends "key: value" to the first composite literal of the named
// type inside the named function.
func AddLiteralElement(rel string, src []byte, funcName, typeName, element string) ([]byte, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return nil, err
	}
	fd := p.funcDecl(funcName)
	if fd == nil {
		return nil, errors.Wrapf(ErrNoAnchor, "%s declares no function %s", rel, funcName)
	}
	lit := literalOf(fd, typeName)
	if lit == nil {
		return nil, errors.Wrapf(ErrNoAnchor, "%s: %s builds no %s literal", rel, funcName, typeName)
	}
	text := element + ",\n"
	if n := len(lit.Elts); n > 0 && !bytes.Contains(p.src[p.offset(lit.Elts[n-1].End()):p.offset(lit.Rbrace)], []byte(",")) {
		text = ",\n" + text
	}
	rbrace := p.offset(lit.Rbrace)

	return p.splice(rel, rbrace, rbrace, text)
}

// literalOf finds the first composite literal of the named type in a function.
func literalOf(fd *ast.FuncDecl, typeName string) *ast.CompositeLit {
	var lit *ast.CompositeLit
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if lit != nil {
			return false
		}
		if cl, ok := n.(*ast.CompositeLit); ok {
			if id, ok := cl.Type.(*ast.Ident); ok && id.Name == typeName {
				lit = cl

				return false
			}
		}

		return true
	})

	return lit
}

// WrapReturn rewrites the "return &Type{...}, nil" of the named function so the literal
// is assigned to varName, the call runs against it, and its error is returned wrapped
// by wrapErr (an expression over err), before varName is returned.
func WrapReturn(rel string, src []byte, funcName, typeName, varName, call, wrapErr string) ([]byte, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return nil, err
	}
	fd := p.funcDecl(funcName)
	if fd == nil {
		return nil, errors.Wrapf(ErrNoAnchor, "%s declares no function %s", rel, funcName)
	}
	ret := returnOfLiteral(fd, typeName)
	if ret == nil {
		return nil, errors.Wrapf(ErrNoAnchor, "%s: %s has no \"return &%s{...}, nil\"", rel, funcName, typeName)
	}
	literal := string(p.src[p.offset(ret.Results[0].Pos()):p.offset(ret.Results[0].End())])
	text := varName + " := " + literal + "\n" +
		"if err := " + call + "; err != nil {\n" +
		"return nil, " + wrapErr + "\n" +
		"}\n\n" +
		"return " + varName + ", nil"

	return p.splice(rel, p.offset(ret.Pos()), p.offset(ret.End()), text)
}

// returnOfLiteral finds the first "return &Type{...}, <x>" statement in a function.
func returnOfLiteral(fd *ast.FuncDecl, typeName string) *ast.ReturnStmt {
	var ret *ast.ReturnStmt
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if ret != nil {
			return false
		}
		rs, ok := n.(*ast.ReturnStmt)
		if !ok || len(rs.Results) != 2 {
			return true
		}
		unary, ok := rs.Results[0].(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			return true
		}
		if cl, ok := unary.X.(*ast.CompositeLit); ok {
			if id, ok := cl.Type.(*ast.Ident); ok && id.Name == typeName {
				ret = rs

				return false
			}
		}

		return true
	})

	return ret
}

// StructFieldOfType returns the name of the first field of the named struct whose type is
// a pointer to typeName from the package at importPath, or empty.
func StructFieldOfType(rel string, src []byte, structName, importPath, typeName string) (string, error) {
	p, err := parseSource(rel, src)
	if err != nil {
		return "", err
	}
	pkg := localImportName(p.file, importPath)
	spec := p.typeSpec(structName)
	if pkg == "" || spec == nil {
		return "", nil
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return "", nil
	}
	for _, f := range st.Fields.List {
		star, ok := f.Type.(*ast.StarExpr)
		if !ok || len(f.Names) == 0 {
			continue
		}
		if isQualified(star.X, pkg, typeName) {
			return f.Names[0].Name, nil
		}
	}

	return "", nil
}
