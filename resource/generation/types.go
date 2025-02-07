package generation

import "fmt"

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
	resourcesTestName           = "resources_test"
)

type generatedType struct {
	Name            string
	IsView          bool
	IsCompoundTable bool
	Fields          []*typeField
}

type typeField struct {
	Name           string
	Type           string
	Tag            string
	IsPrimaryKey   bool
	IsIndex        bool
	IsUniqueIndex  bool
	QueryTag       string
	ConstraintType string
	ReadPerm       string
	ListPerm       string
	PatchPerm      string
	Conditions     []string
}

type FieldMetadata struct {
	ConstraintType ConstraintType
	ColumnName     string
	SpannerType    string
	IsNullable     bool
	IsIndex        bool
	IsUniqueIndex  bool
}

type InformationSchemaResult struct {
	TableName       string  `spanner:"TABLE_NAME"`
	ColumnName      string  `spanner:"COLUMN_NAME"`
	ConstraintName  *string `spanner:"CONSTRAINT_NAME"`
	ConstraintType  *string `spanner:"CONSTRAINT_TYPE"`
	SpannerType     string  `spanner:"SPANNER_TYPE"`
	IsNullable      bool    `spanner:"IS_NULLABLE"`
	IsView          bool    `spanner:"IS_VIEW"`
	IsIndex         bool    `spanner:"IS_INDEX"`
	IsUniqueIndex   bool    `spanner:"IS_UNIQUE_INDEX"`
	OrdinalPosition int64   `spanner:"ORDINAL_POSITION"`
}

type TableMetadata struct {
	Columns map[string]FieldMetadata
	IsView  bool
}

type generationOption struct {
	option  OptionType
	handler HandlerType
}

type generatedHandler struct {
	template    string
	handlerType HandlerType
}

type generatedResource struct {
	Name     string
	Fields   []*generatedResource
	dataType string
	Required bool
}

func (r generatedResource) DataType() string {
	if r.dataType == "uuid" {
		return "string"
	}

	if r.dataType == "link" {
		return "Link"
	}

	return r.dataType
}

func (r generatedResource) MetaType() string {
	return r.dataType
}

func generatedFileName(name string) string {
	return fmt.Sprintf("%s_%s.go", genPrefix, name)
}
