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

// Package holds a slice of each high level type used in resource generation.
type Package struct {
	Structs    []*Struct
	NamedTypes []*NamedType
}

// TypeInfo provides convience methods over a go/types' Object.
type TypeInfo struct {
	obj types.Object
}

// Name is the type's local object name.
func (t *TypeInfo) Name() string {
	return t.obj.Name()
}

// Type is the qualified type name.
// e.g. ccc.UUID, []ccc.UUID
func (t *TypeInfo) Type() string {
	return typeStringer(t.obj.Type())
}

// DerefType returns non-pointer qualified type.
// e.g. *ccc.UUID -> ccc.UUID
func (t *TypeInfo) DerefType() string {
	return typeStringer(derefType(t.obj.Type()))
}

// UnqualifiedType is the type name without package prefix.
// e.g. ccc.UUID -> UUID, []ccc.UUID -> []UUID
func (t *TypeInfo) UnqualifiedType() string {
	qualifier := func(*types.Package) string {
		return ""
	}

	return types.TypeString(t.obj.Type(), qualifier)
}

// DerefUnqualifiedType is the non-pointer type name without package prefix.
// e.g. *ccc.UUID -> UUID
func (t *TypeInfo) DerefUnqualifiedType() string {
	qualifier := func(*types.Package) string {
		return ""
	}

	return types.TypeString(derefType(t.obj.Type()), qualifier)
}

// TypeName is the qualified type name without array/slice/pointer prefix.
// e.g. *ccc.UUID -> ccc.UUID, []ccc.UUID -> ccc.UUID
func (t *TypeInfo) TypeName() string {
	return typeStringer(unwrapType(t.obj.Type()))
}

// UnqualifiedTypeName is the type name without array/slice/pointer or package prefix.
// e.g. *ccc.UUID -> UUID, []ccc.UUID -> UUID
func (t *TypeInfo) UnqualifiedTypeName() string {
	qualifier := func(*types.Package) string {
		return ""
	}

	return types.TypeString(unwrapType(t.obj.Type()), qualifier)
}

// IsPointer returns true if the declaration is a pointer
func (t *TypeInfo) IsPointer() bool {
	switch t.obj.Type().(type) {
	case *types.Pointer:
		return true
	default:
		return false
	}
}

// IsIterable returns true if type is slice or array
func (t *TypeInfo) IsIterable() bool {
	switch t.obj.Type().(type) {
	case *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}

// Interface is an abstraction over types.Interface
type Interface struct {
	Name      string
	named     *types.Named
	isGeneric bool
}

// Struct is an abstraction combining types.Struct and ast.StructType for simpler parsing.
type Struct struct {
	*TypeInfo
	astInfo    *ast.StructType
	fields     []*Field
	interfaces []string
	methodSet  map[string]struct{}
	comments   string
}

func newStruct(obj types.Object) *Struct {
	tt := obj.Type()

	st, ok := decodeToType[*types.Struct](tt)
	if !ok {
		return &Struct{}
	}

	s := &Struct{
		TypeInfo:  &TypeInfo{obj},
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

		s.fields = append(s.fields, &Field{
			TypeInfo:    TypeInfo{field},
			tags:        reflect.StructTag(st.Tag(i)),
			isLocalType: isTypeLocalToPackage(field, obj.Pkg()),
		})
	}

	return s
}

// Comments returns the godoc comment text on the struct's type declaration
func (s *Struct) Comments() string {
	return s.comments
}

// Pos returns the position of the struct keyword in its fileset.
func (s *Struct) Pos() token.Pos {
	return s.astInfo.Struct
}

func (s *Struct) setInterface(iface string) {
	if !slices.Contains(s.interfaces, iface) {
		s.interfaces = append(s.interfaces, iface)
	}
}

// Implements returns true if the interface's name matches a name in the set of interfaces the Struct satisfies.
func (s *Struct) Implements(interfaceName string) bool {
	return slices.Contains(s.interfaces, interfaceName)
}

func (s *Struct) String() string {
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

// PrintWithFieldError pretty-formats the struct, highlighting a field with an error message.
func (s *Struct) PrintWithFieldError(fieldIndex int, errMsg string) string {
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

// PrintErrors pretty-prints the struct annotated with field errors
// if any have been stored with AddFieldError.
func (s *Struct) PrintErrors() string {
	if !s.HasErrors() {
		return ""
	}

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

	msg := strings.Builder{}
	msg.WriteString(fmt.Sprintf("type %s struct {\n", s.Name()))
	for _, field := range s.fields {
		nameTabs := strings.Repeat("\t", numNameTabs-(len(field.Name())/8))
		typeTabs := strings.Repeat("\t", numTypeTabs-(len(field.Type())/8))

		if len(field.errs) == 0 {
			msg.WriteString("\t")
			msg.WriteString(field.Name() + nameTabs)
			msg.WriteString(field.Type() + typeTabs)
			msg.WriteString(string(field.tags))
			msg.WriteString("\n")

			continue
		}

		msg.WriteString("\033[91m\t")
		msg.WriteString(field.Name() + nameTabs)
		msg.WriteString(field.Type() + typeTabs)
		msg.WriteString(string(field.tags))
		msg.WriteString(" << ")
		if len(field.errs) > 1 {
			msg.WriteString(fmt.Sprintf("[%d] ", len(field.errs)))
		}
		msg.WriteString("[")
		for j, fieldError := range field.errs {
			if j > 0 {
				msg.WriteString(", ")
			}
			msg.WriteString(fieldError)

			if j == len(field.errs)-1 {
				msg.WriteString("]\033[0m\n")
			}
		}
	}
	msg.WriteString("}")

	return msg.String()
}

// HasErrors returns true if a field error has been stored by AddFieldError
func (s *Struct) HasErrors() bool {
	for _, field := range s.fields {
		if len(field.errs) != 0 {
			return true
		}
	}

	return false
}

// NumFields is the number of fields in the struct's type declaration.
func (s *Struct) NumFields() int {
	return len(s.fields)
}

// Fields returns a slice of the fields belonging to the struct.
func (s *Struct) Fields() []*Field {
	return s.fields
}

// HasMethod returns true if the method name matches a name in the set of methods belonging to the struct.
func (s *Struct) HasMethod(methodName string) bool {
	_, ok := s.methodSet[methodName]

	return ok
}

// Field is an abstraction combining types.Var and ast.Field for simpler parsing.
type Field struct {
	TypeInfo
	astInfo     *ast.Field
	tags        reflect.StructTag
	comments    string
	isLocalType bool
	errs        []string
}

// AddError stores an error message to be printed in the parent struct's PrintErrors method
func (f *Field) AddError(msg string) {
	f.errs = append(f.errs, msg)
}

func (f Field) String() string {
	return fmt.Sprintf("%s\t\t%s\t\t%s", f.Name(), f.Type(), f.tags)
}

// LookupTag returns a struct tag's value and whether or not the struct tag exists on this field.
func (f Field) LookupTag(key string) (string, bool) {
	return f.tags.Lookup(key)
}

// HasTag returns true if this field has a matching struct tag.
func (f Field) HasTag(key string) bool {
	_, ok := f.tags.Lookup(key)

	return ok
}

// AsStruct converts this Field to a Struct. Returns nil if the Field's type is not a struct type.
func (f Field) AsStruct() *Struct {
	s := newStruct(f.obj)

	if s.obj == nil {
		return nil
	}

	return s
}

// IsLocalType returns true if this Field's type originates from the same package
// its parent struct is defined in.
func (f Field) IsLocalType() bool {
	return f.isLocalType
}

// ResolvedType returns this Field's unqualified type if it's local, or its qualified type otherwise.
func (f Field) ResolvedType() string {
	if f.IsLocalType() {
		return f.UnqualifiedType()
	}

	return f.Type()
}

// DerefResolvedType returns this Field's unqualified type if it's local, or its qualified type otherwise.
func (f Field) DerefResolvedType() string {
	if f.IsLocalType() {
		return f.DerefUnqualifiedType()
	}

	return f.DerefType()
}

// Comments returns the godoc comment text on the field's declaration.
func (f Field) Comments() string {
	return f.comments
}

// OriginType returns the origin type if this Field's type is a generic instantiation.
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

// TypeArgs returns any type arguments if this field's type is a generic instantiation.
// e.g. ccc.Foo[bool] -> bool, ccc.Foo[ccc.UUID] -> ccc.UUID
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

// NamedType is any distinct named type
// e.g. type MyNamedType string, type MyOtherNamedType ccc.UUID
type NamedType struct {
	TypeInfo
	Comments string
}
