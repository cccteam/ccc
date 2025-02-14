package generation

import (
	"fmt"
	"go/types"
	"regexp"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource"

	"github.com/ettle/strcase"
)

var baseTypes = []string{
	"bool",
	"string",
	"int", "int8", "int16", "int32", "int64",
	"float32", "float64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"byte",
	"rune",
	"complex64", "complex128",
	"error",
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
	querySetOutputFilename      = "types.go"
	resourceInterfaceOutputName = "resources_iface"
	resourcesTestFileName       = "resource_types_test.go"
	routesName                  = "routes"
	routerTestName              = "routes_test"
)

type generatedType struct {
	Name                  string
	IsView                bool
	HasCompoundPrimaryKey bool
	Fields                []*typeField
	SearchIndexes         []*searchIndex
}

type typeField struct {
	Name            string
	Type            string
	Tag             string
	IsPrimaryKey    bool
	IsIndex         bool
	IsUniqueIndex   bool
	ConstraintTypes []ConstraintType
	fieldTagInfo
}

type fieldTagInfo struct {
	QueryTag      string
	ReadPerm      string
	ListPerm      string
	PatchPerm     string
	Conditions    []string
	SpannerColumn string
}

type searchIndex struct {
	Name       string
	SearchType string
}

type ColumnMeta struct {
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

type TableMetadata struct {
	Columns       map[string]ColumnMeta
	SearchIndexes map[string][]*expressionField
	IsView        bool
	PkCount       int
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

type _tsType int

const (
	link _tsType = iota
	uuid
	boolean
	str
	number
	date
	enumerated
)

func (t _tsType) String() string {
	switch t {
	case link:
		return "Link"
	case uuid:
		return "uuid"
	case boolean:
		return "boolean"
	case str:
		return "string"
	case number:
		return "number"
	case date:
		return "Date"
	case enumerated:
		return "enumerated"
	}

	return "string"
}

type generatedResource struct {
	Name               string
	Fields             []*generatedResource
	dataType           _tsType
	Required           bool
	IsPrimaryKey       bool
	IsForeignKey       bool
	OrdinalPosition    int64
	KeyOrdinalPosition int64
	ReferencedResource string
	ReferencedColumn   string
}

type ResourceInfo struct {
	Name                  string
	Fields                []FieldInfo
	SearchIndexes         []*searchIndex
	IsView                bool // Determines how CreatePatch is rendered in resource generation.
	HasCompoundPrimaryKey bool // Determines how CreatePatchSet is rendered in resource generation.
}

type FieldInfo struct {
	Parent             *ResourceInfo
	Name               string
	SpannerName        string
	GoType             string
	typescriptType     string
	query              string   //
	Conditions         []string // Contains auxillary tags like `immutable`. Determines JSON tag in handler generation.
	permissions        []string
	Required           bool
	IsPrimaryKey       bool
	IsForeignKey       bool
	IsIndex            bool
	IsUniqueIndex      bool
	OrdinalPosition    int64 // Position of column in the table definition
	KeyOrdinalPosition int64 // Position of primary or foreign key in a compound key definition
	IsEnumerated       bool
	ReferencedResource string
	ReferencedField    string
}

func (f FieldInfo) TypescriptDataType() string {
	if f.typescriptType == "uuid" {
		return "string"
	}

	return f.typescriptType
}

func (f FieldInfo) TypescriptDisplayType() string {
	if f.IsEnumerated {
		return "enumerated"
	}
	return f.typescriptType
}

func (f FieldInfo) JSONTag() string {
	caser := strcase.NewCaser(false, nil, nil)
	camelCaseName := caser.ToCamel(f.Name)

	if !f.IsPrimaryKey {
		return fmt.Sprintf("json:%q", camelCaseName+",omitempty")
	}

	return fmt.Sprintf("json:%q", camelCaseName)
}

func (f FieldInfo) JSONTagForPatch() string {
	if f.IsPrimaryKey || f.IsImmutable() {
		return fmt.Sprintf("json:%q", "-")
	}

	caser := strcase.NewCaser(false, nil, nil)
	camelCaseName := caser.ToCamel(f.Name)

	return fmt.Sprintf("json:%q", camelCaseName)
}

func (f FieldInfo) IndexTag() string {
	if f.IsIndex {
		return `index:"true"`
	}

	return ""
}

func (f FieldInfo) UniqueIndexTag() string {
	if f.IsUniqueIndex {
		return `index:"true"`
	}

	return ""
}

func (f FieldInfo) IsImmutable() bool {
	return slices.Contains(f.Conditions, "immutable")
}

func (f FieldInfo) QueryTag() string {
	if f.query != "" {
		return fmt.Sprintf("query:%q", f.query)
	}

	return ""
}

func (f FieldInfo) ReadPermTag() string {
	if slices.Contains(f.permissions, "Read") {
		return fmt.Sprintf("perm:%q", "Read")
	}

	return ""
}

func (f FieldInfo) ListPermTag() string {
	if slices.Contains(f.permissions, "List") {
		return fmt.Sprintf("perm:%q", "List")
	}

	return ""
}

func (f FieldInfo) PatchPermTag() string {
	var patches []string
	for _, perm := range f.permissions {
		if perm != "Read" && perm != "List" {
			patches = append(patches, perm)
		}
	}

	if len(patches) != 0 {
		return fmt.Sprintf("perm:%q", strings.Join(patches, ","))
	}

	return ""
}

func (f FieldInfo) IsView() bool {
	return f.Parent.IsView
}

func (r generatedResource) DataType() string {
	if r.dataType == uuid || r.dataType == enumerated {
		return str.String()
	}

	return r.dataType.String()
}

func (r generatedResource) DisplayType() string {
	return r.dataType.String()
}

type expressionField struct {
	tokenType resource.SearchType
	fieldName string
}

func generatedFileName(name string) string {
	return fmt.Sprintf("%s_%s.go", genPrefix, name)
}

type intermediateStruct struct {
	types.Struct
}
