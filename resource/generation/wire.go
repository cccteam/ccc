package generation

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// The wire vocabulary is one set of rules for every struct the generator carries
// across the wire by value: RPC requests, RPC results, and computed resource rows.
// Table-backed resources are rows and rows are flat; a row's struct-typed column is
// the one place the same walk runs on the column path (see below).
//
// Allowed: basic types; named non-struct types as leaves, typed by their underlying
// basic type; named types the generator's built-in table maps (time.Time, UUIDs,
// dates, decimals) and types carrying a @typescript declaration as leaves; a byte
// slice, unnamed or a named slice over byte with no JSON methods, as one leaf (bytes:
// a base64 string on the wire, never a list of numbers; see leaf.go); every other
// named struct type is walked wherever it is declared; a pointer to any of those; one
// slice level per field, of any of those. Refused, each naming the field path: a
// struct that reaches itself, maps, interfaces, channels, functions, anonymous
// structs, arrays, a slice of slices or a pointer to a slice (a byte slice being a
// leaf, not a slice, here), embedded and unexported fields, and a named type whose
// underlying type is none of the above. There is no depth limit.
//
// Each walked struct gets a local mirror type in the handler file with generated
// camel-case JSON tags, so the wire shape lives entirely in generated code. Data
// crosses between source and mirror by conversion where Go allows it (a struct whose
// fields are all leaves) and otherwise through a pinned view: an anonymous struct
// spelling the source's fields with their original types, converted from the source
// before the literal reads it. The view is legal now and stops compiling the moment
// the source gains, loses, or retypes a field, so a stale handler is a compile error
// until the generator runs again.
//
// The column path (newColumnWalker) derives the TypeScript interface of a struct a
// table or view column holds. No mirror is generated there: the runtime marshals the
// column value as declared, so the struct's json tags are its wire names (every field
// needs one, "-" leaves the field out, omitempty makes it optional), and a struct that
// writes its own JSON is refused unless it declares its TypeScript type, since its
// fields say nothing about the wire.

// wireShape is one struct crossing the wire: its source type and the local mirror.
type wireShape struct {
	// Mirror is the local mirror type's name. The root's is chosen by the handler
	// template (request, response, the resource's camel name); nested mirrors take
	// their source type's name with a lower-case initial.
	Mirror string
	// Source is the source struct type, qualified by package name (rpc.Reading).
	Source string
	// Fields are the struct's fields in declaration order.
	Fields []*wireField
	// nested lists every struct reachable from this one, leaves first, each once.
	nested []*wireShape
	named  *types.Named
}

// wireField is one field of a wireShape.
type wireField struct {
	// Name is the Go field name; JSONName its generated wire name.
	Name     string
	JSONName string
	// Pointer marks *T; Slice marks []T; ElemPointer marks []*T.
	Pointer     bool
	Slice       bool
	ElemPointer bool
	// Nested is the walked struct behind the field, nil for a leaf.
	Nested *wireShape
	// Optional marks a column-path field tagged omitempty: the interface declares it
	// optional, as the runtime leaves it out of the JSON when it is empty.
	Optional bool
	// SourceType is the field's type as declared, qualified by package name.
	SourceType string
	// tsLeaf is a leaf's TypeScript type before any [] suffix, and tsImport the
	// @typescript declaration behind it, nil for a built-in row.
	tsLeaf   string
	tsImport *tsImport
	// Tag carries the source field's struct tag for the call sites that read one.
	Tag reflect.StructTag
	// Imports are the packages the field's type reaches.
	Imports []parser.Import
}

// IsLeaf reports whether the field is carried as-is, with no mirror behind it.
func (f *wireField) IsLeaf() bool {
	return f.Nested == nil
}

// prefix renders the field's pointer and slice markers as a type prefix.
func (f *wireField) prefix() string {
	var b strings.Builder
	if f.Pointer {
		b.WriteString("*")
	}
	if f.Slice {
		b.WriteString(sliceSuffix)
	}
	if f.ElemPointer {
		b.WriteString("*")
	}

	return b.String()
}

// MirrorType is the field's type inside the mirror: the source type for a leaf, the
// nested mirror behind the same pointer and slice markers otherwise.
func (f *wireField) MirrorType() string {
	if f.IsLeaf() {
		return f.SourceType
	}

	return f.prefix() + f.Nested.Mirror
}

// TypescriptType is the field's TypeScript type inside an interface: the leaf's
// mapped type, or the nested interface qualified by the namespace it declares in.
func (f *wireField) TypescriptType(namespace string) string {
	base := tsDataType(f.tsLeaf)
	if !f.IsLeaf() {
		base = namespace + "." + f.Nested.TypescriptName()
	}
	if f.Slice {
		base += sliceSuffix
	}

	return base
}

// TypescriptDisplayType is the field's display type in generated metadata: the
// leaf's type, or object for a nested field, an imported type, or unknown, with []
// for a slice. The templates render it through renderDisplayType, which lower-cases it
// and refuses anything outside the vocabulary (displaytype.go).
func (f *wireField) TypescriptDisplayType() string {
	base := leafDisplayType(f.tsLeaf, f.tsImport)
	if !f.IsLeaf() {
		base = objectDisplayType
	}
	if f.Slice {
		base += sliceSuffix
	}

	return base
}

// sliceSuffix marks a TypeScript array type.
const sliceSuffix = "[]"

// leafDataType is tsDataType over a leaf that may carry the [] suffix: the element's
// interface type, with the suffix kept.
func leafDataType(leaf string) string {
	base, slice := strings.CutSuffix(leaf, sliceSuffix)
	base = tsDataType(base)
	if slice {
		return base + sliceSuffix
	}

	return base
}

// tsDataType maps a metadata display type to the TypeScript type an interface
// declares for it: a uuid and a byte slice (base64) are strings, a civil date a Date.
func tsDataType(displayType string) string {
	switch displayType {
	case uuidTSType, bytesTSType:
		return stringTSType
	case civilDateTSType:
		return dateTSType
	default:
		return displayType
	}
}

// Flat reports whether every field is a leaf, so the mirror converts whole. A nil
// shape (a handler built without a walk) is flat.
func (s *wireShape) Flat() bool {
	if s == nil {
		return true
	}
	for _, f := range s.Fields {
		if !f.IsLeaf() {
			return false
		}
	}

	return true
}

// Nested returns every struct reachable from this one, leaves first, each once.
func (s *wireShape) Nested() []*wireShape {
	if s == nil {
		return nil
	}

	return s.nested
}

// TypescriptName is the nested interface's name: the source type's own name.
func (s *wireShape) TypescriptName() string {
	return strcase.ToPascal(s.Mirror)
}

// TypescriptImports lists the @typescript declarations every leaf under the shape
// carries, so the file declaring the shape's interfaces imports them.
func (s *wireShape) TypescriptImports() []*tsImport {
	if s == nil {
		return nil
	}
	var imports []*tsImport
	seen := make(map[*wireShape]bool)
	var visit func(*wireShape)
	visit = func(sh *wireShape) {
		if seen[sh] {
			return
		}
		seen[sh] = true
		for _, f := range sh.Fields {
			if f.tsImport != nil {
				imports = append(imports, f.tsImport)
			}
			if f.Nested != nil {
				visit(f.Nested)
			}
		}
	}
	visit(s)

	return imports
}

// Imports lists the packages every leaf type under the shape reaches, so the
// handler file can import them.
func (s *wireShape) Imports() []parser.Import {
	if s == nil {
		return nil
	}
	seen := make(map[string]parser.Import)
	var visit func(*wireShape)
	visit = func(sh *wireShape) {
		for _, f := range sh.Fields {
			for _, imp := range f.Imports {
				seen[imp.Path] = imp
			}
			if f.Nested != nil {
				visit(f.Nested)
			}
		}
	}
	visit(s)

	imports := make([]parser.Import, 0, len(seen))
	for _, imp := range seen {
		imports = append(imports, imp)
	}
	slices.SortFunc(imports, func(a, b parser.Import) int { return strings.Compare(a.Path, b.Path) })

	return imports
}

// MirrorDecls renders the nested mirror type declarations, leaves first, indented
// for the body of the handler function. A flat shape renders nothing.
func (s *wireShape) MirrorDecls() string {
	return mirrorDecls(s.Nested())
}

func mirrorDecls(nested []*wireShape) string {
	var b strings.Builder
	for _, n := range nested {
		fmt.Fprintf(&b, "\ttype %s struct {\n", n.Mirror)
		for _, f := range n.Fields {
			fmt.Fprintf(&b, "\t\t%s %s `%s:%q`\n", f.Name, f.MirrorType(), jsonTagKey, f.JSONName)
		}
		b.WriteString("\t}\n\n")
	}

	return b.String()
}

// typescriptNamespace renders the nested interfaces a shape declares, in a
// namespace of the given name so two roots sharing a Go struct get independent
// types. Empty for a flat shape.
func typescriptNamespace(name string, shape *wireShape) string {
	return typescriptNamespaceOf(name, shape.Nested())
}

// typescriptNamespaceOf renders the namespace for an explicit list of nested
// shapes, so a method's request and result share one.
func typescriptNamespaceOf(name string, nested []*wireShape) string {
	if len(nested) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "export namespace %s {\n", name)
	for i, n := range nested {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "  export interface %s {\n", n.TypescriptName())
		for _, f := range n.Fields {
			optional := ""
			if f.Optional {
				optional = "?"
			}
			fmt.Fprintf(&b, "    %s%s: %s;\n", f.JSONName, optional, f.TypescriptType(name))
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}\n")

	return b.String()
}

// wireDirection is which way a converter carries data.
type wireDirection int

const (
	// toMirror builds the local mirror from the source: results and computed rows.
	toMirror wireDirection = iota
	// toSource builds the source from the local mirror: decoded requests.
	toSource
)

// Converters renders the converter closures a non-flat shape needs, one per struct
// that is not itself flat, leaves first and the root last, indented for the body of
// the handler function. rootMirror names the root's mirror type, which the handler
// template declares. Each closure takes its input by value and pins the source's
// shape with an anonymous-struct view, so a change to the source is a compile error
// here until the generator runs again. A flat shape renders nothing: its whole
// conversion is the pin.
func (s *wireShape) Converters(direction wireDirection, rootMirror string) string {
	if s.Flat() {
		return ""
	}

	var b strings.Builder
	for _, n := range s.nested {
		if !n.Flat() {
			n.writeConverter(&b, direction, n.Mirror, false)
		}
	}
	s.writeConverter(&b, direction, rootMirror, true)

	return b.String()
}

// ConverterName is the root converter closure's name for the direction.
func (s *wireShape) ConverterName(direction wireDirection, rootMirror string) string {
	return converterName(direction, rootMirror)
}

func converterName(direction wireDirection, mirror string) string {
	switch direction {
	case toMirror:
		return "mirror" + strcase.ToPascal(mirror)
	default:
		return "source" + strcase.ToPascal(mirror)
	}
}

// writeConverter renders one closure. The root converter returns a pointer, the
// value the handler works with; nested converters return values the root composes.
func (s *wireShape) writeConverter(b *strings.Builder, direction wireDirection, mirror string, root bool) {
	in, out := s.Source, mirror
	if direction == toSource {
		in, out = mirror, s.Source
	}
	ret := out
	if root {
		ret = "*" + out
	}
	fmt.Fprintf(b, "\t%s := func(src %s) %s {\n", converterName(direction, mirror), in, ret)

	// The pinned view: reading the source through an anonymous struct spelling its
	// fields with their original types. In the mirror direction the view is the
	// input; in the source direction it is the output the literal fills.
	if direction == toMirror {
		b.WriteString("\t\tview := ")
		s.writeView(b)
		b.WriteString("(src)\n")
	}
	from := "view"
	if direction == toSource {
		from = "src"
	}

	for _, f := range s.Fields {
		if f.IsLeaf() {
			continue
		}
		f.writeLocal(b, direction, from)
	}

	// The literal: a mirror value, or the pinned view converted to the source type.
	// The root returns a pointer, so the source direction converts a pointer to the
	// view into a pointer to the source (legal: identical underlying struct types).
	b.WriteString("\n\t\treturn ")
	switch {
	case direction == toSource && root:
		fmt.Fprintf(b, "(*%s)(&", out)
		s.writeView(b)
	case direction == toSource:
		fmt.Fprintf(b, "%s(", out)
		s.writeView(b)
	case root:
		b.WriteString("&" + out)
	default:
		b.WriteString(out)
	}
	b.WriteString("{")
	for i, f := range s.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "%s: %s", f.Name, f.valueExpr(direction, from))
	}
	b.WriteString("}")
	if direction == toSource {
		b.WriteString(")")
	}
	b.WriteString("\n\t}\n\n")
}

// writeView renders the anonymous struct spelling the source's fields.
func (s *wireShape) writeView(b *strings.Builder) {
	b.WriteString("struct {\n")
	for _, f := range s.Fields {
		fmt.Fprintf(b, "\t\t\t%s %s\n", f.Name, f.SourceType)
	}
	b.WriteString("\t\t}")
}

// local is the closure-local variable a nested field is built into.
func (f *wireField) local() string {
	return strcase.ToGoCamel(f.Name)
}

// convert renders the expression carrying one element across: a conversion for a
// flat nested struct, the nested converter otherwise.
func (f *wireField) convert(direction wireDirection, expr string) string {
	n := f.Nested
	if n.Flat() {
		if direction == toMirror {
			return fmt.Sprintf("%s(%s)", n.Mirror, expr)
		}

		return fmt.Sprintf("%s(%s)", n.Source, expr)
	}

	return fmt.Sprintf("%s(%s)", converterName(direction, n.Mirror), expr)
}

// elemType is the type one element of the field has on the output side.
func (f *wireField) elemType(direction wireDirection) string {
	if direction == toMirror {
		return f.Nested.Mirror
	}

	return f.Nested.Source
}

// valueExpr is the expression the literal assigns to the field.
func (f *wireField) valueExpr(direction wireDirection, from string) string {
	switch {
	case f.IsLeaf():
		return from + "." + f.Name
	case !f.Pointer && !f.Slice:
		return f.convert(direction, from+"."+f.Name)
	default:
		return f.local()
	}
}

// writeLocal renders the statements building a nested pointer or slice field into
// its local, preserving nil: a nil pointer or slice stays nil, so it encodes as
// null exactly as the source would.
func (f *wireField) writeLocal(b *strings.Builder, direction wireDirection, from string) {
	if !f.Pointer && !f.Slice {
		return
	}
	src := from + "." + f.Name
	elem := f.elemType(direction)
	local := f.local()

	switch {
	case f.Pointer:
		fmt.Fprintf(b, "\t\tvar %s *%s\n\t\tif %s != nil {\n\t\t\tv := %s\n\t\t\t%s = &v\n\t\t}\n", local, elem, src, f.convert(direction, "*"+src), local)
	case f.ElemPointer:
		fmt.Fprintf(b, "\t\tvar %s []*%s\n\t\tif %s != nil {\n\t\t\t%s = make([]*%s, 0, len(%s))\n\t\t\tfor _, e := range %s {\n\t\t\t\tif e == nil {\n\t\t\t\t\t%s = append(%s, nil)\n\n\t\t\t\t\tcontinue\n\t\t\t\t}\n\t\t\t\tv := %s\n\t\t\t\t%s = append(%s, &v)\n\t\t\t}\n\t\t}\n",
			local, elem, src, local, elem, src, src, local, local, f.convert(direction, "*e"), local, local)
	default:
		fmt.Fprintf(b, "\t\tvar %s []%s\n\t\tif %s != nil {\n\t\t\t%s = make([]%s, 0, len(%s))\n\t\t\tfor _, e := range %s {\n\t\t\t\t%s = append(%s, %s)\n\t\t\t}\n\t\t}\n",
			local, elem, src, local, elem, src, src, local, local, f.convert(direction, "e"))
	}
}

// wireWalker walks structs under the wire rules, sharing nested mirrors across
// the fields that reach them and refusing the names that would collide.
type wireWalker struct {
	// leaves resolves the generator's leaf types: the built-in table and the
	// @typescript declarations.
	leaves *leafResolver
	// columns marks the column path: wire names come from json tags, a struct with
	// its own JSON methods is refused, and no mirror is generated.
	columns bool
	// qualifiers are the package names the handler file imports: the fixed set
	// every handler imports, the packages the caller names, and every package a
	// leaf type reaches. A mirror, converter, or local named like one would shadow
	// the import.
	qualifiers map[string]struct{}
	shapes     map[*types.Named]*wireShape
	names      map[string]*types.Named
	order      []*wireShape
}

// handlerIdentifiers are the names generated handlers declare around the mirrors;
// a mirror type named like one would shadow it.
var handlerIdentifiers = []string{
	"request", "response", "params", "p", "decoder", "ctx", "span", "err", "w", "r",
	"row", "rec", "rmap", "resp", defaultDomainRouteParam, "gate", "txn", "result", idIdentifier, "querySet",
}

// idIdentifier is the local the generated read handlers bind a single key to.
const idIdentifier = "id"

// converterIdentifiers are the names converter closures declare; a local built
// for a nested field cannot take one.
var converterIdentifiers = []string{"src", "view", "e", "v"}

// handlerQualifiers are the package qualifiers every handler file imports.
var handlerQualifiers = []string{contextQualifier, httpQualifier, cccQualifier, "accesstypes", "resource", "tracer", "httpio", errorsQualifier, routerQualifier}

// The qualifiers the import fixer and the handler templates both name.
const (
	bytesQualifier   = "bytes"
	contextQualifier = "context"
	httpQualifier    = "http"
	cccQualifier     = "ccc"
	errorsQualifier  = "errors"
	routerQualifier  = "router"
)

// newWireWalker builds a walker over the leaf resolver. qualifiers names the
// packages the handler file imports beyond the fixed set: the source package and
// the application packages the template references.
func newWireWalker(leaves *leafResolver, qualifiers ...string) *wireWalker {
	w := &wireWalker{
		leaves:     leaves,
		qualifiers: make(map[string]struct{}, len(handlerQualifiers)+len(qualifiers)),
		shapes:     make(map[*types.Named]*wireShape),
		names:      make(map[string]*types.Named),
	}
	for _, q := range handlerQualifiers {
		w.qualifiers[q] = struct{}{}
	}
	for _, q := range qualifiers {
		w.qualifiers[q] = struct{}{}
	}

	return w
}

// newColumnWalker builds a walker for the column path: it derives the interfaces of
// the structs one resource's columns hold, each struct once, under the column rules.
func newColumnWalker(leaves *leafResolver) *wireWalker {
	w := newWireWalker(leaves)
	w.columns = true

	return w
}

// walkColumn walks the struct a column holds, returning its shape as a nested
// interface: the struct is its own TypeScript interface in the resource's namespace,
// and everything it reaches precedes it in Shapes.
func (w *wireWalker) walkColumn(named *types.Named, path string) (*wireShape, error) {
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil, errors.Newf("%s: %s is not a struct", path, typeStringer(named))
	}
	if err := w.guardJSONMethods(named, path); err != nil {
		return nil, err
	}

	return w.nestedShape(named, st, path, nil)
}

// Shapes lists every struct the walker mirrored, leaves first, each once.
func (w *wireWalker) Shapes() []*wireShape {
	return w.order
}

// guardJSONMethods refuses, on the column path, a struct that writes or reads its own
// JSON and declares no TypeScript type: the runtime marshals the column value as
// declared, so the struct's fields do not describe the wire.
func (w *wireWalker) guardJSONMethods(named *types.Named, path string) error {
	if !w.columns || !hasJSONMethods(named) {
		return nil
	}

	return errors.Newf("%s: %s writes its own JSON (MarshalJSON or UnmarshalJSON), so its fields do not describe the wire; add @%s(...) to its declaration", path, typeStringer(named), typescriptKeyword)
}

// walk walks a root struct, returning its shape with every nested struct behind it.
func (w *wireWalker) walk(root *parser.Struct) (*wireShape, error) {
	named, ok := root.GoType().(*types.Named)
	if !ok {
		return nil, errors.Newf("struct %s: not a named struct type", root.Name())
	}

	return w.walkNamed(named, root.Name())
}

// walkNamed walks a root named struct type. One walker may walk several roots
// (a method's request and its result): nested structs they share keep one mirror,
// and every name is checked against everything the walker has seen.
func (w *wireWalker) walkNamed(named *types.Named, path string) (*wireShape, error) {
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil, errors.Newf("struct %s: underlying type %s is not a struct", path, named.Underlying())
	}

	shape := &wireShape{Source: typeStringer(named), named: named}
	if err := w.fill(shape, st, path, []*types.Named{named}); err != nil {
		return nil, err
	}
	shape.nested = reachable(shape, w.order)

	return shape, w.checkNames(shape, path)
}

// reachable returns the walked shapes a root reaches, in the walker's order
// (leaves first), so a root declares only its own mirrors.
func reachable(root *wireShape, order []*wireShape) []*wireShape {
	seen := make(map[*wireShape]bool)
	var visit func(*wireShape)
	visit = func(sh *wireShape) {
		for _, f := range sh.Fields {
			if f.Nested != nil && !seen[f.Nested] {
				seen[f.Nested] = true
				visit(f.Nested)
			}
		}
	}
	visit(root)

	var out []*wireShape
	for _, sh := range order {
		if seen[sh] {
			out = append(out, sh)
		}
	}

	return out
}

// mergeNested unions the nested shapes of several roots walked by one walker,
// each once, leaves first, so one handler declares one mirror per struct.
func mergeNested(roots ...*wireShape) []*wireShape {
	seen := make(map[*wireShape]bool)
	var out []*wireShape
	for _, root := range roots {
		for _, sh := range root.Nested() {
			if !seen[sh] {
				seen[sh] = true
				out = append(out, sh)
			}
		}
	}

	return out
}

// checkNames refuses the mirror, converter, and local names that would shadow an
// identifier the generated handler relies on. It runs once the walk is complete,
// when every package a leaf reaches is known.
func (w *wireWalker) checkNames(root *wireShape, path string) error {
	for _, imp := range root.Imports() {
		w.qualifiers[imp.Name] = struct{}{}
	}

	taken := make(map[string]string, len(root.nested)*2)
	for _, n := range root.nested {
		if _, q := w.qualifiers[n.Mirror]; q {
			return errors.Newf("%s: %s would mirror as %q, a package the generated handler imports; rename the type", path, n.Source, n.Mirror)
		}
		if slices.Contains(handlerIdentifiers, n.Mirror) {
			return errors.Newf("%s: %s would mirror as %q, a name the generated handler uses; rename the type", path, n.Source, n.Mirror)
		}
		taken[n.Mirror] = n.Source
		for _, direction := range []wireDirection{toMirror, toSource} {
			taken[converterName(direction, n.Mirror)] = n.Source
		}
	}

	shapes := append(slices.Clone(root.nested), root)
	for _, sh := range shapes {
		if sh.Flat() {
			continue
		}
		for _, f := range sh.Fields {
			if f.IsLeaf() || (!f.Pointer && !f.Slice) {
				continue
			}
			local := f.local()
			_, qualifier := w.qualifiers[local]
			switch {
			case slices.Contains(converterIdentifiers, local):
				return errors.Newf("%s.%s: the field's name %q is used by the generated converter; rename the field", sh.Source, f.Name, local)
			case qualifier:
				return errors.Newf("%s.%s: the field's name %q is a package the generated handler imports; rename the field", sh.Source, f.Name, local)
			case taken[local] != "":
				return errors.Newf("%s.%s: the field's name %q collides with the mirror of %s; rename one", sh.Source, f.Name, local, taken[local])
			}
		}
	}

	return nil
}

// fill walks one struct's fields into the shape. path is the field path for
// messages; stack holds the structs being walked, for recursion.
func (w *wireWalker) fill(shape *wireShape, st *types.Struct, path string, stack []*types.Named) error {
	for i := range st.NumFields() {
		v := st.Field(i)
		fieldPath := path + "." + v.Name()
		switch {
		case v.Embedded():
			return errors.Newf("%s: embedded fields do not cross the wire; name the field", fieldPath)
		case !v.Exported():
			return errors.Newf("%s: unexported fields do not cross the wire; export it or move it out of the struct", fieldPath)
		}

		f := &wireField{
			Name:       v.Name(),
			JSONName:   caser.ToCamel(v.Name()),
			SourceType: typeStringer(v.Type()),
			Tag:        reflect.StructTag(st.Tag(i)),
			Imports:    parser.TypeImports(v.Type()),
		}
		if w.columns {
			keep, err := f.readJSONTag(fieldPath)
			if err != nil {
				return err
			}
			if !keep {
				continue
			}
		}
		if err := w.classify(f, v.Type(), fieldPath, stack); err != nil {
			return err
		}
		shape.Fields = append(shape.Fields, f)
	}

	return nil
}

// readJSONTag reads a column-path field's wire name from its json tag: the runtime
// marshals the column value as declared, so the tag is the name. Every field needs
// one; "-" leaves the field out of the interface (keep is false); omitempty makes it
// optional.
func (f *wireField) readJSONTag(path string) (keep bool, err error) {
	tag, ok := f.Tag.Lookup(jsonTagKey)
	if !ok {
		return false, errors.Newf("%s: no json tag; a struct held by a column is marshaled as declared, so every field names its wire name in a json tag", path)
	}
	name, options, _ := strings.Cut(tag, ",")
	switch name {
	case "-":
		return false, nil
	case "":
		return false, errors.Newf("%s: the json tag names no field; a struct held by a column is marshaled as declared, so every field names its wire name in a json tag", path)
	}
	f.JSONName = name
	f.Optional = slices.Contains(strings.Split(options, ","), "omitempty")

	return true, nil
}

// classify reads the field's type into its markers and leaf or nested shape.
func (w *wireWalker) classify(f *wireField, t types.Type, path string, stack []*types.Named) error {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		f.Pointer = true
		t = types.Unalias(p.Elem())
		if _, ok := t.(*types.Pointer); ok {
			return errors.Newf("%s: a pointer to a pointer does not cross the wire", path)
		}
	}
	// A byte slice is one leaf, before the slice markers are read: encoding/json carries
	// it as a base64 string, so *[]byte is a pointer to a leaf and [][]byte a list of it.
	if isByteSlice(t) {
		f.tsLeaf = bytesTSType

		return nil
	}
	if sl, ok := t.(*types.Slice); ok {
		if f.Pointer {
			return errors.Newf("%s: a pointer to a slice does not cross the wire; use the slice", path)
		}
		f.Slice = true
		t = types.Unalias(sl.Elem())
		if p, ok := t.(*types.Pointer); ok {
			f.ElemPointer = true
			t = types.Unalias(p.Elem())
		}
		if isByteSlice(t) {
			f.tsLeaf = bytesTSType

			return nil
		}
		if _, ok := t.(*types.Slice); ok {
			return errors.Newf("%s: a slice of slices does not cross the wire; declare a struct for the inner element", path)
		}
	}

	switch u := t.(type) {
	case *types.Basic:
		leaf, ok, err := w.leaves.resolve(u)
		if err != nil {
			return errors.Wrap(err, path)
		}
		if !ok {
			return errors.Newf("%s: %s has no TypeScript type", path, u)
		}
		f.tsLeaf = leaf.TS

		return nil
	case *types.Named:
		return w.classifyNamed(f, u, path, stack)
	case *types.Struct:
		return errors.Newf("%s: an anonymous struct does not cross the wire; declare a named type", path)
	case *types.Array:
		return errors.Newf("%s: an array does not cross the wire; use a slice", path)
	case *types.Map:
		return errors.Newf("%s: a map does not cross the wire; declare a struct or a slice of structs", path)
	case *types.Interface:
		return errors.Newf("%s: an interface does not cross the wire; declare the concrete type", path)
	default:
		return errors.Newf("%s: %s does not cross the wire", path, typeStringer(t))
	}
}

// classifyNamed resolves a named type: a leaf (a @typescript declaration, a row of the
// built-in table, or a named basic type typed by its underlying type), or a struct,
// which is walked.
func (w *wireWalker) classifyNamed(f *wireField, named *types.Named, path string, stack []*types.Named) error {
	leaf, ok, err := w.leaves.resolveNamed(named)
	if err != nil {
		return errors.Wrap(err, path)
	}
	if ok {
		f.tsLeaf = leaf.TS
		f.tsImport = leaf.Import

		return nil
	}

	switch u := named.Underlying().(type) {
	case *types.Basic:
		return errors.Newf("%s: %s has no TypeScript type", path, typeStringer(named))
	case *types.Struct:
		if slices.Contains(stack, named) {
			return errors.Newf("%s: %s reaches itself; a struct that crosses the wire cannot be recursive", path, typeStringer(named))
		}
		if err := w.guardJSONMethods(named, path); err != nil {
			return err
		}
		nested, err := w.nestedShape(named, u, path, stack)
		if err != nil {
			return err
		}
		f.Nested = nested

		return nil
	default:
		return errors.Newf("%s: %s is a named %s; only named basic types and structs cross the wire", path, typeStringer(named), typeStringer(u))
	}
}

// nestedShape returns the shared shape for a named struct, walking it on first
// sight. Mirror names are the type's name with a lower-case initial, so two
// distinct types that would need the same name are refused.
func (w *wireWalker) nestedShape(named *types.Named, st *types.Struct, path string, stack []*types.Named) (*wireShape, error) {
	if shape, ok := w.shapes[named]; ok {
		return shape, nil
	}

	mirror := lowerFirst(named.Obj().Name())
	if other, taken := w.names[mirror]; taken && other != named {
		if w.columns {
			return nil, errors.Newf("%s: %s and %s would both be the interface %s; rename one", path, typeStringer(named), typeStringer(other), strcase.ToPascal(mirror))
		}

		return nil, errors.Newf("%s: %s and %s would both mirror as %q; rename one", path, typeStringer(named), typeStringer(other), mirror)
	}

	shape := &wireShape{Mirror: mirror, Source: typeStringer(named), named: named}
	w.shapes[named] = shape
	w.names[mirror] = named
	if err := w.fill(shape, st, path, append(stack, named)); err != nil {
		return nil, err
	}
	// Appended after its own fields walked, so everything it reaches precedes it.
	w.order = append(w.order, shape)

	return shape, nil
}

// lowerFirst lowers a name's first rune.
func lowerFirst(s string) string {
	r, width := utf8.DecodeRuneInString(s)

	return string(unicode.ToLower(r)) + s[width:]
}
