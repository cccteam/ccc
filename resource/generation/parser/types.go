package parser

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
)

type Package struct {
	Structs    []Struct
	NamedTypes []NamedType
	Fset       *token.FileSet
}

type TypeInfo struct {
	obj types.Object
}

func (t TypeInfo) Name() string {
	return t.obj.Name()
}

// e.g. ccc.UUID, []ccc.UUID
func (t TypeInfo) Type() string {
	return typeStringer(t.obj.Type())
}

// e.g. *ccc.UUID -> ccc.UUID
func (t TypeInfo) DerefType() string {
	return typeStringer(derefType(t.obj.Type()))
}

// Type without package prefix.
// e.g. ccc.UUID -> UUID, []ccc.UUID -> []UUID
func (t TypeInfo) UnqualifiedType() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(t.obj.Type(), qualifier)
}

// Type without pointer and package prefix removed
// e.g. *ccc.UUID -> UUID
func (t TypeInfo) DerefUnqualifiedType() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(derefType(t.obj.Type()), qualifier)
}

// Qualified type without array/slice/pointer prefix.
// e.g. *ccc.UUID -> ccc.UUID, []ccc.UUID -> ccc.UUID
func (t TypeInfo) TypeName() string {
	return typeStringer(unwrapType(t.obj.Type()))
}

// Type without array/slice/pointer or package prefix.
// e.g. *ccc.UUID -> UUID, []ccc.UUID -> UUID
func (t TypeInfo) UnqualifiedTypeName() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(unwrapType(t.obj.Type()), qualifier)
}

func (t TypeInfo) IsPointer() bool {
	switch t.obj.Type().(type) {
	case *types.Pointer:
		return true
	default:
		return false
	}
}

// Returns true if type is slice or array
func (t TypeInfo) IsIterable() bool {
	switch t.obj.Type().(type) {
	case *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}

type Interface struct {
	Name  string
	iface *types.Interface
}

type Struct struct {
	TypeInfo
	astInfo    *ast.StructType
	fields     []Field
	interfaces []string
	methodSet  map[string]struct{}
	comments   string
}

func newStruct(obj types.Object, fset *token.FileSet) Struct {
	tt := obj.Type()

	if fset == nil {
		fset = token.NewFileSet()
	}

	st, ok := decodeToType[*types.Struct](tt)
	if !ok {
		return Struct{}
	}

	s := Struct{
		TypeInfo:  TypeInfo{obj},
		methodSet: make(map[string]struct{}),
	}

	methodSet := types.NewMethodSet(types.NewPointer(tt))
	for method := range methodSet.Methods() {
		kind := method.Kind()
		if kind != types.MethodVal {
			continue
		}

		name := method.Obj().Name()
		s.methodSet[name] = struct{}{}
	}

	for i := range st.NumFields() {
		field := st.Field(i)

		s.fields = append(s.fields, Field{
			TypeInfo:    TypeInfo{field},
			tags:        reflect.StructTag(st.Tag(i)),
			isLocalType: isTypeLocalToPackage(field, obj.Pkg()),
		})
	}

	return s
}

func (s Struct) Comments() string {
	return s.comments
}

func (s *Struct) SetInterface(iface string) {
	if !slices.Contains(s.interfaces, iface) {
		s.interfaces = append(s.interfaces, iface)
	}
}

func (s Struct) Implements(iface string) bool {
	return slices.Contains(s.interfaces, iface)
}

// Pretty prints the struct name and its fields. Useful for debugging.
func (s Struct) String() string {
	var (
		maxNameLength int
		maxTypeLength int
	)

	for _, field := range s.fields {
		maxNameLength = max(len(field.Name()), maxNameLength)
		maxTypeLength = max(len(field.Type()), maxTypeLength)
	}

	numNameTabs := maxNameLength/8 + 1
	numTypeTabs := maxTypeLength/8 + 1

	var fields string
	for _, field := range s.fields {
		nameTabs := strings.Repeat("\t", numNameTabs-(len(field.Name())/8))
		typeTabs := strings.Repeat("\t", numTypeTabs-(len(field.Type())/8))
		fields += fmt.Sprintf("\t%s%s%s%s%s\n", field.Name(), nameTabs, field.Type(), typeTabs, field.tags)
	}

	return fmt.Sprintf("type %s struct {\n%s}", s.Name(), fields)
}

func (s Struct) PrintWithFieldError(fieldIndex int, errMsg string) string {
	var (
		maxNameLength int
		maxTypeLength int
	)

	for _, field := range s.fields {
		maxNameLength = max(len(field.Name()), maxNameLength)
		maxTypeLength = max(len(field.Type()), maxTypeLength)
	}

	numNameTabs := maxNameLength/8 + 1
	numTypeTabs := maxTypeLength/8 + 1

	var fields string
	for i, field := range s.fields {
		nameTabs := strings.Repeat("\t", numNameTabs-(len(field.Name())/8))
		typeTabs := strings.Repeat("\t", numTypeTabs-(len(field.Type())/8))
		if i == fieldIndex {
			fields += fmt.Sprintf("\033[91m\t%s%s%s%s%s << %s\033[0m\n", field.Name(), nameTabs, field.Type(), typeTabs, field.tags, errMsg)
		} else {
			fields += fmt.Sprintf("\t%s%s%s%s%s\n", field.Name(), nameTabs, field.Type(), typeTabs, field.tags)
		}

	}

	return fmt.Sprintf("type %s struct {\n%s}", s.Name(), fields)
}

func (s Struct) NumFields() int {
	return len(s.fields)
}

func (s Struct) Fields() []Field {
	return s.fields
}

func (s Struct) Error(fset *token.FileSet) string {
	return fmt.Sprintf("%s at %s", s.Name(), fset.Position(s.astInfo.Pos()))
}

func (s Struct) HasMethod(methodName string) bool {
	_, ok := s.methodSet[methodName]

	return ok
}

type Field struct {
	TypeInfo
	astInfo     *ast.Field
	tags        reflect.StructTag
	comments    string
	isLocalType bool
}

func (f Field) String() string {
	return fmt.Sprintf("%s\t\t%s\t\t%s", f.Name(), f.Type(), f.tags)
}

func (f Field) LookupTag(key string) (string, bool) {
	return f.tags.Lookup(key)
}

func (f Field) HasTag(key string) bool {
	_, ok := f.tags.Lookup(key)

	return ok
}

func (f Field) AsStruct() *Struct {
	s := newStruct(f.obj, nil)

	if s.TypeInfo.obj == nil {
		return nil
	}

	return &s
}

// Returns true if the field's type originates from the same package
// its parent struct is defined in.
func (f Field) IsLocalType() bool {
	return f.isLocalType
}

// Returns the field's unqualified type if it's local, and the qualified type otherwise.
func (f Field) ResolvedType() string {
	if f.IsLocalType() {
		return f.UnqualifiedType()
	}

	return f.Type()
}

// Returns the field's unqualified type if it's local, and the qualified type otherwise.
func (f Field) DerefResolvedType() string {
	if f.IsLocalType() {
		return f.DerefUnqualifiedType()
	}

	return f.DerefType()
}

func (f Field) Comments() string {
	return f.comments
}

// If the type is a generic instantiation, returns the origin of the generic type.
// e.g. ccc.Foo[bool] returns ccc.Foo
func (f Field) OriginType() string {
	indexExpr, ok := decodeToExpr[*ast.IndexExpr](f.astInfo.Type)
	if !ok {
		return f.Type()
	}

	typeIdent, ok := decodeToExpr[*ast.Ident](indexExpr.X)
	if ok {
		return typeIdent.String()
	}

	return f.Type()
}

func (f Field) TypeArgs() string {
	indexExpr, ok := decodeToExpr[*ast.IndexExpr](f.astInfo.Type)
	if !ok {
		return ""
	}

	typeArgIdent, ok := decodeToExpr[*ast.Ident](indexExpr.Index)
	if ok {
		return typeArgIdent.String()
	}

	return ""
}

func (f Field) Error(fset *token.FileSet) string {
	return fmt.Sprintf("%s at %s", f.Name(), fset.Position(f.astInfo.Pos()))
}

type NamedType struct {
	TypeInfo
	Comments string
}
