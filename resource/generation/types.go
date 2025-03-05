package generation

import (
	"fmt"
	"go/types"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource"

	"github.com/ettle/strcase"
)

type Generator interface {
	Generate() error
	Close()
}

var tokenizeRegex = regexp.MustCompile(`(TOKENIZE_[^)]+)\(([^)]+)\)`)

const (
	genPrefix = "zz_gen"
)

type ConstraintType string

const (
	PrimaryKey ConstraintType = "PRIMARY KEY"
	ForeignKey ConstraintType = "FOREIGN KEY"
)

type HandlerType string

const (
	List  HandlerType = "list"
	Read  HandlerType = "read"
	Patch HandlerType = "patch"
)

func (h HandlerType) template() string {
	switch h {
	case Read:
		return readTemplate
	case List:
		return listTemplate
	case Patch:
		return patchTemplate
	default:
		panic(fmt.Sprintf("template(): unknown handler type: %s", h))
	}
}

func (h HandlerType) Method() string {
	switch h {
	case Read, List:
		return "GET"
	case Patch:
		return "PATCH"
	default:
		panic(fmt.Sprintf("Method(): unknown handler type: %s", h))
	}
}

type OptionType string

const (
	Regenerate OptionType = "regenerate"
	NoGenerate OptionType = "nogenerate"
)

type PatchType string

const (
	CreatePatch PatchType = "Create"
	UpdatePatch PatchType = "Update"
)

type GeneratedFileDeleteMethod int

const (
	// Used to remove files with the genPrefix value instead of reading the contents of the file.
	Prefix GeneratedFileDeleteMethod = iota
	// Used to remove files that contain the header comment "// Code generated by resourcegeneration. DO NOT EDIT."
	HeaderComment
)

const (
	querySetOutputFileName        = "types.go"
	resourceInterfaceOutputName   = "resources_iface"
	resourcesTestFileName         = "resource_types_test.go"
	routesOutputName              = "routes"
	routerTestOutputName          = "routes_test"
	consolidatedHandlerOutputName = "consolidated_handler"
)

var rpcMethods = [...]string{"Method", "Execute"}

type searchIndex struct {
	Name       string
	SearchType string
}

type InformationSchemaResult struct {
	TableName            string  `spanner:"TABLE_NAME"`
	ColumnName           string  `spanner:"COLUMN_NAME"`
	ConstraintName       *string `spanner:"CONSTRAINT_NAME"`
	IsPrimaryKey         bool    `spanner:"IS_PRIMARY_KEY"`
	IsForeignKey         bool    `spanner:"IS_FOREIGN_KEY"`
	ReferencedTable      *string `spanner:"REFERENCED_TABLE"`
	ReferencedColumn     *string `spanner:"REFERENCED_COLUMN"`
	SpannerType          string  `spanner:"SPANNER_TYPE"`
	IsNullable           bool    `spanner:"IS_NULLABLE"`
	IsView               bool    `spanner:"IS_VIEW"`
	IsIndex              bool    `spanner:"IS_INDEX"`
	IsUniqueIndex        bool    `spanner:"IS_UNIQUE_INDEX"`
	GenerationExpression *string `spanner:"GENERATION_EXPRESSION"`
	OrdinalPosition      int64   `spanner:"ORDINAL_POSITION"`
	KeyOrdinalPosition   int64   `spanner:"KEY_ORDINAL_POSITION"`
}

type tableMetadata struct {
	Columns       map[string]columnMeta
	SearchIndexes map[string][]*expressionField
	IsView        bool
	PkCount       int
}

type columnMeta struct {
	ColumnName         string
	ConstraintTypes    []ConstraintType
	IsPrimaryKey       bool
	IsForeignKey       bool
	SpannerType        string
	IsNullable         bool
	IsIndex            bool
	IsUniqueIndex      bool
	OrdinalPosition    int64
	KeyOrdinalPosition int64
	ReferencedTable    string
	ReferencedColumn   string
}

type generationOption struct {
	option  OptionType
	handler HandlerType
}

type generatedHandler struct {
	template    string
	handlerType HandlerType
}

type generatedRoute struct {
	Method      string
	Path        string
	HandlerFunc string
}

type parsedType struct {
	name        string
	tt          types.Type
	packageName string
	position    int
}

func (p parsedType) Name() string {
	return p.name
}

// e.g. ccc.UUID, []ccc.UUID
func (p parsedType) Type() string {
	return typeStringer(p.tt)
}

// Returns type without package prefix.
// e.g. ccc.UUID -> UUID, []ccc.UUID -> []UUID
func (p parsedType) UnqualifiedType() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(p.tt, qualifier)
}

// Returns unwrapped type.
// e.g. ccc.UUID -> ccc.UUID, []ccc.UUID -> ccc.UUID
func (p parsedType) TypeName() string {
	return typeStringer(unwrapType(p.tt))
}

// Returns unwrapped and unqualified type as string.
// e.g. ccc.UUID -> UUID, []ccc.UUID -> UUID
func (p parsedType) UnqualifiedTypeName() string {
	qualifier := func(p *types.Package) string {
		return ""
	}

	return types.TypeString(unwrapType(p.tt), qualifier)
}

func (p parsedType) PackageName() string {
	return p.packageName
}

func (p parsedType) Position() int {
	return p.position
}

func (p parsedType) IsStruct() bool {
	return isUnderlyingTypeStruct(p.tt)
}

func (p parsedType) IsIterable() bool {
	switch p.tt.(type) {
	case *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}

func (p parsedType) ToStructType() parsedStruct {
	if p.IsStruct() {
		st, _ := decodeToType[*types.Struct](p.tt)
		pStruct := parsedStruct{
			parsedType: p,
			methods:    structMethods(p.tt),
			localTypes: localTypesFromStruct(p.packageName, p.tt, map[string]struct{}{}),
		}

		for i := range st.NumFields() {
			field := st.Field(i)

			sField := structField{
				parsedType: parsedType{name: field.Name(), tt: field.Type(), packageName: p.packageName},
				tags:       reflect.StructTag(st.Tag(i)),
			}

			pStruct.fields = append(pStruct.fields, sField)
		}

		return pStruct
	}

	return parsedStruct{}
}

type parsedStruct struct {
	parsedType
	fields     []structField
	methods    []*types.Selection
	localTypes []parsedType
}

func (p parsedStruct) String() string {
	var fieldNames []string
	for _, field := range p.fields {
		fieldNames = append(fieldNames, field.name)
	}
	return fmt.Sprintf(`struct {name: %q, fields: %v}`, p.name, fieldNames)
}

func (p parsedStruct) Name() string {
	return p.name
}

func (p parsedStruct) Fields() []structField {
	return p.fields
}

func (p parsedStruct) LocalTypes() []parsedType {
	return p.localTypes
}

type structField struct {
	parsedType
	tags        reflect.StructTag
	isLocalType bool
}

func (s structField) JSONTag() string {
	caser := strcase.NewCaser(false, nil, nil)
	camelCaseName := caser.ToCamel(s.Name())

	return fmt.Sprintf("json:%q", camelCaseName)
}

func (s structField) IsLocalType() bool {
	return s.isLocalType
}

type routeMap map[string][]generatedRoute

func (r routeMap) Resources() []string {
	resources := []string{}
resourceRange:
	for resource := range r {
		for _, route := range r[resource] {
			if route.Method == "POST" {
				continue resourceRange
			}
		}
		resources = append(resources, resource)
	}

	slices.Sort(resources)

	return resources
}

type rpcMethodInfo struct {
	parsedStruct
}

type resourceInfo struct {
	parsedType
	Fields                []*resourceField
	searchIndexes         map[string][]*expressionField // Search Indexes are hidden columns in Spanner that are not present in Go struct definitions
	IsView                bool                          // Determines how CreatePatch is rendered in resource generation.
	HasCompoundPrimaryKey bool                          // Determines how CreatePatchSet is rendered in resource generation.
	IsConsolidated        bool
}

func (r *resourceInfo) SearchIndexes() []*searchIndex {
	typeIndexMap := make(map[resource.FilterType]string)
	for searchIndex, expressionFields := range r.searchIndexes {
		for _, exprField := range expressionFields {
			typeIndexMap[exprField.tokenType] = searchIndex
		}
	}

	var indexes []*searchIndex
	for tokenType, indexName := range typeIndexMap {
		indexes = append(indexes, &searchIndex{
			Name:       indexName,
			SearchType: string(tokenType),
		})
	}

	return indexes
}

func (r *resourceInfo) PrimaryKeyIsUUID() bool {
	for _, f := range r.Fields {
		if f.IsPrimaryKey {
			return f.Type() == "ccc.UUID"
		}
	}

	return false
}

func (r *resourceInfo) PrimaryKeyType() string {
	for _, f := range r.Fields {
		if f.IsPrimaryKey {
			return f.Type()
		}
	}

	return ""
}

type resourceField struct {
	*structField
	Parent         *resourceInfo
	typescriptType string
	// Spanner stuff
	IsPrimaryKey       bool
	IsForeignKey       bool
	IsIndex            bool
	IsUniqueIndex      bool
	IsNullable         bool
	OrdinalPosition    int64 // Position of column in the table definition
	KeyOrdinalPosition int64 // Position of primary or foreign key in a compound key definition
	IsEnumerated       bool
	ReferencedResource string
	ReferencedField    string
}

func (f *resourceField) TypescriptDataType() string {
	if f.typescriptType == "uuid" {
		return "string"
	}

	return f.typescriptType
}

func (f *resourceField) TypescriptDisplayType() string {
	if f.IsEnumerated {
		return "enumerated"
	}

	return f.typescriptType
}

func (f *resourceField) JSONTag() string {
	if f.IsPrimaryKey {
		return f.structField.JSONTag()
	}

	caser := strcase.NewCaser(false, nil, nil)
	camelCaseName := caser.ToCamel(f.Name())
	return fmt.Sprintf("json:%q", camelCaseName+",omitzero")
}

func (f *resourceField) JSONTagForPatch() string {
	if f.IsPrimaryKey || f.IsImmutable() {
		return fmt.Sprintf("json:%q", "-")
	}

	caser := strcase.NewCaser(false, nil, nil)
	camelCaseName := caser.ToCamel(f.Name())

	return fmt.Sprintf("json:%q", camelCaseName)
}

func (f *resourceField) IndexTag() string {
	if f.IsIndex {
		return `index:"true"`
	}

	return ""
}

func (f *resourceField) UniqueIndexTag() string {
	if f.IsUniqueIndex {
		return `index:"true"`
	}

	return ""
}

func (f *resourceField) IsImmutable() bool {
	tag, ok := f.tags.Lookup("conditions")
	if !ok {
		return false
	}

	conditions := strings.Split(tag, ",")

	return slices.Contains(conditions, "immutable")
}

func (f *resourceField) QueryTag() string {
	query, ok := f.tags.Lookup("query")
	if !ok {
		return ""
	}

	return fmt.Sprintf("query:%q", query)
}

func (f *resourceField) ReadPermTag() string {
	tag, ok := f.tags.Lookup("perm")
	if !ok {
		return ""
	}

	permissions := strings.Split(tag, ",")

	if slices.Contains(permissions, "Read") {
		return fmt.Sprintf("perm:%q", "Read")
	}

	return ""
}

func (f *resourceField) ListPermTag() string {
	tag, ok := f.tags.Lookup("perm")
	if !ok {
		return ""
	}

	permissions := strings.Split(tag, ",")

	if slices.Contains(permissions, "List") {
		return fmt.Sprintf("perm:%q", "List")
	}

	return ""
}

func (f *resourceField) PatchPermTag() string {
	tag, ok := f.tags.Lookup("perm")
	if !ok {
		return ""
	}

	permissions := strings.Split(tag, ",")

	var patches []string
	for _, perm := range permissions {
		if perm != "Read" && perm != "List" {
			patches = append(patches, perm)
		}
	}

	if len(patches) != 0 {
		return fmt.Sprintf("perm:%q", strings.Join(patches, ","))
	}

	return ""
}

func (f *resourceField) SearchIndexTags() string {
	typeIndexMap := make(map[resource.FilterType][]string)
	for searchIndex, expressionFields := range f.Parent.searchIndexes {
		for _, exprField := range expressionFields {
			if f.tags.Get("spanner") == exprField.fieldName {
				typeIndexMap[exprField.tokenType] = append(typeIndexMap[exprField.tokenType], searchIndex)
			}
		}
	}

	var tags []string
	for tokenType, indexes := range typeIndexMap {
		tags = append(tags, fmt.Sprintf("%s:%q", tokenType, strings.Join(indexes, ",")))
	}

	return strings.Join(tags, " ")
}

func (f *resourceField) IsView() bool {
	return f.Parent.IsView
}

func (f *resourceField) IsRequired() bool {
	if f.IsPrimaryKey && f.Type() != "ccc.UUID" {
		return true
	}

	if !f.IsPrimaryKey && !f.IsNullable {
		return true
	}

	return false
}

type expressionField struct {
	tokenType resource.FilterType
	fieldName string
}

func generatedFileName(name string) string {
	return fmt.Sprintf("%s_%s.go", genPrefix, name)
}

type TSGenMode interface {
	mode()
}

type tsGenMode int

func (t tsGenMode) mode() {}

const (
	// Adds permission.ts to generator output
	TSPerm tsGenMode = 1 << iota

	// Adds resource.ts to generator output
	TSMeta
)
