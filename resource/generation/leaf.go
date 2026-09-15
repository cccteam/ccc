package generation

import (
	"fmt"
	"go/types"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// One leaf resolution serves every path that carries a Go type to the browser: the
// wire walker, which types RPC requests, RPC results, and computed rows, and the
// column classifier, which types table and view fields. A type resolves, after
// aliases are read through, in this order: a database/sql Null wrapper is refused
// outright, naming the pointer to write instead; then the built-in table by qualified
// name; a generic row by origin (ccc.NullEnum, resolved to its type argument); a
// @typescript declaration on the type; a basic type, or a named type over one, by the
// basic type's row; a byte slice, unnamed or a named slice over byte with no JSON
// methods, as the bytes leaf. A type that reaches none of these is no leaf: the walker
// mirrors it when it is a struct and refuses it otherwise, and the column path derives
// a struct's interface and refuses everything else, naming the fix. Nothing falls back
// to string.
//
// A byte slice is one leaf, never a list of numbers, because that is what the wire
// carries: encoding/json writes a []byte as a base64 string and a nil one as null, and
// reads a base64 string back. The leaf is bytes in the metadata and string in the
// interface (tsDataType). The line is the one valueKindOf draws for the value limits:
// an unnamed slice of byte (uint8 or an alias of it), a named slice type over it, and a
// pointer to either. A byte array ([N]byte) is not on it, since encoding/json writes an
// array as an array; a named type carrying @typescript keeps what it declares, and one
// writing its own JSON (json.RawMessage) describes nothing and is refused as before.

// tsImport is a TypeScript type a @typescript declaration names: the identifier the
// generated file imports and the module it comes from, or a TypeScript built-in when
// From is empty.
type tsImport struct {
	Name string
	From string
}

// IsBuiltin reports whether the declaration names a TypeScript built-in (string,
// number, boolean, unknown), which no file imports.
func (i *tsImport) IsBuiltin() bool {
	return i.From == ""
}

// unknownTSType is the TypeScript type of a value with no fixed shape: spanner.NullJSON,
// and a @typescript(unknown) declaration. Its display type is object.
const unknownTSType = "unknown"

// builtinTypescriptNames are the TypeScript types @typescript may name without from:.
var builtinTypescriptNames = []string{stringTSType, numberTSType, booleanStr, unknownTSType}

// tsLeaf is a resolved leaf: the type the built-in table or the declaration names, and
// the declaration when one supplied it.
type tsLeaf struct {
	// TS is the leaf's TypeScript type as the table spells it (uuid, civilDate, Date,
	// string, ...) or as the declaration names it (Point, unknown).
	TS string
	// Import is the @typescript declaration behind the leaf; nil for a table row.
	Import *tsImport
}

// DisplayType is the leaf's display type in generated metadata: an imported type and
// unknown are one opaque object; every other leaf displays as its own type.
func (l tsLeaf) DisplayType() string {
	return leafDisplayType(l.TS, l.Import)
}

// leafDisplayType is DisplayType over the parts a wireField keeps.
func leafDisplayType(ts string, imported *tsImport) string {
	if ts == unknownTSType || (imported != nil && !imported.IsBuiltin()) {
		return objectDisplayType
	}

	return ts
}

// nullEnumOrigin is the one generic row of the built-in table: ccc.NullEnum[T] resolves
// to its type argument's leaf, as its JSON is the value's JSON or null.
const nullEnumOrigin = "ccc.NullEnum"

// databaseSQLPath is the package whose Null wrappers are refused: every named type it
// declares with the Null prefix, so a wrapper Go adds later is refused without a table
// edit. sql.RawBytes carries no prefix and is a byte slice, the bytes leaf.
const databaseSQLPath = "database/sql"

// sqlNullPrefix is the prefix of the refused database/sql types: the eight NullX
// wrappers and the generic Null[T].
const sqlNullPrefix = "Null"

// validField is the flag field every database/sql Null wrapper carries beside its
// value; the value field is the other one, and names the pointer the refusal offers.
const validField = "Valid"

// ownFixRefusal is a leaf refusal that names the only fix there is, so the column path
// reports it as it stands instead of adding its @typescript clause: the type is
// declared where an application cannot annotate it.
type ownFixRefusal struct {
	msg string
}

// Error is the refusal's message, path-free; the callers add the field's path.
func (e *ownFixRefusal) Error() string {
	return e.msg
}

// refuseSQLNull refuses a database/sql Null wrapper on every path, the callers having
// read one pointer through already: the eight NullX types and the generic Null[T].
// None writes its own JSON, so encoding/json carries the wrapper as {X, Valid}, refuses
// a bare value into it, and reads null as the zero value silently; the field lies in
// both directions where the pointer to the value carries null on the wire and through
// the patch decoder. The pointer is read off the wrapper's value field, the one that is
// not Valid (Int64 on NullInt64, Time on NullTime, V on Null[T] as instantiated). A
// standard-library type cannot carry a @typescript declaration, so the message offers
// none. Returns nil for every other type.
func refuseSQLNull(named *types.Named) error {
	obj := named.Obj()
	if obj.Pkg() == nil || obj.Pkg().Path() != databaseSQLPath || !strings.HasPrefix(obj.Name(), sqlNullPrefix) {
		return nil
	}
	msg := fmt.Sprintf("%s has no JSON form of its own", typeStringer(named))
	value, ok := sqlNullValueField(named)
	if !ok {
		return &ownFixRefusal{msg: msg + "; type a nullable column with a pointer to the value"}
	}

	return &ownFixRefusal{msg: fmt.Sprintf("%s (encoding/json writes it as {%s, %s}); type a nullable column with the pointer *%s", msg, value.Name(), validField, typeStringer(value.Type()))}
}

// sqlNullValueField is the value field of a database/sql Null wrapper: the first field
// of its struct that is not Valid, with the type arguments applied. ok is false when
// the type is no struct or carries no such field.
func sqlNullValueField(named *types.Named) (value *types.Var, ok bool) {
	st, isStruct := named.Underlying().(*types.Struct)
	if !isStruct {
		return nil, false
	}
	for i := range st.NumFields() {
		if field := st.Field(i); field.Name() != validField {
			return field, true
		}
	}

	return nil, false
}

// leafResolver resolves Go types to TypeScript leaves over the built-in table and the
// @typescript declarations a reader supplies.
type leafResolver struct {
	mapped map[string]string
	// declFor reads a type's @typescript declaration, nil when it has none; a nil
	// reader (tests over the table alone) declares nothing.
	declFor func(*types.Named) (*tsImport, error)
}

// newLeafResolver builds a resolver over the built-in table and the declaration reader.
func newLeafResolver(declFor func(*types.Named) (*tsImport, error)) *leafResolver {
	return &leafResolver{mapped: defaultTypescriptOverrides(), declFor: declFor}
}

// resolve resolves t, with no pointer around it and no slice but a byte slice, to a
// leaf. ok is false when t reaches no row and no declaration; err reports a malformed
// declaration.
func (r *leafResolver) resolve(t types.Type) (leaf tsLeaf, ok bool, err error) {
	t = types.Unalias(t)
	switch u := t.(type) {
	case *types.Basic:
		ts, ok := r.mapped[basicName(u)]

		return tsLeaf{TS: ts}, ok, nil
	case *types.Named:
		return r.resolveNamed(u)
	case *types.Slice:
		if isByte(u.Elem()) {
			return tsLeaf{TS: bytesTSType}, true, nil
		}

		return tsLeaf{}, false, nil
	default:
		return tsLeaf{}, false, nil
	}
}

// resolveNamed resolves a named type: a database/sql Null wrapper is refused first,
// then the table by name, then a generic row by origin, then its declaration, then its
// underlying basic type, then a byte slice with no JSON methods. The refusal comes
// before everything so the wrapper never falls through to struct derivation and a
// missing-json-tag message; the table comes next so the library types it maps are
// never read for a declaration they cannot carry; the declaration comes before the
// basic row so a named string may still declare a type, and before the byte slice so a
// named []byte may too.
func (r *leafResolver) resolveNamed(named *types.Named) (tsLeaf, bool, error) {
	if err := refuseSQLNull(named); err != nil {
		return tsLeaf{}, false, err
	}
	if ts, ok := r.mapped[typeStringer(named)]; ok {
		return tsLeaf{TS: ts}, true, nil
	}
	if origin := named.Origin(); origin != named && originName(origin) == nullEnumOrigin && named.TypeArgs().Len() == 1 {
		return r.resolve(named.TypeArgs().At(0))
	}
	if r.declFor != nil {
		decl, err := r.declFor(named)
		if err != nil {
			return tsLeaf{}, false, err
		}
		if decl != nil {
			return tsLeaf{TS: decl.Name, Import: decl}, true, nil
		}
	}
	if basic, ok := named.Underlying().(*types.Basic); ok {
		ts, ok := r.mapped[basicName(basic)]

		return tsLeaf{TS: ts}, ok, nil
	}
	// A named slice over byte is the bytes leaf when encoding/json writes it as one: a
	// type with its own JSON methods (json.RawMessage) writes something else.
	if isByteSlice(named.Underlying()) && !hasJSONMethods(named) {
		return tsLeaf{TS: bytesTSType}, true, nil
	}

	return tsLeaf{}, false, nil
}

// originName is a generic type's qualified name without its type parameters.
func originName(origin *types.Named) string {
	obj := origin.Obj()
	if obj.Pkg() == nil {
		return obj.Name()
	}

	return obj.Pkg().Name() + "." + obj.Name()
}

// basicName is a basic type's table key: byte and rune are read as the kinds they
// alias, so a byte column and a uint8 column share one row.
func basicName(b *types.Basic) string {
	if kind := b.Kind(); kind >= 0 && int(kind) < len(types.Typ) && types.Typ[kind] != nil {
		return types.Typ[kind].Name()
	}

	return b.Name()
}

// columnClass is a table or view field's Go type as the column classifier reads it.
type columnClass struct {
	// Leaf is the field's leaf, when the type reaches one.
	Leaf tsLeaf
	// Slice marks a field carrying a list: a []T, an array, or a named slice type.
	Slice bool
	// Derive is the named struct the field carries when its type reaches no leaf; the
	// column path derives its interface. Nil when Leaf is set.
	Derive *types.Named
	// Carrier is the named type a generated storage method attaches to: the field's
	// type once the pointer is read through, when that type is named (a struct, a
	// named slice, a declared type). Nil for an unnamed slice or a basic type.
	Carrier *types.Named
}

// classifyColumn reads a field's type on the column path: through the alias and one
// pointer, then the named type as a whole (a declaration or a table row wins before
// any slice is stripped, so a named type over []byte stays what it declares, and a
// named byte slice with neither is the bytes leaf), then a byte slice as one leaf, then
// one slice level, then the element's leaf. A struct that reaches no leaf is returned
// for derivation; anything else that reaches none is refused with the message the
// walker uses, and the caller adds the field's path and the fix.
func (r *leafResolver) classifyColumn(t types.Type) (columnClass, error) {
	var class columnClass
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
		if _, ok := t.(*types.Pointer); ok {
			return columnClass{}, errors.New("a pointer to a pointer has no TypeScript type")
		}
	}

	if named, ok := t.(*types.Named); ok {
		class.Carrier = named
		leaf, ok, err := r.resolveNamed(named)
		if err != nil {
			return columnClass{}, err
		}
		if ok {
			class.Leaf = leaf

			return class, nil
		}
		// The runtime marshals the column value as declared, so a named type that
		// writes its own JSON has no shape to read off it, whatever it wraps.
		if hasJSONMethods(named) {
			return columnClass{}, errors.Newf("%s writes its own JSON (MarshalJSON or UnmarshalJSON), so its fields do not describe the wire; add @%s(...) to its declaration", typeStringer(named), typescriptKeyword)
		}
	}

	// A byte slice is one leaf, before any slice is stripped: the wire carries it as a
	// base64 string, never as a list.
	if isByteSlice(t) {
		class.Leaf = tsLeaf{TS: bytesTSType}

		return class, nil
	}

	elem, isSlice := sliceElem(t)
	if isSlice {
		class.Slice = true
		t = types.Unalias(elem)
		if p, ok := t.(*types.Pointer); ok {
			t = types.Unalias(p.Elem())
		}
	}

	// The element as a whole first, as above: a declared type over a slice is what it
	// declares, and a named type writing its own JSON is refused before its shape is
	// read.
	if named, ok := t.(*types.Named); ok {
		leaf, ok, err := r.resolveNamed(named)
		if err != nil {
			return columnClass{}, err
		}
		if ok {
			class.Leaf = leaf

			return class, nil
		}
		if hasJSONMethods(named) {
			return columnClass{}, errors.Newf("%s writes its own JSON (MarshalJSON or UnmarshalJSON), so its fields do not describe the wire; add @%s(...) to its declaration", typeStringer(named), typescriptKeyword)
		}
		if _, isStruct := named.Underlying().(*types.Struct); isStruct {
			class.Derive = named

			return class, nil
		}
	}
	// The element as a byte slice ([][]byte, an ARRAY<BYTES> column) is a list of the
	// bytes leaf; any other inner slice has no interface to name.
	if _, nested := sliceElem(t); nested && isSlice && !isByteSlice(t) {
		return columnClass{}, errors.New("a slice of slices has no TypeScript type; declare a struct for the inner element")
	}

	leaf, ok, err := r.resolve(t)
	if err != nil {
		return columnClass{}, err
	}
	if ok {
		class.Leaf = leaf

		return class, nil
	}

	return columnClass{}, errors.Newf("%s has no TypeScript type", typeStringer(t))
}

// sliceElem reads one slice level off t: a slice, an array, or a named type whose
// underlying type is one of those.
func sliceElem(t types.Type) (elem types.Type, ok bool) {
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return u.Elem(), true
	case *types.Array:
		return u.Elem(), true
	default:
		return nil, false
	}
}

// isListColumn reports whether a table or view field carries a list: its type, aliases
// and one pointer read through, is a slice, an array, or a named type over one, other
// than a byte slice, which is one value (the bytes leaf, whatever it is named). It is
// the column classifier's Slice reading without the leaf resolution, for the tag checks
// that run at extraction, before any TypeScript type is resolved. The one reading the
// two make differently is a named slice type declaring its TypeScript type: one
// declared value to the classifier, a list here, and an ARRAY column either way, which
// is what the tag checks ask about.
func isListColumn(t types.Type) bool {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	if isByteSlice(t.Underlying()) {
		return false
	}
	_, isList := sliceElem(t)

	return isList
}

// isByteSlice reports whether t, aliases read through, is an unnamed slice of byte
// (uint8 or an alias of it): the shape encoding/json writes as a base64 string. A
// named slice type is read through its underlying type by the caller, which also
// checks the type's JSON methods.
func isByteSlice(t types.Type) bool {
	s, ok := types.Unalias(t).(*types.Slice)

	return ok && isByte(s.Elem())
}

// hasJSONMethods reports whether the type writes or reads its own JSON: it declares
// MarshalJSON or UnmarshalJSON on either receiver, so its fields say nothing about the
// wire form.
func hasJSONMethods(named *types.Named) bool {
	return hasMethod(named, "MarshalJSON") || hasMethod(named, "UnmarshalJSON")
}

// hasMethod reports whether the type declares the method on either receiver, itself or
// through an embedded field.
func hasMethod(named *types.Named, name string) bool {
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), true, named.Obj().Pkg(), name)
	_, ok := obj.(*types.Func)

	return ok
}

// tsImportGroup is one import line of a generated TypeScript file: the names imported
// from one module, sorted.
type tsImportGroup struct {
	From  string
	Names []string
}

// NameList renders the group's names for an import clause.
func (g tsImportGroup) NameList() string {
	return strings.Join(g.Names, ", ")
}

// groupImports folds the imported types a file renders into one line per module,
// modules and names sorted, built-ins left out.
func groupImports(imports []*tsImport) []tsImportGroup {
	byModule := make(map[string][]string)
	for _, imp := range imports {
		if imp == nil || imp.IsBuiltin() {
			continue
		}
		if !slices.Contains(byModule[imp.From], imp.Name) {
			byModule[imp.From] = append(byModule[imp.From], imp.Name)
		}
	}

	groups := make([]tsImportGroup, 0, len(byModule))
	for from, names := range byModule {
		slices.Sort(names)
		groups = append(groups, tsImportGroup{From: from, Names: names})
	}
	slices.SortFunc(groups, func(a, b tsImportGroup) int {
		return strings.Compare(a.From, b.From)
	})

	return groups
}
