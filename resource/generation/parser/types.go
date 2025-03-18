package parser

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"
)

type Type struct {
	name     string
	tt       types.Type
	pkg      *types.Package
	position int
}

type Typ = Type

func newType(o types.Object) Type {
	return Type{
		name:     o.Name(),
		tt:       o.Type(),
		pkg:      o.Pkg(),
		position: int(o.Pos()),
	}
}

func (t Type) Name() string {
	return t.name
}

// e.g. ccc.UUID, []ccc.UUID
func (t Type) Type() string {
	return typeStringer(t.tt)
}

// Type without package prefix.
// e.g. ccc.UUID -> UUID, []ccc.UUID -> []UUID
func (t Type) UnqualifiedType() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(t.tt, qualifier)
}

// Qualified type without array/slice/pointer prefix.
// e.g. *ccc.UUID -> ccc.UUID, []ccc.UUID -> ccc.UUID
func (t Type) TypeName() string {
	return typeStringer(unwrapType(t.tt))
}

// Type without array/slice/pointer or package prefix.
// e.g. *ccc.UUID -> UUID, []ccc.UUID -> UUID
func (t Type) UnqualifiedTypeName() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(unwrapType(t.tt), qualifier)
}

func (t Type) PackageName() string {
	return t.pkg.Name()
}

// Position in the Package the type object was parsed from
func (t Type) Position() int {
	return t.position
}

func (t Type) IsStruct() bool {
	return isUnderlyingTypeStruct(t.tt)
}

func (t Type) IsPointer() bool {
	switch t.tt.(type) {
	case *types.Pointer:
		return true
	default:
		return false
	}
}

// Returns true if type is slice or array
func (t Type) IsIterable() bool {
	switch t.tt.(type) {
	case *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}

func (t Type) ToStructType() Struct {
	if t.IsStruct() {
		st, _ := decodeToType[*types.Struct](t.tt)
		pStruct := Struct{
			Typ:        t,
			methods:    structMethods(t.tt),
			localTypes: localTypesFromStruct(t.pkg, t.tt, map[string]struct{}{}),
		}

		for i := range st.NumFields() {
			field := st.Field(i)

			sField := Field{
				Typ:  Type{name: field.Name(), tt: field.Type(), pkg: t.pkg},
				tags: reflect.StructTag(st.Tag(i)),
			}

			pStruct.fields = append(pStruct.fields, sField)
		}

		return pStruct
	}

	return Struct{}
}

type Struct struct {
	Typ
	fields     []Field
	methods    []*types.Selection
	localTypes []Type
}

func newStruct(o types.Object) (Struct, bool) {
	st, ok := decodeToType[*types.Struct](o.Type())
	if !ok {
		return Struct{}, false
	}

	s := Struct{
		Typ:        newType(o),
		methods:    structMethods(o.Type()),
		localTypes: localTypesFromStruct(o.Pkg(), o.Type(), map[string]struct{}{}),
	}

	for i := range st.NumFields() {
		fieldVar := st.Field(i)

		field := Field{
			Typ:         Type{name: fieldVar.Name(), tt: fieldVar.Type(), pkg: o.Pkg()},
			tags:        reflect.StructTag(st.Tag(i)),
			isLocalType: isTypeLocalToPackage(fieldVar, o.Pkg()),
		}

		s.fields = append(s.fields, field)
	}

	return s, true
}

// Pretty prints the struct name and its fields. Useful for debugging.
func (s Struct) String() string {
	var fieldNames []string
	for _, field := range s.fields {
		fieldNames = append(fieldNames, field.name)
	}
	return fmt.Sprintf(`struct {name: %q, fields: %v}`, s.name, fieldNames)
}

func (s Struct) Name() string {
	return s.name
}

func (s Struct) Fields() []Field {
	return s.fields
}

func (s Struct) LocalTypes() []Type {
	return s.localTypes
}

type Field struct {
	Typ
	tags        reflect.StructTag
	isLocalType bool
}

func (f Field) LookupTag(key string) (string, bool) {
	return f.tags.Lookup(key)
}

// Returns true if the field's type originates from the same package
// its parent struct is defined in.
func (f Field) IsLocalType() bool {
	return f.isLocalType
}
