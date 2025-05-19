package parser

import (
	"fmt"
	"go/ast"
	"go/types"
	"log"
	"reflect"
	"slices"
	"strings"
)

type TypeInfo struct {
	obj       types.Object
	name      string
	tt        types.Type
	pkg       *types.Package
	position  int
	unwrapped bool
}

func newType(obj types.Object, unwrap bool) *TypeInfo {
	tt := obj.Type()
	if unwrap {
		tt = unwrapType(tt)
	}

	return &TypeInfo{
		obj:       obj,
		name:      obj.Name(),
		tt:        tt,
		pkg:       obj.Pkg(),
		position:  int(obj.Pos()),
		unwrapped: unwrap,
	}
}

func (t *TypeInfo) Name() string {
	return t.name
}

// e.g. ccc.UUID, []ccc.UUID
func (t *TypeInfo) Type() string {
	return typeStringer(t.tt)
}

// Type without package prefix.
// e.g. ccc.UUID -> UUID, []ccc.UUID -> []UUID
func (t *TypeInfo) UnqualifiedType() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(t.tt, qualifier)
}

// Qualified type without array/slice/pointer prefix.
// e.g. *ccc.UUID -> ccc.UUID, []ccc.UUID -> ccc.UUID
func (t *TypeInfo) TypeName() string {
	return typeStringer(unwrapType(t.tt))
}

// Type without array/slice/pointer or package prefix.
// e.g. *ccc.UUID -> UUID, []ccc.UUID -> UUID
func (t *TypeInfo) UnqualifiedTypeName() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(unwrapType(t.tt), qualifier)
}

func (t *TypeInfo) PackageName() string {
	return t.pkg.Name()
}

// Position in the Package the type object was parsed from
func (t *TypeInfo) Position() int {
	return t.position
}

func (t *TypeInfo) IsPointer() bool {
	switch t.tt.(type) {
	case *types.Pointer:
		return true
	default:
		return false
	}
}

// Returns true if type is slice or array
func (t *TypeInfo) IsIterable() bool {
	switch t.tt.(type) {
	case *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}

// TODO: this method is only used in templates to retrieve a struct's fields.
// the ok boolean should not be ignored. maybe replace this method with an iterator over fields
// if the type is a struct.
func (t *TypeInfo) AsStruct() *Struct {
	return newStruct(t.obj)
}

type Struct struct {
	*TypeInfo
	astInfo    *ast.StructType
	fields     []*Field
	localTypes []*TypeInfo
	interfaces []string
	comments   string
}

func newStruct(obj types.Object) *Struct {
	tt := obj.Type()

	st, ok := decodeToType[*types.Struct](tt)
	if !ok {
		return nil
	}

	s := &Struct{
		TypeInfo:   newType(obj, true),
		localTypes: localTypesFromStruct(obj, map[string]struct{}{}),
	}

	for i := range st.NumFields() {
		field := st.Field(i)

		s.fields = append(s.fields, &Field{
			TypeInfo:    newType(field, false),
			tags:        reflect.StructTag(st.Tag(i)),
			isLocalType: isTypeLocalToPackage(field, obj.Pkg()),
		})
	}

	return s
}

func (s *Struct) Comments() string {
	return s.comments
}

func (s *Struct) SetInterface(iface string) {
	if !slices.Contains(s.interfaces, iface) {
		s.interfaces = append(s.interfaces, iface)
	}
}

func (s *Struct) Implements(iface string) bool {
	return slices.Contains(s.interfaces, iface)
}

// Pretty prints the struct name and its fields. Useful for debugging.
func (s *Struct) String() string {
	var (
		maxNameLength int
		maxTypeLength int
	)

	for _, field := range s.fields {
		maxNameLength = max(len(field.name), maxNameLength)
		maxTypeLength = max(len(field.Type()), maxTypeLength)
	}

	numNameTabs := maxNameLength/8 + 1
	numTypeTabs := maxTypeLength/8 + 1

	var fields string
	for _, field := range s.fields {
		nameTabs := strings.Repeat("\t", numNameTabs-(len(field.name)/8))
		typeTabs := strings.Repeat("\t", numTypeTabs-(len(field.Type())/8))
		fields += fmt.Sprintf("\t%s%s%s%s%s\n", field.name, nameTabs, field.Type(), typeTabs, field.tags)
	}

	return fmt.Sprintf("type %s struct {\n%s}", s.name, fields)
}

func (s *Struct) PrintWithFieldError(fieldIndex int, errMsg string) string {
	var (
		maxNameLength int
		maxTypeLength int
	)

	for _, field := range s.fields {
		maxNameLength = max(len(field.name), maxNameLength)
		maxTypeLength = max(len(field.Type()), maxTypeLength)
	}

	numNameTabs := maxNameLength/8 + 1
	numTypeTabs := maxTypeLength/8 + 1

	var fields string
	for i, field := range s.fields {
		nameTabs := strings.Repeat("\t", numNameTabs-(len(field.name)/8))
		typeTabs := strings.Repeat("\t", numTypeTabs-(len(field.Type())/8))
		if i == fieldIndex {
			fields += fmt.Sprintf("\033[91m\t%s%s%s%s%s << %s\033[0m\n", field.name, nameTabs, field.Type(), typeTabs, field.tags, errMsg)
		} else {
			fields += fmt.Sprintf("\t%s%s%s%s%s\n", field.name, nameTabs, field.Type(), typeTabs, field.tags)
		}

	}

	return fmt.Sprintf("type %s struct {\n%s}", s.name, fields)
}

func (s *Struct) Name() string {
	return s.name
}

func (s *Struct) NumFields() int {
	return len(s.fields)
}

func (s *Struct) Fields() []*Field {
	return s.fields
}

func (s *Struct) LocalTypes() []*TypeInfo {
	return s.localTypes
}

type Field struct {
	*TypeInfo
	astInfo     *ast.Field
	tags        reflect.StructTag
	comments    string
	isLocalType bool
}

func (f *Field) String() string {
	return fmt.Sprintf("%s\t\t%s\t\t%s", f.name, f.Type(), f.tags)
}

func (f *Field) LookupTag(key string) (string, bool) {
	return f.tags.Lookup(key)
}

func (f Field) HasTag(key string) bool {
	_, ok := f.tags.Lookup(key)

	return ok
}

// Returns true if the field's type originates from the same package
// its parent struct is defined in.
func (f *Field) IsLocalType() bool {
	return f.isLocalType
}

// Returns the field's unqualified type if it's local, and the qualified type otherwise.
func (f *Field) ResolvedType() string {
	if f.IsLocalType() {
		return f.UnqualifiedType()
	}

	return f.Type()
}

func (f *Field) Comments() string {
	return f.comments
}

func (f *Field) addMetadata(field *ast.Field) {
	if indexExpr, ok := decodeToExpr[*ast.IndexExpr](field.Type); ok {
		if ident, ok := decodeToExpr[*ast.Ident](indexExpr.Index); ok {
			log.Printf("field %q type=%q indexExpr[index]=%q\n", f.name, f.Type(), ident.Name)
		}
	}

	if field.Doc != nil {
		f.comments = field.Doc.Text()
	}
	if field.Comment != nil {
		f.comments += field.Comment.Text()
	}
}
