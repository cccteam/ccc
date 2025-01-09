package generation

import (
	"strings"

	"github.com/ettle/strcase"
	"github.com/gertd/go-pluralize"
)

var templateFuncs = map[string]any{
	"Pluralize": pluralize.NewClient().Plural,
	"GoCamel":   strcase.ToGoCamel,
	"Camel":     strcase.ToCamel,
	"IsPtr": func(fieldType string) bool {
		return strings.HasPrefix(fieldType, "*")
	},
	"TrimPtr": func(fieldType string) string {
		return strings.TrimPrefix(fieldType, "*")
	},
	"PrimaryKeyTypeIsUUID": func(fields []*typeField) bool {
		for _, f := range fields {
			if f.IsPrimaryKey {
				return f.Type == "ccc.UUID"
			}
		}

		return false
	},
	"FormatPerm": func(s string) string {
		if s == "" {
			return ""
		}

		return ` perm:"` + s + `"`
	},
	"PrimaryKeyType": func(fields []*typeField) string {
		for _, f := range fields {
			if f.IsPrimaryKey {
				return f.Type
			}
		}

		return ""
	},
	"FormatQueryTag": func(query string) string {
		if query != "" {
			return " " + query
		}

		return ""
	},
	"DetermineJSONTag": func(field *typeField, isPatch bool) string {
		if isPatch {
			if field.IsPrimaryKey {
				return "-"
			}

			for _, c := range field.Conditions {
				if c == "immutable" {
					return "-"
				}
			}
		}

		val := strcase.ToCamel(field.Name)
		if !field.IsPrimaryKey && !isPatch {
			val += ",omitempty"
		}

		return val
	},
}

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

const (
	querySetOutputFilename          = "types.go"
	resourceInterfaceOutputFilename = "resources_iface.go"
)

type Config struct {
	ResourceSource     string
	HandlerDestination string
	SpannerDestination string
	Migrations         string
	PluralRules        map[string]string
	HandlerOptions     map[string]map[HandlerType][]OptionType
}

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
}

type InformationSchemaResult struct {
	TableName      string  `spanner:"TABLE_NAME"`
	ColumnName     string  `spanner:"COLUMN_NAME"`
	ConstraintName *string `spanner:"CONSTRAINT_NAME"`
	ConstraintType *string `spanner:"CONSTRAINT_TYPE"`
	SpannerType    string  `spanner:"SPANNER_TYPE"`
	IsNullable     bool    `spanner:"IS_NULLABLE"`
	IsView         bool    `spanner:"IS_VIEW"`
	IsIndex        bool    `spanner:"IS_INDEX"`
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
